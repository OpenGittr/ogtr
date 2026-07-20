package ratelimit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// fakeClock drives the injectable now() deterministically.
type fakeClock struct{ t time.Time }

func newFakeClock() *fakeClock { return &fakeClock{t: time.Unix(1_700_000_000, 0)} }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func TestSlidingWindow_WindowMath(t *testing.T) {
	clock := newFakeClock()
	l := NewSlidingWindow(3, time.Minute)
	l.now = clock.now

	// 3 allowed, 4th denied inside the window.
	assert.True(t, l.Allow("k"))
	assert.True(t, l.Allow("k"))
	assert.True(t, l.Allow("k"))
	assert.False(t, l.Allow("k"))

	// Denied attempts are not counted: still denied 30s later (3 events in
	// window), but the window *slides* — 31s after the first event, one slot
	// frees up.
	clock.advance(30 * time.Second)
	assert.False(t, l.Allow("k"))

	clock.advance(31 * time.Second) // first three events now older than 1m
	assert.True(t, l.Allow("k"))

	// Independent keys don't interfere.
	assert.True(t, l.Allow("other"))
}

func TestSlidingWindow_EmptyKeyAlwaysAllowed(t *testing.T) {
	l := NewSlidingWindow(1, time.Minute)

	assert.True(t, l.Allow(""))
	assert.True(t, l.Allow(""))
	assert.True(t, l.Allow(""))
}

func TestSlidingWindow_SweepDropsIdleKeys(t *testing.T) {
	clock := newFakeClock()
	l := NewSlidingWindow(3, time.Minute)
	l.now = clock.now

	l.Allow("idle")
	clock.advance(2 * time.Minute)
	l.Allow("active") // triggers the sweep

	l.mu.Lock()
	_, idleKept := l.hits["idle"]
	l.mu.Unlock()

	assert.False(t, idleKept, "idle key should be swept")
}

func TestGuessThrottle_BlocksOverLimitAndCoolsDown(t *testing.T) {
	clock := newFakeClock()
	g := NewGuessThrottle(5, time.Minute, 2*time.Minute)
	g.now = clock.now

	// At the limit: not blocked. One past it: blocked.
	for range 5 {
		g.RecordMiss("ip1")
	}

	assert.False(t, g.Blocked("ip1"), "exactly the limit is not over the limit")

	g.RecordMiss("ip1")
	assert.True(t, g.Blocked("ip1"))

	// Other keys unaffected; empty key never blocks.
	assert.False(t, g.Blocked("ip2"))
	assert.False(t, g.Blocked(""))

	// Still blocked inside the cooldown, released after it.
	clock.advance(time.Minute)
	assert.True(t, g.Blocked("ip1"))

	clock.advance(90 * time.Second)
	assert.False(t, g.Blocked("ip1"), "cooldown elapsed")

	// Misses aged out with the release — a single new miss doesn't re-block.
	g.RecordMiss("ip1")
	assert.False(t, g.Blocked("ip1"))
}

func TestGuessThrottle_WindowSlidesMissesOut(t *testing.T) {
	clock := newFakeClock()
	g := NewGuessThrottle(5, time.Minute, time.Minute)
	g.now = clock.now

	for range 5 {
		g.RecordMiss("ip")
	}

	// The old misses age out; 5 fresh ones still only reach the limit.
	clock.advance(2 * time.Minute)

	for range 5 {
		g.RecordMiss("ip")
	}

	assert.False(t, g.Blocked("ip"))
}
