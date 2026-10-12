package fiscal_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"elevon-backend/internal/fiscal"
	"elevon-backend/internal/testdb"

	"github.com/google/uuid"
)

// The outbound queue: one active job per document, claimed with SKIP LOCKED,
// retried with backoff while FBR is unreachable, dead on anything FBR or
// this app has decided against.

func jobState(t *testing.T, db *sql.DB, id uuid.UUID) (status string, attempts int, nextRun time.Time, code sql.NullString) {
	t.Helper()
	if err := db.QueryRow(`SELECT status, attempt_count, next_run_at, last_error_code FROM fiscal_outbound_jobs WHERE id = $1`, id).
		Scan(&status, &attempts, &nextRun, &code); err != nil {
		t.Fatal(err)
	}
	return
}

// liveDeps uses the real clock: the queue schedules against now, so the
// fixed fixture clock in deps() would make nothing ever due.
func liveDeps(db *sql.DB) fiscal.Deps { return fiscal.Deps{DB: db, Timeout: 5 * time.Second} }

func activeJobs(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM fiscal_outbound_jobs WHERE status IN ('pending','processing')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestEnqueue_OneActiveJobPerDocument(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	enableFiscal(t, db, nil, true)
	cfg, _ := fiscal.LoadConfig(db)
	inv := seedBatchSale(t, db, "20261012-001", seedOpt{fiscal: "pending"})
	b, err := fiscal.CreateBatch(db, batchDay(), []uuid.UUID{seedBatchSale(t, db, "20261012-002", seedOpt{})}, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i := 0; i < 2; i++ {
		if err := fiscal.Enqueue(db, fiscal.JobRef{InvoiceID: &inv}, fiscal.KindSale, now); err != nil {
			t.Fatal(err)
		}
		if err := fiscal.Enqueue(db, fiscal.JobRef{BatchID: &b.ID}, fiscal.KindBatch, now); err != nil {
			t.Fatal(err)
		}
	}
	if activeJobs(t, db) != 2 {
		t.Fatalf("one job per document while active, got %d", activeJobs(t, db))
	}
	// A debit note for the same invoice is a different document.
	if err := fiscal.Enqueue(db, fiscal.JobRef{InvoiceID: &inv}, fiscal.KindDebitNote, now); err != nil {
		t.Fatal(err)
	}
	if activeJobs(t, db) != 3 {
		t.Fatalf("sale and debit note are separate jobs, got %d", activeJobs(t, db))
	}
}

func TestClaimNext_SkipsFutureAndHandsEachJobToOneWorker(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	enableFiscal(t, db, nil, true)
	a := seedBatchSale(t, db, "20261012-001", seedOpt{fiscal: "pending"})
	bID := seedBatchSale(t, db, "20261012-002", seedOpt{fiscal: "pending"})
	c := seedBatchSale(t, db, "20261012-003", seedOpt{fiscal: "pending"})
	now := time.Now()
	if err := fiscal.Enqueue(db, fiscal.JobRef{InvoiceID: &a}, fiscal.KindSale, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := fiscal.Enqueue(db, fiscal.JobRef{InvoiceID: &bID}, fiscal.KindSale, now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := fiscal.Enqueue(db, fiscal.JobRef{InvoiceID: &c}, fiscal.KindSale, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	got := make([]*fiscal.Job, 2)
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			j, err := fiscal.ClaimNext(db, now)
			if err != nil {
				t.Error(err)
				return
			}
			got[i] = j
		}(i)
	}
	wg.Wait()
	if got[0] == nil || got[1] == nil || got[0].ID == got[1].ID {
		t.Fatalf("two concurrent claims get two different jobs: %+v %+v", got[0], got[1])
	}
	for _, j := range got {
		if j.Ref.InvoiceID == nil || *j.Ref.InvoiceID == c || j.Status != "processing" {
			t.Fatalf("the future job is never claimed and a claim is 'processing': %+v", j)
		}
	}
	if j, err := fiscal.ClaimNext(db, now); err != nil || j != nil {
		t.Fatalf("nothing else is due: %+v %v", j, err)
	}
}

func TestBackoff_Table(t *testing.T) {
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 15 * time.Minute}
	for i, d := range want {
		if got := fiscal.Backoff(i + 1); got != d {
			t.Errorf("Backoff(%d) = %v, want %v", i+1, got, d)
		}
	}
}

func TestProcessOnce_UnreachableReschedulesWithBackoff(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	s := newStub(t, []byte(`<html>bad gateway</html>`), fixtureBody(t, "resp_postA.json"))
	s.validateStatus = http.StatusBadGateway
	enableFiscal(t, db, s, true)
	inv := seedBatchSale(t, db, "20261012-001", seedOpt{fiscal: "pending"})
	if err := fiscal.Enqueue(db, fiscal.JobRef{InvoiceID: &inv}, fiscal.KindSale, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	var jobID uuid.UUID
	if err := db.QueryRow(`SELECT id FROM fiscal_outbound_jobs`).Scan(&jobID); err != nil {
		t.Fatal(err)
	}

	did, err := fiscal.ProcessOnce(context.Background(), liveDeps(db))
	if err != nil || !did {
		t.Fatalf("one job processed: %v %v", did, err)
	}
	status, attempts, nextRun, code := jobState(t, db, jobID)
	if status != "pending" || attempts != 1 || nextRun.Before(time.Now().Add(25*time.Second)) || code.String != "fiscal_unreachable" {
		t.Fatalf("rescheduled ~30s out: %s %d %v %v", status, attempts, nextRun, code)
	}
	if st, _, _ := invoiceFiscal(t, db, inv); st != "pending" {
		t.Fatalf("invoice stays pending: %s", st)
	}
	if did, _ := fiscal.ProcessOnce(context.Background(), liveDeps(db)); did {
		t.Fatal("nothing is due until next_run_at")
	}
}

func TestProcessOnce_RejectedSaleIsDeadAndFailed(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	s := newStub(t, fixtureBody(t, "resp_negTax.json"), fixtureBody(t, "resp_postA.json"))
	enableFiscal(t, db, s, true)
	inv := seedBatchSale(t, db, "20261012-001", seedOpt{fiscal: "pending"})
	if err := fiscal.Enqueue(db, fiscal.JobRef{InvoiceID: &inv}, fiscal.KindSale, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := fiscal.ProcessOnce(context.Background(), liveDeps(db)); err != nil {
		t.Fatal(err)
	}
	var jobID uuid.UUID
	if err := db.QueryRow(`SELECT id FROM fiscal_outbound_jobs`).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	status, _, _, code := jobState(t, db, jobID)
	if status != "dead" || code.String != "0104" {
		t.Fatalf("a rejection cannot succeed on retry: %s %v", status, code)
	}
	if st, _, _ := invoiceFiscal(t, db, inv); st != "failed" {
		t.Fatalf("invoice marked failed: %s", st)
	}
}

func TestProcessOnce_RejectedBatchIsDeadAndReleased(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	s := newStub(t, fixtureBody(t, "resp_negTax.json"), fixtureBody(t, "resp_postA.json"))
	enableFiscal(t, db, s, true)
	cfg, _ := fiscal.LoadConfig(db)
	ids := []uuid.UUID{seedBatchSale(t, db, "20261012-001", seedOpt{})}
	b, _ := fiscal.CreateBatch(db, batchDay(), ids, cfg, nil)
	// CreateBatch already queued the job for after the inline window; pull
	// it forward so the worker sees it now.
	if _, err := db.Exec(`UPDATE fiscal_outbound_jobs SET next_run_at = now() - interval '1 second' WHERE batch_id = $1`, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fiscal.ProcessOnce(context.Background(), liveDeps(db)); err != nil {
		t.Fatal(err)
	}
	got, _, _ := fiscal.LoadBatch(db, b.ID)
	st, _, batch := invoiceFiscal(t, db, ids[0])
	if got.Status != "failed" || st != "unfiled" || batch.Valid {
		t.Fatalf("batch failed, invoices released: %s / %s %v", got.Status, st, batch)
	}
}

func TestProcessOnce_MaxAttemptsKillsTheJobAndReleasesTheBatch(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	s := newStub(t, []byte(`<html>bad gateway</html>`), fixtureBody(t, "resp_postA.json"))
	s.validateStatus = http.StatusBadGateway
	enableFiscal(t, db, s, true)
	cfg, _ := fiscal.LoadConfig(db)
	ids := []uuid.UUID{seedBatchSale(t, db, "20261012-001", seedOpt{})}
	b, _ := fiscal.CreateBatch(db, batchDay(), ids, cfg, nil)
	// CreateBatch already queued the job for after the inline window; pull
	// it forward so the worker sees it now.
	if _, err := db.Exec(`UPDATE fiscal_outbound_jobs SET next_run_at = now() - interval '1 second' WHERE batch_id = $1`, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE fiscal_outbound_jobs SET attempt_count = max_attempts - 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := fiscal.ProcessOnce(context.Background(), liveDeps(db)); err != nil {
		t.Fatal(err)
	}
	var status string
	var code sql.NullString
	if err := db.QueryRow(`SELECT status, last_error_code FROM fiscal_outbound_jobs`).Scan(&status, &code); err != nil {
		t.Fatal(err)
	}
	if status != "dead" || code.String != "fiscal_max_attempts" {
		t.Fatalf("out of attempts: %s %s", status, code.String)
	}
	got, _, _ := fiscal.LoadBatch(db, b.ID)
	if st, _, _ := invoiceFiscal(t, db, ids[0]); got.Status != "failed" || st != "unfiled" {
		t.Fatalf("released for the operator: %s / %s", got.Status, st)
	}
	pending, dead, _, err := fiscal.Counts(db)
	if err != nil || pending != 0 || dead != 1 {
		t.Fatalf("counts: %d %d %v", pending, dead, err)
	}
}

func TestProcessOnce_SucceededJobStampsLastSuccess(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	s := acceptingStub(t)
	enableFiscal(t, db, s, true)
	inv := seedBatchSale(t, db, "20261012-001", seedOpt{fiscal: "pending"})
	if err := fiscal.Enqueue(db, fiscal.JobRef{InvoiceID: &inv}, fiscal.KindSale, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := fiscal.ProcessOnce(context.Background(), liveDeps(db)); err != nil {
		t.Fatal(err)
	}
	if st, num, _ := invoiceFiscal(t, db, inv); st != "synced" || !num.Valid {
		t.Fatalf("filed: %s %v", st, num)
	}
	pending, dead, last, err := fiscal.Counts(db)
	if err != nil || pending != 0 || dead != 0 || last == nil {
		t.Fatalf("counts after success: %d %d %v %v", pending, dead, last, err)
	}
}

// ── final-review fixes ───────────────────────────────────────────────────

// Review finding 1: a document is filed only from the state that asked for
// it. A sale that was since covered by a combined invoice (or voided) must
// never be posted again, whatever job still points at it.
func TestSubmit_RefusesADocumentNotPending(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	s := acceptingStub(t)
	enableFiscal(t, db, s, true)
	for _, status := range []string{"covered", "unfiled", "synced", "failed"} {
		inv := seedBatchSale(t, db, "20261012-"+status[:3], seedOpt{fiscal: status})
		_, err := fiscal.Submit(context.Background(), liveDeps(db), inv, fiscal.KindSale, nil)
		if !errors.Is(err, fiscal.ErrNotPending) {
			t.Fatalf("%s: err = %v, want fiscal_not_pending", status, err)
		}
	}
	voided := seedBatchSale(t, db, "20261012-void", seedOpt{fiscal: "pending", status: "voided"})
	if _, err := fiscal.Submit(context.Background(), liveDeps(db), voided, fiscal.KindSale, nil); !errors.Is(err, fiscal.ErrNotPending) {
		t.Fatalf("voided: %v", err)
	}
	if s.hits() != 0 {
		t.Fatalf("no HTTP for a refused document: %d", s.hits())
	}
}

func TestRetryJob_ReclaimsOrRefuses(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	enableFiscal(t, db, nil, true)
	dead := func(inv uuid.UUID, kind string) uuid.UUID {
		var id uuid.UUID
		if err := db.QueryRow(`INSERT INTO fiscal_outbound_jobs (invoice_id, kind, status, attempt_count, last_error_code)
			VALUES ($1, $2, 'dead', 20, 'fiscal_max_attempts') RETURNING id`, inv, kind).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	failed := seedBatchSale(t, db, "20261012-001", seedOpt{fiscal: "failed"})
	covered := seedBatchSale(t, db, "20261012-002", seedOpt{fiscal: "covered"})
	now := time.Now()

	if err := fiscal.RetryJob(db, dead(failed, "sale"), now); err != nil {
		t.Fatalf("a failed sale can be retried: %v", err)
	}
	if st, _, _ := invoiceFiscal(t, db, failed); st != "pending" {
		t.Fatalf("retry re-claims the sale: %s", st)
	}
	if err := fiscal.RetryJob(db, dead(covered, "sale"), now); !errors.Is(err, fiscal.ErrNotRetryable) {
		t.Fatalf("a covered sale must not be retried: %v", err)
	}
	if st, _, _ := invoiceFiscal(t, db, covered); st != "covered" {
		t.Fatalf("untouched: %s", st)
	}
	if err := fiscal.RetryJob(db, uuid.New(), now); !errors.Is(err, fiscal.ErrJobNotFound) {
		t.Fatalf("unknown job: %v", err)
	}
}

// Review finding 2: once FBR (or the ledger) holds a number, nothing may
// release the document for refiling.
func TestFailBatchAndMarkFailed_AreNoOpsOnceFiled(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	enableFiscal(t, db, nil, true)
	cfg, _ := fiscal.LoadConfig(db)
	ids := []uuid.UUID{seedBatchSale(t, db, "20261012-001", seedOpt{})}
	b, _ := fiscal.CreateBatch(db, batchDay(), ids, cfg, nil)
	if err := fiscal.RecordSyncedBatch(db, b.ID, "6110180871403DIBATCH0002", nil); err != nil {
		t.Fatal(err)
	}
	if err := fiscal.FailBatch(db, b.ID, errors.New("db hiccup")); err != nil {
		t.Fatal(err)
	}
	got, _, _ := fiscal.LoadBatch(db, b.ID)
	if st, _, batch := invoiceFiscal(t, db, ids[0]); got.Status != "pending" || st != "pending" || !batch.Valid {
		t.Fatalf("a filed batch is never released: %s / %s %v", got.Status, st, batch)
	}

	inv := seedBatchSale(t, db, "20261012-002", seedOpt{fiscal: "pending"})
	if err := fiscal.RecordSynced(db, inv, "sale", "6110180871403DISALE0002", nil); err != nil {
		t.Fatal(err)
	}
	if err := fiscal.MarkFailed(db, inv, fiscal.KindSale, errors.New("db hiccup")); err != nil {
		t.Fatal(err)
	}
	if st, _, _ := invoiceFiscal(t, db, inv); st != "pending" {
		t.Fatalf("a filed sale is never marked failed: %s", st)
	}
}

func TestEligible_ExcludesASaleFBRAlreadyHolds(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	enableFiscal(t, db, nil, true)
	cfg, _ := fiscal.LoadConfig(db)
	filedButFailed := seedBatchSale(t, db, "20261012-001", seedOpt{fiscal: "failed"})
	if err := fiscal.RecordSynced(db, filedButFailed, "sale", "6110180871403DISALE0003", nil); err != nil {
		t.Fatal(err)
	}
	clean := seedBatchSale(t, db, "20261012-002", seedOpt{fiscal: "failed"})
	rows, err := fiscal.Eligible(db, batchDay(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == filedButFailed && r.Eligible {
			t.Fatalf("a sale with a ledger row is not batch-eligible: %+v", r)
		}
	}
	_, err = fiscal.CreateBatch(db, batchDay(), []uuid.UUID{clean, filedButFailed}, cfg, nil)
	var inel *fiscal.IneligibleError
	if !errors.As(err, &inel) || len(inel.IDs) != 1 || inel.IDs[0] != filedButFailed {
		t.Fatalf("CreateBatch refuses it: %v", err)
	}
}

// Review finding 3: a job claimed by a process that died is reclaimed once
// its lease is up, so a redeploy mid-filing cannot strand a document.
func TestClaimNext_ReclaimsStaleProcessing(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	enableFiscal(t, db, nil, true)
	inv := seedBatchSale(t, db, "20261012-001", seedOpt{fiscal: "pending"})
	now := time.Now()
	if err := fiscal.Enqueue(db, fiscal.JobRef{InvoiceID: &inv}, fiscal.KindSale, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	j, err := fiscal.ClaimNext(db, now)
	if err != nil || j == nil {
		t.Fatalf("first claim: %+v %v", j, err)
	}
	if again, _ := fiscal.ClaimNext(db, now.Add(time.Minute)); again != nil {
		t.Fatal("a fresh processing job is not reclaimed")
	}
	again, err := fiscal.ClaimNext(db, now.Add(fiscal.ProcessingLease+time.Second))
	if err != nil || again == nil || again.ID != j.ID {
		t.Fatalf("stale processing job reclaimed after the lease: %+v %v", again, err)
	}
}

// Review finding 3: the job exists from the moment the document is claimed,
// so a crash between the commit and the inline attempt leaves something the
// worker will pick up.
func TestCreateBatch_LeavesAJobDueAfterTheInlineWindow(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	enableFiscal(t, db, nil, true)
	cfg, _ := fiscal.LoadConfig(db)
	b, err := fiscal.CreateBatch(db, batchDay(), []uuid.UUID{seedBatchSale(t, db, "20261012-001", seedOpt{})}, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	var status string
	var next time.Time
	if err := db.QueryRow(`SELECT status, next_run_at FROM fiscal_outbound_jobs WHERE batch_id = $1`, b.ID).Scan(&status, &next); err != nil {
		t.Fatalf("job row: %v", err)
	}
	if status != "pending" || next.Before(time.Now().Add(20*time.Second)) {
		t.Fatalf("pending, due after the inline window: %s %v", status, next)
	}
	if j, _ := fiscal.ClaimNext(db, time.Now()); j != nil {
		t.Fatal("not due yet")
	}
}

func TestSettleInline_ClosesTheJobTheInlineAttemptResolved(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	enableFiscal(t, db, nil, true)
	inv := seedBatchSale(t, db, "20261012-001", seedOpt{fiscal: "pending"})
	ref := fiscal.JobRef{InvoiceID: &inv}
	now := time.Now()
	if err := fiscal.Enqueue(db, ref, fiscal.KindSale, now.Add(8*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := fiscal.SettleInline(db, ref, fiscal.KindSale, nil, now); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM fiscal_outbound_jobs WHERE invoice_id = $1`, inv).Scan(&status); err != nil || status != "succeeded" {
		t.Fatalf("success closes the job: %s %v", status, err)
	}
	inv2 := seedBatchSale(t, db, "20261012-002", seedOpt{fiscal: "pending"})
	ref2 := fiscal.JobRef{InvoiceID: &inv2}
	_ = fiscal.Enqueue(db, ref2, fiscal.KindSale, now.Add(8*time.Second))
	if err := fiscal.SettleInline(db, ref2, fiscal.KindSale, &fiscal.RejectedError{Code: "0104", Message: "tax"}, now); err != nil {
		t.Fatal(err)
	}
	var code sql.NullString
	if err := db.QueryRow(`SELECT status, last_error_code FROM fiscal_outbound_jobs WHERE invoice_id = $1`, inv2).Scan(&status, &code); err != nil || status != "dead" || code.String != "0104" {
		t.Fatalf("a rejection kills the job: %s %v %v", status, code, err)
	}
	inv3 := seedBatchSale(t, db, "20261012-003", seedOpt{fiscal: "pending"})
	ref3 := fiscal.JobRef{InvoiceID: &inv3}
	_ = fiscal.Enqueue(db, ref3, fiscal.KindSale, now.Add(8*time.Second))
	if err := fiscal.SettleInline(db, ref3, fiscal.KindSale, fiscal.ErrUnreachable, now); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status FROM fiscal_outbound_jobs WHERE invoice_id = $1`, inv3).Scan(&status); err != nil || status != "pending" {
		t.Fatalf("unreachable leaves the job for the worker: %s %v", status, err)
	}
}
