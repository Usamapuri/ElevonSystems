package fiscal

import (
	"bytes"
	"os"
	"testing"
)

// CLAUDE.md invariant 7: retries consult fiscal_invoices first.
//
// FBR has no idempotency key. The only thing standing between a retried
// submission and a duplicate filing against the store's real NTN is that
// Submit reads its own ledger before it opens a socket. That ordering is not
// something a behavioural test can pin — a passing happy-path test looks
// identical whether the check runs first or last — so this test reads
// submit.go as source and fails the build if the calls ever swap.
func TestSubmit_ConsultsTheLedgerBeforeAnyHTTP(t *testing.T) {
	src, err := os.ReadFile("submit.go")
	if err != nil {
		t.Fatal(err)
	}

	ledger := bytes.Index(src, []byte("FindSynced("))
	if ledger < 0 {
		t.Fatal("submit.go must call FindSynced")
	}
	validate := bytes.Index(src, []byte(".Validate("))
	if validate < 0 {
		t.Fatal("submit.go must call the client's Validate")
	}
	post := bytes.Index(src, []byte(".Post("))
	if post < 0 {
		t.Fatal("submit.go must call the client's Post")
	}

	if ledger > validate {
		t.Fatalf("FindSynced (offset %d) must appear before .Validate (offset %d): a retry that "+
			"validates before checking fiscal_invoices can file the same invoice twice", ledger, validate)
	}
	if validate > post {
		t.Fatalf(".Validate (offset %d) must appear before .Post (offset %d): the DI flow is "+
			"validate then post", validate, post)
	}
}

// The same contract for combined invoices: SubmitBatch consults
// fiscal_invoices (FindSyncedBatch) before handing the document to file(),
// which is where the HTTP happens.
func TestSubmitBatch_ConsultsTheLedgerBeforeAnyHTTP(t *testing.T) {
	src, err := os.ReadFile("batch.go")
	if err != nil {
		t.Fatal(err)
	}
	ledger := bytes.Index(src, []byte("FindSyncedBatch("))
	if ledger < 0 {
		t.Fatal("batch.go must call FindSyncedBatch")
	}
	send := bytes.Index(src, []byte("file("))
	if send < 0 {
		t.Fatal("batch.go must send through file()")
	}
	if ledger > send {
		t.Fatalf("FindSyncedBatch (offset %d) must appear before file( (offset %d)", ledger, send)
	}
	// And file() is the only place in the package that opens a socket.
	for _, name := range []string{"batch.go"} {
		body, _ := os.ReadFile(name)
		if bytes.Contains(body, []byte(".Validate(")) || bytes.Contains(body, []byte(".Post(")) {
			t.Fatalf("%s must not call the client directly; Submit and SubmitBatch share file()", name)
		}
	}
}
