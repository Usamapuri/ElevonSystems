package models

import "elevon-backend/internal/dayops"

// OpenDayRequest starts today's business day. OpeningCash is a pointer
// because the declaration is the whole point of opening a day (spec §6.7):
// "0" is a real answer a person typed, an omitted field is not, and the
// handler refuses the second.
type OpenDayRequest struct {
	OpeningCash *float64 `json:"opening_cash"`
	Notes       string   `json:"notes"`
}

// CashMovementRequest is one paid-in or paid-out against the open day.
type CashMovementRequest struct {
	Type   string  `json:"type"`
	Amount float64 `json:"amount"`
	Reason string  `json:"reason"`
	Notes  string  `json:"notes"`
}

// CloseDayRequest closes the open day in one step. All three counts are
// required — enter 0 for a tender that took nothing — so a NULL counted
// column can only ever mean "force-closed, nobody counted".
type CloseDayRequest struct {
	CountedCash   *float64 `json:"counted_cash"`
	CountedCard   *float64 `json:"counted_card"`
	CountedOnline *float64 `json:"counted_online"`
	ClosingNotes  string   `json:"closing_notes"`
}

// ReopenDayRequest unseals a day against an admin PIN. DayID is optional:
// omitted, it means today's day, which is the case the owner hits after
// sealing and then spotting a miscount.
type ReopenDayRequest struct {
	Pin   string  `json:"pin"`
	DayID *string `json:"day_id"`
}

// ForceCloseDayRequest seals a day nobody counted, against an admin PIN and
// a written reason. DayID is optional: omitted, it means the day currently
// holding the open slot — normally a forgotten previous day.
type ForceCloseDayRequest struct {
	Pin    string  `json:"pin"`
	Reason string  `json:"reason"`
	DayID  *string `json:"day_id"`
}

// CurrentDayResponse is what the day-close screen loads on arrival. Day is
// null only when today has never been opened; after a close it is today's
// sealed row, so the screen can show "closed" instead of offering an Open
// button that would only 409.
type CurrentDayResponse struct {
	Day       *dayops.Day       `json:"day"`
	Expected  *dayops.Expected  `json:"expected"`
	Movements []dayops.Movement `json:"movements"`
}
