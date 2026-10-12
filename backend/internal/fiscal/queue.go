package fiscal

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"
)

// The outbound queue. A job is one document (a sale, a debit note or a
// combined invoice) FBR could not be reached for. The worker claims jobs
// with FOR UPDATE SKIP LOCKED, so several processes can drain the same
// table, and retries with a bounded backoff. A job dies on anything that
// retrying cannot fix: FBR's own rejection, a refusal this app made, or the
// attempt ceiling.

// JobRef names the document a job files: exactly one of InvoiceID or BatchID.
type JobRef struct {
	InvoiceID *uuid.UUID
	BatchID   *uuid.UUID
}

// Job is one fiscal_outbound_jobs row.
type Job struct {
	ID            uuid.UUID  `json:"id"`
	Ref           JobRef     `json:"-"`
	InvoiceID     *uuid.UUID `json:"invoice_id"`
	BatchID       *uuid.UUID `json:"batch_id"`
	Kind          Kind       `json:"kind"`
	Status        string     `json:"status"`
	AttemptCount  int        `json:"attempt_count"`
	MaxAttempts   int        `json:"max_attempts"`
	NextRunAt     time.Time  `json:"next_run_at"`
	LastError     *string    `json:"last_error"`
	LastErrorCode *string    `json:"last_error_code"`
	CreatedAt     time.Time  `json:"created_at"`
}

const codeMaxAttempts = "fiscal_max_attempts"

const jobColumns = `id, invoice_id, batch_id, kind, status, attempt_count, max_attempts, next_run_at, last_error, last_error_code, created_at`

func scanJob(row interface{ Scan(...any) error }) (*Job, error) {
	var j Job
	if err := row.Scan(&j.ID, &j.InvoiceID, &j.BatchID, &j.Kind, &j.Status, &j.AttemptCount, &j.MaxAttempts,
		&j.NextRunAt, &j.LastError, &j.LastErrorCode, &j.CreatedAt); err != nil {
		return nil, err
	}
	j.Ref = JobRef{InvoiceID: j.InvoiceID, BatchID: j.BatchID}
	return &j, nil
}

// Enqueue schedules a filing. It is a no-op while an active job already
// exists for the same document and kind: the partial unique indexes make a
// duplicate impossible, and a WHERE NOT EXISTS keeps the insert quiet
// rather than erroring (a partial index cannot be an ON CONFLICT target
// without restating its predicate).
func Enqueue(q Execer, ref JobRef, kind Kind, runAt time.Time) error {
	_, err := q.Exec(`
		INSERT INTO fiscal_outbound_jobs (invoice_id, batch_id, kind, next_run_at)
		SELECT $1::uuid, $2::uuid, $3::varchar, $4::timestamptz
		WHERE NOT EXISTS (
			SELECT 1 FROM fiscal_outbound_jobs
			WHERE kind = $3::varchar AND status IN ('pending','processing')
			  AND invoice_id IS NOT DISTINCT FROM $1::uuid AND batch_id IS NOT DISTINCT FROM $2::uuid)`,
		ref.InvoiceID, ref.BatchID, string(kind), runAt)
	return err
}

// ClaimNext takes the oldest due job, marking it processing. nil means
// nothing is due.
func ClaimNext(db *sql.DB, now time.Time) (*Job, error) {
	j, err := scanJob(db.QueryRow(`
		UPDATE fiscal_outbound_jobs
		SET status = 'processing', started_at = $1, updated_at = $1
		WHERE id = (
			SELECT id FROM fiscal_outbound_jobs
			WHERE status = 'pending' AND next_run_at <= $1
			ORDER BY next_run_at, created_at
			LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING `+jobColumns, now))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return j, err
}

// Backoff is the wait before attempt+1. attempt is 1-based: the first
// failure waits 30 s, then 1, 2, 5 and 10 minutes, then 15 minutes flat.
func Backoff(attempt int) time.Duration {
	table := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute}
	if attempt < 1 {
		attempt = 1
	}
	if attempt <= len(table) {
		return table[attempt-1]
	}
	return 15 * time.Minute
}

func succeed(db *sql.DB, id uuid.UUID, now time.Time) error {
	_, err := db.Exec(`UPDATE fiscal_outbound_jobs
		SET status = 'succeeded', succeeded_at = $2, updated_at = $2, last_error = NULL, last_error_code = NULL
		WHERE id = $1`, id, now)
	return err
}

func retry(db *sql.DB, j *Job, cause error, now time.Time) error {
	attempt := j.AttemptCount + 1
	_, err := db.Exec(`UPDATE fiscal_outbound_jobs
		SET status = 'pending', attempt_count = $2, next_run_at = $3, updated_at = $4,
		    last_error = $5, last_error_code = $6
		WHERE id = $1`, j.ID, attempt, now.Add(Backoff(attempt)), now, truncate(cause.Error(), maxFailureMessage), ErrorCode(cause))
	return err
}

func dead(db *sql.DB, j *Job, code, message string, now time.Time) error {
	_, err := db.Exec(`UPDATE fiscal_outbound_jobs
		SET status = 'dead', attempt_count = attempt_count + 1, updated_at = $2, last_error = $3, last_error_code = $4
		WHERE id = $1`, j.ID, now, truncate(message, maxFailureMessage), truncate(code, maxErrorCode))
	return err
}

// ProcessOnce claims and runs one job. It returns false when nothing was
// due. A job's own failure is recorded on the job, never returned: the
// worker keeps draining.
func ProcessOnce(ctx context.Context, d Deps) (bool, error) {
	now := d.now()
	j, err := ClaimNext(d.DB, now)
	if err != nil || j == nil {
		return false, err
	}

	var submitErr error
	switch {
	case j.Ref.BatchID != nil:
		_, submitErr = SubmitBatch(ctx, d, *j.Ref.BatchID, nil)
	case j.Ref.InvoiceID != nil:
		_, submitErr = Submit(ctx, d, *j.Ref.InvoiceID, j.Kind, nil)
	default:
		submitErr = ErrUnknownKind
	}

	switch {
	case submitErr == nil:
		return true, succeed(d.DB, j.ID, d.now())
	case errors.Is(submitErr, ErrUnreachable) || errors.Is(submitErr, context.DeadlineExceeded):
		if j.AttemptCount+1 >= j.MaxAttempts {
			cause := &codedError{code: codeMaxAttempts, message: "FBR could not be reached after " + itoa(j.MaxAttempts) + " attempts"}
			return true, giveUp(d.DB, j, cause, d.now())
		}
		return true, retry(d.DB, j, submitErr, d.now())
	default:
		return true, giveUp(d.DB, j, submitErr, d.now())
	}
}

// giveUp kills the job and records the failure on its document: MarkFailed
// for an invoice, FailBatch (which also releases the invoices) for a batch.
// The same cause lands in both places, so the job and the document agree.
func giveUp(db *sql.DB, j *Job, cause error, now time.Time) error {
	code, message := failureDetail(cause)
	if err := dead(db, j, code, message, now); err != nil {
		return err
	}
	switch {
	case j.Ref.BatchID != nil:
		return FailBatch(db, *j.Ref.BatchID, cause)
	case j.Ref.InvoiceID != nil:
		return MarkFailed(db, *j.Ref.InvoiceID, j.Kind, cause)
	}
	return nil
}

// codedError carries a code and message through failureDetail unchanged,
// so a dead job's reason lands on the invoice exactly as recorded.
type codedError struct{ code, message string }

func (e *codedError) Error() string { return e.message }
func (e *codedError) Code() string  { return e.code }

// StartWorker drains the queue every `every` until ctx is done. Errors are
// logged, panics recovered: a broken job must never take the server down.
func StartWorker(ctx context.Context, d Deps, every time.Duration) {
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				drain(ctx, d)
			}
		}
	}()
}

func drain(ctx context.Context, d Deps) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("fiscal worker: recovered: %v", r)
		}
	}()
	for {
		did, err := ProcessOnce(ctx, d)
		if err != nil {
			log.Printf("fiscal worker: %v", err)
			return
		}
		if !did {
			return
		}
	}
}

// Counts summarises the queue for the status screen.
func Counts(q Querier) (pending, dead int, lastSuccess *time.Time, err error) {
	err = q.QueryRow(`SELECT
		count(*) FILTER (WHERE status IN ('pending','processing')),
		count(*) FILTER (WHERE status = 'dead'),
		max(succeeded_at)
		FROM fiscal_outbound_jobs`).Scan(&pending, &dead, &lastSuccess)
	return
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
