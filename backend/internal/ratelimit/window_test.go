package ratelimit

import (
	"testing"
	"time"
)

func TestWindow_AllowStopsAtMaxAndRecoversAfterWindow(t *testing.T) {
	w := New(3, time.Minute)
	clock := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	w.now = func() time.Time { return clock }
	for i := 0; i < 3; i++ {
		if !w.Allow("k") {
			t.Fatalf("call %d must be allowed", i+1)
		}
	}
	if w.Allow("k") {
		t.Fatal("4th call inside the window must be refused")
	}
	if !w.Allow("other") {
		t.Fatal("keys are independent")
	}
	clock = clock.Add(61 * time.Second)
	if !w.Allow("k") {
		t.Fatal("window has slid past the old hits; must allow again")
	}
}

func TestWindow_CheckNeverConsumes_RecordDoes(t *testing.T) {
	w := New(2, time.Minute)
	for i := 0; i < 10; i++ {
		if !w.Check("k") {
			t.Fatal("Check must not consume the bucket")
		}
	}
	w.Record("k")
	w.Record("k")
	if w.Check("k") {
		t.Fatal("two recorded failures reach max=2; Check must refuse")
	}
}
