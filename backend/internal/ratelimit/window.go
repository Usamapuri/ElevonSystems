// Package ratelimit is a process-local sliding-window limiter. One backend
// per store, so in-memory buckets are enough (spec §6.1).
package ratelimit

import (
	"sync"
	"time"
)

// Window counts events per key inside a sliding window.
//
//   - Allow(key): check and record in one step; every call counts. Use where
//     the limit must hold regardless of outcome (forgot-password).
//   - Check(key) then Record(key): count only failures (login), so a user who
//     typos twice and then signs in is not penalised.
type Window struct {
	mu      sync.Mutex
	buckets map[string][]time.Time
	max     int
	window  time.Duration
	now     func() time.Time // swapped in tests
}

// New builds a limiter allowing max events per key per window.
func New(max int, window time.Duration) *Window {
	return &Window{buckets: map[string][]time.Time{}, max: max, window: window, now: time.Now}
}

// prune drops hits older than the window. Caller holds mu.
func (w *Window) prune(key string, now time.Time) []time.Time {
	cutoff := now.Add(-w.window)
	kept := w.buckets[key][:0]
	for _, t := range w.buckets[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(w.buckets, key)
		return nil
	}
	w.buckets[key] = kept
	return kept
}

// Allow reports whether key is under the limit and records the hit if so.
func (w *Window) Allow(key string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	if len(w.prune(key, now)) >= w.max {
		return false
	}
	w.buckets[key] = append(w.buckets[key], now)
	return true
}

// Check reports whether key is under the limit without recording anything.
func (w *Window) Check(key string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.prune(key, w.now())) < w.max
}

// Record adds a hit for key (call after a failure).
func (w *Window) Record(key string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	w.prune(key, now)
	w.buckets[key] = append(w.buckets[key], now)
}
