package email

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

type failTransport struct{ t *testing.T }

func (f failTransport) RoundTrip(*http.Request) (*http.Response, error) {
	f.t.Fatal("dev-mode client must not touch the network")
	return nil, errors.New("unreachable")
}

func TestSendPasswordReset_DevModeLogsInsteadOfSending(t *testing.T) {
	c := &Client{httpClient: &http.Client{Transport: failTransport{t}}}
	if c.IsLiveSend() {
		t.Fatal("no key → not live")
	}
	if err := c.SendPasswordReset(context.Background(), "a@b.pk", "Ali", "http://x/reset-password?token=abc", "Gas Co"); err != nil {
		t.Fatalf("dev mode must succeed silently: %v", err)
	}
}

func TestIsLiveSend_NeedsKeyAndFrom(t *testing.T) {
	if (&Client{apiKey: "re_123"}).IsLiveSend() {
		t.Fatal("key without EMAIL_FROM is not live")
	}
	if !(&Client{apiKey: "re_123", fromAddr: "Gas Co <no-reply@example.com>"}).IsLiveSend() {
		t.Fatal("key + from is live")
	}
}

func TestBuildResetText_CarriesNameBusinessAndURL(t *testing.T) {
	txt := buildResetText("Gas Co", "Ali", "http://x/reset-password?token=abc")
	for _, want := range []string{"Hi Ali", "Gas Co", "http://x/reset-password?token=abc", "1 hour"} {
		if !strings.Contains(txt, want) {
			t.Errorf("text is missing %q:\n%s", want, txt)
		}
	}
}
