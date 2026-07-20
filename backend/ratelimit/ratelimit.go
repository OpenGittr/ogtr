// Package ratelimit provides small in-memory rate limiters for abuse
// defense: a sliding-window limiter (link creation, abuse reports) and a
// 404-guess throttle for the public resolver.
//
// Deliberately per-instance (documented limitation): state lives in process
// memory, resets on restart, and is per-replica in Kubernetes — with N
// replicas a client effectively gets N× the limit. Acceptable for v1 abuse
// defense (the limits are generous for humans, tight for scripts); a shared
// store (e.g. Redis) is the future upgrade path if real deployments need
// exact global limits.
package ratelimit

import (
	"sync"
	"time"
)

// SlidingWindow allows at most limit events per key within a rolling
// window. Denied attempts are not counted — a client that keeps retrying
// while blocked is not punished further, it just stays at the limit until
// events age out.
type SlidingWindow struct {
	limit  int
	window time.Duration
	now    func() time.Time // injectable clock for tests

	mu        sync.Mutex
	hits      map[string][]time.Time
	lastSweep time.Time
}

// NewSlidingWindow builds a limiter: limit events per window per key.
func NewSlidingWindow(limit int, window time.Duration) *SlidingWindow {
	return &SlidingWindow{
		limit:  limit,
		window: window,
		now:    time.Now,
		hits:   map[string][]time.Time{},
	}
}

// Allow reports whether the key may perform another event now, recording
// the event when allowed. An empty key (no usable identity) is always
// allowed — the limiter never blocks on missing attribution.
func (l *SlidingWindow) Allow(key string) bool {
	if key == "" {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweep(now)

	kept := pruneBefore(l.hits[key], now.Add(-l.window))

	if len(kept) >= l.limit {
		l.hits[key] = kept

		return false
	}

	l.hits[key] = append(kept, now)

	return true
}

// sweep drops keys whose events have all aged out, bounding memory. Runs at
// most once per window.
func (l *SlidingWindow) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < l.window {
		return
	}

	l.lastSweep = now
	cutoff := now.Add(-l.window)

	for key, times := range l.hits {
		if kept := pruneBefore(times, cutoff); len(kept) == 0 {
			delete(l.hits, key)
		} else {
			l.hits[key] = kept
		}
	}
}

// GuessThrottle is the resolver's anti-enumeration defense: a client that
// produces more than limit unknown-code 404s inside the window is blocked
// for cooldown. Successful resolutions are never counted — only misses.
type GuessThrottle struct {
	limit    int
	window   time.Duration
	cooldown time.Duration
	now      func() time.Time

	mu           sync.Mutex
	misses       map[string][]time.Time
	blockedUntil map[string]time.Time
	lastSweep    time.Time
}

// NewGuessThrottle builds a throttle: more than limit misses per window
// puts the key in a cooldown block.
func NewGuessThrottle(limit int, window, cooldown time.Duration) *GuessThrottle {
	return &GuessThrottle{
		limit:        limit,
		window:       window,
		cooldown:     cooldown,
		now:          time.Now,
		misses:       map[string][]time.Time{},
		blockedUntil: map[string]time.Time{},
	}
}

// Blocked reports whether the key is currently in cooldown.
func (t *GuessThrottle) Blocked(key string) bool {
	if key == "" {
		return false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	until, ok := t.blockedUntil[key]
	if !ok {
		return false
	}

	if t.now().After(until) {
		delete(t.blockedUntil, key)
		delete(t.misses, key)

		return false
	}

	return true
}

// RecordMiss counts one unknown-code 404 for the key; crossing the limit
// starts the cooldown.
func (t *GuessThrottle) RecordMiss(key string) {
	if key == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.now()
	t.sweep(now)

	kept := append(pruneBefore(t.misses[key], now.Add(-t.window)), now)
	t.misses[key] = kept

	if len(kept) > t.limit {
		t.blockedUntil[key] = now.Add(t.cooldown)
	}
}

// sweep bounds memory like SlidingWindow.sweep; expired blocks go too.
func (t *GuessThrottle) sweep(now time.Time) {
	if now.Sub(t.lastSweep) < t.window {
		return
	}

	t.lastSweep = now
	cutoff := now.Add(-t.window)

	for key, times := range t.misses {
		if kept := pruneBefore(times, cutoff); len(kept) == 0 {
			delete(t.misses, key)
		} else {
			t.misses[key] = kept
		}
	}

	for key, until := range t.blockedUntil {
		if now.After(until) {
			delete(t.blockedUntil, key)
		}
	}
}

// pruneBefore drops timestamps at or before the cutoff (timestamps are
// appended in order, so the slice is sorted).
func pruneBefore(times []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for i < len(times) && !times[i].After(cutoff) {
		i++
	}

	return times[i:]
}
