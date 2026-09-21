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
