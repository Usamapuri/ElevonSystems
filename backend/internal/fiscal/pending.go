package fiscal

import "time"

// PendingSummary is "how much is unfiled" (spec 2026-10-12 §2.4): completed
// invoices in unfiled or failed. It is information for the till and the
// dashboard and nothing else — no code path reads it to warn or to block.
type PendingSummary struct {
	Count        int     `json:"count"`
	Kg           float64 `json:"kg"`
	TotalPayable int64   `json:"total_payable"`
}

// Pending returns the summary for one date and for every date. "Today" is by
// invoice_date, the document date, so it agrees with what the File with FBR
// screen lists for that day (spec 2026-10-12 §8.2).
func Pending(q Querier, businessDate time.Time) (today, all PendingSummary, err error) {
	err = q.QueryRow(`
		SELECT count(*) FILTER (WHERE i.invoice_date = $1::date),
		       COALESCE(sum(k.kg) FILTER (WHERE i.invoice_date = $1::date), 0)::float8,
		       COALESCE(sum(i.total_payable) FILTER (WHERE i.invoice_date = $1::date), 0)::bigint,
		       count(*), COALESCE(sum(k.kg), 0)::float8, COALESCE(sum(i.total_payable), 0)::bigint
		FROM invoices i
		JOIN LATERAL (SELECT COALESCE(sum(quantity), 0) AS kg FROM invoice_lines WHERE invoice_id = i.id) k ON true
		WHERE i.status = 'completed' AND i.fiscal_status IN ('unfiled','failed')`,
		businessDate.Format("2006-01-02")).
		Scan(&today.Count, &today.Kg, &today.TotalPayable, &all.Count, &all.Kg, &all.TotalPayable)
	return
}
