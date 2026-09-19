// Package util holds small, dependency-free helpers used across the backend.
package util

import (
	"sync"
	"time"
)

// BusinessTimezone is the only place this string lives. The DB connection
// layer pins every Postgres session to the same zone (database.connection.go)
// so SQL casts and Go formatting agree on what "midnight" means.
const BusinessTimezone = "Asia/Karachi"

var (
	businessLocOnce sync.Once
	businessLoc     *time.Location

	dayBoundaryMu   sync.RWMutex
	dayBoundaryHour = 0 // LPG shop default: midnight. Overridden from settings.day_boundary_hour at boot.
)

// BusinessLocation returns the business *time.Location, cached. Falls back to
// a fixed UTC+5 zone if the container has no tzdata.
func BusinessLocation() *time.Location {
	businessLocOnce.Do(func() {
		if loc, err := time.LoadLocation(BusinessTimezone); err == nil {
			businessLoc = loc
			return
		}
		businessLoc = time.FixedZone("PKT", 5*60*60)
	})
	return businessLoc
}

// BusinessTimezoneName returns the IANA name in use.
func BusinessTimezoneName() string { return BusinessTimezone }

// SetDayBoundaryHour sets the hour (0–12) at which a new business day starts.
// Out-of-range values are ignored so a bad setting can never shift reports.
func SetDayBoundaryHour(hour int) {
	if hour < 0 || hour > 12 {
		return
	}
	dayBoundaryMu.Lock()
	dayBoundaryHour = hour
	dayBoundaryMu.Unlock()
}

// DayBoundaryHour returns the configured boundary hour.
func DayBoundaryHour() int {
	dayBoundaryMu.RLock()
	defer dayBoundaryMu.RUnlock()
	return dayBoundaryHour
}

// BusinessNow is time.Now() in the business timezone.
func BusinessNow() time.Time { return time.Now().In(BusinessLocation()) }

// BusinessDate returns the business day (midnight, business tz) that t falls
// in: convert to business time, subtract the boundary hour, truncate.
func BusinessDate(t time.Time) time.Time {
	local := t.In(BusinessLocation())
	shifted := local.Add(-time.Duration(DayBoundaryHour()) * time.Hour)
	return time.Date(shifted.Year(), shifted.Month(), shifted.Day(), 0, 0, 0, 0, BusinessLocation())
}
