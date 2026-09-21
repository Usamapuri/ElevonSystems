package fiscal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// maxResponseBody caps what the client reads back from PRAL. The real
// envelopes are a few hundred bytes; anything past this is a gateway or a
// captive portal, not a validation result, and must not be buffered whole.
const maxResponseBody = 1 << 20 // 1 MiB

// ErrUnreachable is the retryable class: a transport failure, a timeout, a
// non-200 status or a body that is not the DI envelope. Every one of them
// comes back wrapped as "fiscal_unreachable: <cause>" and matches
// errors.Is(err, ErrUnreachable), so the outbound worker can tell "try again
// later" apart from "FBR said no".
var ErrUnreachable = errors.New("fiscal_unreachable")

// unknownRejectionCode stands in when FBR says statusCode "01" but names no
// error code, so a rejection always carries a code a person can look up.
const unknownRejectionCode = "unknown"

// LineStatus is one entry of validationResponse.invoiceStatuses. InvoiceNo
// and ErrorCode arrive as JSON null on rejections, which decodes into the
// zero string — the whole point of keeping these as plain strings.
type LineStatus struct {
	ItemSNo    string `json:"itemSNo"`
	StatusCode string `json:"statusCode"`
	InvoiceNo  string `json:"invoiceNo"`
	ErrorCode  string `json:"errorCode"`
	Error      string `json:"error"`
}

// Response is one PRAL reply, flattened. FBR answers HTTP 200 to everything,
// including rejections, so HTTPStatus alone says nothing — Rejection() is
// what decides whether a filing succeeded.
type Response struct {
	InvoiceNumber string
	StatusCode    string
	ErrorCode     string
	Error         string
	Lines         []LineStatus
	HTTPStatus    int
	Raw           json.RawMessage
}

// wireResponse is the envelope as PRAL sends it
// (audit/FBR_SANDBOX_2026-09-21.md). invoiceStatuses is null on an
// invoice-level rejection, which decodes to a nil slice.
type wireResponse struct {
	InvoiceNumber      string `json:"invoiceNumber"`
	Dated              string `json:"dated"`
	ValidationResponse struct {
		StatusCode      string       `json:"statusCode"`
		Status          string       `json:"status"`
		ErrorCode       string       `json:"errorCode"`
		Error           string       `json:"error"`
		InvoiceStatuses []LineStatus `json:"invoiceStatuses"`
	} `json:"validationResponse"`
}

// RejectedError is FBR refusing a document. Line 0 is an invoice-level
// refusal (invoiceStatuses null), 1-based otherwise.
//
// Error() is deliberately static in shape — the stable code and nothing
// else. The human sentence lives in Message, for the fiscal screen and the
// audit trail, so no upstream text can slip into an API error body.
type RejectedError struct {
	Code    string
	Message string
	Line    int
}

func (e *RejectedError) Error() string { return "fiscal_rejected " + e.Code }

// Rejection reports whether this response refused the document: nil when the
// envelope says "00" and every line says "00", otherwise the first error
// found, invoice level before line level.
func (r Response) Rejection() *RejectedError {
	if r.StatusCode != "00" && (r.ErrorCode != "" || r.Error != "") {
		return &RejectedError{Code: codeOrUnknown(r.ErrorCode), Message: r.Error}
	}
	for i, line := range r.Lines {
		if line.StatusCode == "00" {
			continue
		}
		return &RejectedError{
			Code:    codeOrUnknown(line.ErrorCode),
			Message: line.Error,
			Line:    lineNumber(line.ItemSNo, i),
		}
	}
	if r.StatusCode != "00" {
		return &RejectedError{Code: codeOrUnknown(r.ErrorCode), Message: r.Error}
	}
	return nil
}

func codeOrUnknown(code string) string {
	if code == "" {
		return unknownRejectionCode
	}
	return code
}

// lineNumber prefers FBR's own itemSNo and falls back to the 1-based
// position, so a missing or unparseable serial still points at a line.
func lineNumber(itemSNo string, idx int) int {
	if n, err := strconv.Atoi(itemSNo); err == nil && n > 0 {
		return n
	}
	return idx + 1
}

// Client is the PRAL Digital Invoicing API as this app uses it: validate,
// then post, plus a plain GET for the reference lists. Submit takes one of
// these rather than an *http.Client so a test can hand it a fake.
type Client interface {
	Validate(ctx context.Context, inv DIInvoice) (Response, error)
	Post(ctx context.Context, inv DIInvoice) (Response, error)
	Get(ctx context.Context, url string) ([]byte, error)
}

type httpClient struct {
	cfg   Config
	token string
	http  *http.Client
}

// NewHTTPClient builds the real client. The token is held in memory only: it
// goes into the Authorization header and nowhere else — never into an error,
// a log line or an audit excerpt.
//
// The client keeps net/http's default transport, so TLS verification stays
// on: a store's FBR token must never travel over a connection this process
// did not authenticate.
func NewHTTPClient(cfg Config, token string, timeout time.Duration) Client {
	return &httpClient{cfg: cfg, token: token, http: &http.Client{Timeout: timeout}}
}

func (c *httpClient) Validate(ctx context.Context, inv DIInvoice) (Response, error) {
	return c.send(ctx, c.cfg.ValidateURL(), inv)
}

func (c *httpClient) Post(ctx context.Context, inv DIInvoice) (Response, error) {
	return c.send(ctx, c.cfg.PostURL(), inv)
}

func (c *httpClient) Get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, unreachable(err)
	}
	c.authorize(req)
	body, _, err := c.do(req)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (c *httpClient) send(ctx context.Context, url string, inv DIInvoice) (Response, error) {
	payload, err := json.Marshal(inv)
	if err != nil {
		return Response{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return Response{}, unreachable(err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.authorize(req)

	body, status, err := c.do(req)
	if err != nil {
		return Response{}, err
	}
	return ParseResponse(body, status)
}

func (c *httpClient) authorize(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
}

// do performs the request and returns the body of a 200. Anything else is
// ErrUnreachable: PRAL answers 200 to every business outcome, so a non-200
// is the gateway talking, not a verdict on the invoice.
func (c *httpClient) do(req *http.Request) ([]byte, int, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, unreachable(requestError(err))
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, unreachable(fmt.Errorf("http %d", resp.StatusCode))
	}
	if readErr != nil {
		return nil, resp.StatusCode, unreachable(readErr)
	}
	return body, resp.StatusCode, nil
}

// ParseResponse reads a DI envelope leniently. PRAL pads its bodies with
// tabs and newlines, which encoding/json already tolerates; a body that is
// not the envelope at all is ErrUnreachable, because it means something
// other than PRAL answered.
func ParseResponse(body []byte, status int) (Response, error) {
	trimmed := bytes.TrimSpace(body)
	var wire wireResponse
	if err := json.Unmarshal(trimmed, &wire); err != nil {
		return Response{}, unreachable(errors.New("response is not a digital invoicing envelope"))
	}
	return Response{
		InvoiceNumber: wire.InvoiceNumber,
		StatusCode:    wire.ValidationResponse.StatusCode,
		ErrorCode:     wire.ValidationResponse.ErrorCode,
		Error:         wire.ValidationResponse.Error,
		Lines:         wire.ValidationResponse.InvoiceStatuses,
		HTTPStatus:    status,
		Raw:           json.RawMessage(trimmed),
	}, nil
}

// unreachable wraps a cause as the retryable class. The two %w verbs keep
// the message exactly "fiscal_unreachable: <cause>" while letting
// errors.Is(err, ErrUnreachable) match.
func unreachable(err error) error {
	return fmt.Errorf("%w: %w", ErrUnreachable, err)
}

// requestError strips the *url.Error wrapper so the cause reads plainly.
// The URL it carries holds no secret — the token travels in a header — but
// the unwrapped text is what belongs in an audit row.
func requestError(err error) error {
	var urlErr interface{ Unwrap() error }
	if errors.As(err, &urlErr) {
		if inner := urlErr.Unwrap(); inner != nil {
			return inner
		}
	}
	return err
}
