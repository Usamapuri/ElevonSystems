package fiscal

import (
	"context"
	"encoding/json"
	"strings"

	"elevon-backend/internal/models"

	"github.com/google/uuid"
)

// The Settings → FBR "Run test" (spec 2026-10-12 §2.7). It sends a one-line
// sample sale through the chosen environment and hands back everything the
// dialog shows: the exact outbound body, each gateway call's raw response,
// and the FBR number on a sandbox pass. A production test only validates —
// posting would file a real invoice against the store's NTN. Nothing a test
// does touches fiscal_invoices, invoices or the queue.

// TestStep is one gateway call.
type TestStep struct {
	Phase       string          `json:"phase"`
	URL         string          `json:"url"`
	HTTPStatus  int             `json:"http_status"`
	StatusCode  string          `json:"status_code"`
	ErrorCode   string          `json:"error_code"`
	Error       string          `json:"error"`
	Line        int             `json:"line"`
	RawResponse json.RawMessage `json:"raw_response"`
}

// TestResult is what POST /admin/fiscal/test returns. ErrorCode is set on a
// refusal made before any HTTP (no token, sandbox in release, no HS code)
// and on a rejection ("fiscal_rejected"); Steps carry FBR's own code.
type TestResult struct {
	OK            bool            `json:"ok"`
	Environment   Environment     `json:"environment"`
	Steps         []TestStep      `json:"steps"`
	RequestBody   json.RawMessage `json:"request_body"`
	InvoiceNumber string          `json:"invoice_number"`
	ErrorCode     string          `json:"error_code"`
}

// TestSample is what the sample sale is priced with: the cash tax rate and
// the default HS code, both read from settings by the caller.
type TestSample struct {
	TaxRate       float64
	DefaultHSCode string
}

const (
	testKind        = "test"
	testDescription = "Connection test"
	testValue       = 100.00
)

// RunTest never returns an error: every outcome is a result the dialog can
// show. It does not require cfg.Enabled — the test is how an admin checks
// the setup before switching filing on.
func RunTest(ctx context.Context, d Deps, env Environment, sample TestSample, actor *uuid.UUID) TestResult {
	res := TestResult{Environment: env, Steps: []TestStep{}}
	refuse := func(err error) TestResult {
		res.OK = false
		res.ErrorCode = ErrorCode(err)
		Record(d.DB, Event{Kind: testKind, Phase: PhaseRefused, Outcome: OutcomeRefused, ErrorCode: res.ErrorCode, CreatedBy: actor})
		return res
	}

	cfg, err := LoadConfig(d.DB)
	if err != nil {
		return refuse(err)
	}
	cfg.IsSandbox = env == EnvSandbox
	if err := cfg.CheckRuntime(); err != nil {
		return refuse(err)
	}
	token, err := LoadAPIKey(d.DB, env)
	if err != nil {
		return refuse(err)
	}
	if strings.TrimSpace(token) == "" {
		return refuse(ErrTokenMissing)
	}

	// One line, 1.000 kg at Rs 100, taxed at the cash rate — the same
	// shape as a real sale, so the response means something.
	tax := round2(testValue * sample.TaxRate)
	inv := models.Invoice{
		InvoiceNumber: "TEST",
		BusinessDate:  d.now(),
		TaxRate:       sample.TaxRate,
		Lines: []models.InvoiceLine{{
			ProductName: testDescription, Quantity: 1, UnitPrice: testValue, ListRate: testValue,
			LineTotal: testValue, LineDiscount: 0, LineTax: tax,
		}},
	}
	payload, err := BuildSaleInvoice(BuildInput{Invoice: inv, Buyer: nil, DefaultHSCode: sample.DefaultHSCode}, cfg)
	if err != nil {
		return refuse(err)
	}
	res.RequestBody = mustJSON(payload)

	cl := d.client(cfg, token)
	step, resp, err := runStep(ctx, d, cl.Validate, PhaseValidate, cfg.ValidateURL(), payload, actor)
	res.Steps = append(res.Steps, step)
	if err != nil {
		res.ErrorCode = ErrorCode(err)
		return res
	}
	if env == EnvProduction {
		res.OK = true
		return res
	}

	step, resp, err = runStep(ctx, d, cl.Post, PhasePost, cfg.PostURL(), payload, actor)
	res.Steps = append(res.Steps, step)
	if err != nil {
		res.ErrorCode = ErrorCode(err)
		return res
	}
	res.InvoiceNumber = strings.TrimSpace(resp.InvoiceNumber)
	res.OK = res.InvoiceNumber != ""
	if !res.OK {
		res.ErrorCode = ErrNoInvoiceNumber.Error()
	}
	return res
}

// runStep makes one call, records its audit row (kind test) and describes
// it. err is set for an unreachable gateway and for a rejection.
func runStep(ctx context.Context, d Deps, call func(context.Context, DIInvoice) (Response, error), phase, url string,
	payload DIInvoice, actor *uuid.UUID) (TestStep, Response, error) {
	step := TestStep{Phase: phase, URL: url}
	ev := Event{Kind: testKind, Phase: phase, RequestExcerpt: string(mustJSON(payload)), CreatedBy: actor}
	started := d.now()
	resp, err := call(ctx, payload)
	ev.LatencyMs = msSince(started, d.now())
	if err != nil {
		step.ErrorCode, step.Error = ErrorCode(err), "FBR could not be reached"
		ev.Outcome, ev.ErrorCode, ev.ResponseExcerpt = OutcomeUnreachable, step.ErrorCode, err.Error()
		Record(d.DB, ev)
		return step, Response{}, err
	}
	step.HTTPStatus, step.StatusCode, step.RawResponse = resp.HTTPStatus, resp.StatusCode, resp.Raw
	ev.HTTPStatus, ev.ResponseExcerpt = intPtr(resp.HTTPStatus), string(resp.Raw)
	if rejected := resp.Rejection(); rejected != nil {
		step.ErrorCode, step.Error, step.Line = rejected.Code, rejected.Message, rejected.Line
		ev.Outcome, ev.ErrorCode = OutcomeRejected, rejected.Code
		Record(d.DB, ev)
		return step, resp, rejected
	}
	ev.Outcome = OutcomeOK
	Record(d.DB, ev)
	return step, resp, nil
}
