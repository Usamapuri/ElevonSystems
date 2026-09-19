package util

import (
	"testing"
	"time"
)

func TestBusinessDate_BoundaryHourShiftsEarlyMorningToPreviousDay(t *testing.T) {
	SetDayBoundaryHour(2)
	defer SetDayBoundaryHour(0)
	loc := BusinessLocation()
	at := time.Date(2026, 9, 19, 0, 30, 0, 0, loc) // 00:30 with a 2 AM boundary
	got := BusinessDate(at)
	want := time.Date(2026, 9, 18, 0, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("BusinessDate = %v, want %v", got, want)
	}
}

func TestBusinessDate_DefaultBoundaryIsMidnight(t *testing.T) {
	SetDayBoundaryHour(0)
	loc := BusinessLocation()
	at := time.Date(2026, 9, 19, 0, 30, 0, 0, loc)
	if got := BusinessDate(at); got.Day() != 19 {
		t.Fatalf("with boundary 0, 00:30 must stay on the 19th, got %v", got)
	}
}

func TestSetDayBoundaryHour_RejectsOutOfRange(t *testing.T) {
	SetDayBoundaryHour(0)
	SetDayBoundaryHour(13)
	if DayBoundaryHour() != 0 {
		t.Fatalf("out-of-range hour must be ignored, got %d", DayBoundaryHour())
	}
}

func TestBusinessTimezoneName(t *testing.T) {
	if BusinessTimezoneName() != "Asia/Karachi" {
		t.Fatal("business timezone must be Asia/Karachi")
	}
}
