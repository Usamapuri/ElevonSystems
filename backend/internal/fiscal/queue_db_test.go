package fiscal_test

import (
	"context"
	"database/sql"
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
	if err := fiscal.Enqueue(db, fiscal.JobRef{BatchID: &b.ID}, fiscal.KindBatch, time.Now().Add(-time.Second)); err != nil {
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
	if err := fiscal.Enqueue(db, fiscal.JobRef{BatchID: &b.ID}, fiscal.KindBatch, time.Now().Add(-time.Second)); err != nil {
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
