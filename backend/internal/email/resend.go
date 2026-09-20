// Package email sends transactional email through Resend's HTTP API.
// One endpoint, one call site (password reset), so no SDK. When
// RESEND_API_KEY or EMAIL_FROM is unset the message is logged instead of
// sent, so a fresh checkout works end to end (the operator copies the reset
// link from the backend log).
package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	resendEndpoint = "https://api.resend.com/emails"
	httpTimeout    = 10 * time.Second
)

// Client sends password-reset mail. The zero value logs instead of sending.
type Client struct {
	apiKey     string
	fromAddr   string
	httpClient *http.Client
}

// NewClientFromEnv reads RESEND_API_KEY and EMAIL_FROM. Missing values are
// not an error — dev mode logs the message. In release mode, that silent
// fallback is worth a boot warning: an operator who forgot to set them
// would otherwise only discover it when a store owner says the reset email
// never arrived.
func NewClientFromEnv() *Client {
	c := &Client{
		apiKey:     strings.TrimSpace(os.Getenv("RESEND_API_KEY")),
		fromAddr:   strings.TrimSpace(os.Getenv("EMAIL_FROM")),
		httpClient: &http.Client{Timeout: httpTimeout},
	}
	if os.Getenv("GIN_MODE") == "release" && !c.IsLiveSend() {
		log.Printf("WARNING: RESEND_API_KEY/EMAIL_FROM unset in release mode — password-reset links will be written to the log instead of emailed")
	}
	return c
}

// IsLiveSend reports whether a real Resend request will be made.
func (c *Client) IsLiveSend() bool {
	return c != nil && c.apiKey != "" && c.fromAddr != ""
}

// SendPasswordReset delivers the reset link. resetURL already carries the
// one-time token. businessName is the store's configured name.
func (c *Client) SendPasswordReset(ctx context.Context, toEmail, firstName, resetURL, businessName string) error {
	if c == nil {
		return fmt.Errorf("email: nil client")
	}
	if strings.TrimSpace(businessName) == "" {
		businessName = "Elevon POS"
	}
	subject := fmt.Sprintf("Reset your %s password", businessName)
	if !c.IsLiveSend() {
		log.Printf("EMAIL (dev mode, RESEND_API_KEY/EMAIL_FROM unset)\n  to: %s\n  subject: %s\n  reset URL: %s", toEmail, subject, resetURL)
		return nil
	}
	payload := map[string]any{
		"from":    c.fromAddr,
		"to":      []string{toEmail},
		"subject": subject,
		"html":    buildResetHTML(businessName, firstName, resetURL),
		"text":    buildResetText(businessName, firstName, resetURL),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("email: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resendEndpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("email: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := c.httpClient
	if client == nil {
		client = &http.Client{Timeout: httpTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("email: resend request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("email: resend returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
}

func greeting(firstName string) string {
	if n := strings.TrimSpace(firstName); n != "" {
		return "Hi " + n
	}
	return "Hi"
}

// buildResetHTML is inline-styled: mail clients strip <style> blocks. The
// three inputs (business name, greeting, reset URL) all ultimately come
// from user-controlled data (settings, first name, a generated token), so
// each is HTML-escaped before being interpolated into the markup.
func buildResetHTML(business, firstName, url string) string {
	business = html.EscapeString(business)
	greet := html.EscapeString(greeting(firstName))
	url = html.EscapeString(url)
	return fmt.Sprintf(`<!doctype html>
<html><body style="margin:0;padding:24px;background:#f5f5f4;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;color:#1c1917;">
<table role="presentation" width="100%%" cellspacing="0" cellpadding="0" style="max-width:520px;margin:0 auto;background:#ffffff;border-radius:12px;padding:36px 32px;">
<tr><td>
<p style="margin:0 0 8px;color:#c2410c;font-size:12px;font-weight:600;letter-spacing:0.12em;text-transform:uppercase;">%s</p>
<h1 style="margin:0 0 16px;font-size:26px;line-height:1.2;font-weight:700;">Reset your password</h1>
<p style="margin:0 0 20px;font-size:15px;line-height:1.55;color:#44403c;">%s — someone (hopefully you) asked to reset the password for your %s account. The link below is valid for <strong>1 hour</strong>.</p>
<p style="margin:24px 0;"><a href="%s" style="display:inline-block;padding:12px 24px;background:#c2410c;color:#ffffff;text-decoration:none;border-radius:8px;font-weight:600;font-size:15px;">Reset password</a></p>
<p style="margin:0 0 4px;color:#78716c;font-size:13px;">Or paste this link into your browser:</p>
<p style="margin:0;word-break:break-all;"><a href="%s" style="color:#c2410c;font-size:13px;">%s</a></p>
<p style="margin:24px 0 0;color:#78716c;font-size:13px;line-height:1.5;">If you did not ask for this, ignore this email — your password will not change.</p>
</td></tr></table></body></html>`, business, greet, business, url, url, url)
}

func buildResetText(business, firstName, url string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s,\n\n", greeting(firstName))
	fmt.Fprintf(&b, "Someone (hopefully you) asked to reset the password for your %s account.\n", business)
	fmt.Fprintf(&b, "Open this link to set a new password (valid for 1 hour):\n\n%s\n\n", url)
	b.WriteString("If you did not ask for this, ignore this email — your password will not change.\n")
	return b.String()
}
