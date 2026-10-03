package ratelimit

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newWithClock(cfg map[string]interface{}, start time.Time) (*RateLimit, *fakeClock) {
	clock := &fakeClock{now: start}
	rl := New(cfg)
	rl.now = clock.Now
	return rl, clock
}

func TestRateLimitNew(t *testing.T) {
	t.Run("uses default window when config is empty", func(t *testing.T) {
		rl := New(map[string]interface{}{})

		assert.Equal(t, defaultWindow, rl.window)
		assert.Equal(t, defaultLimit, rl.limit)
	})

	t.Run("uses default window when window value is malformed", func(t *testing.T) {
		rl := New(map[string]interface{}{
			"window": "not-a-duration",
			"limit":  100,
		})

		assert.Equal(t, defaultWindow, rl.window)
		assert.Equal(t, 100, rl.limit)
	})

	t.Run("does not panic when window is missing", func(t *testing.T) {
		assert.NotPanics(t, func() {
			New(map[string]interface{}{"limit": 50})
		})
	})

	t.Run("parses valid window and limit", func(t *testing.T) {
		rl := New(map[string]interface{}{
			"window": "30s",
			"limit":  100,
		})

		assert.Equal(t, 30*time.Second, rl.window)
		assert.Equal(t, 100, rl.limit)
	})

	t.Run("accepts limit as float64 (YAML number type)", func(t *testing.T) {
		rl := New(map[string]interface{}{
			"window": "1m",
			"limit":  float64(42),
		})

		assert.Equal(t, 42, rl.limit)
	})
}

func TestRateLimitAllow(t *testing.T) {
	t.Run("within a single window", func(t *testing.T) {
		t.Run("permits requests up to the limit", func(t *testing.T) {
			start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
			rl, _ := newWithClock(map[string]interface{}{
				"window": "1m",
				"limit":  10,
			}, start)

			for range 10 {
				assert.True(t, rl.Allow("client-a"))
			}
		})

		t.Run("rejects the request that exceeds the limit", func(t *testing.T) {
			start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
			rl, _ := newWithClock(map[string]interface{}{
				"window": "1m",
				"limit":  3,
			}, start)

			assert.True(t, rl.Allow("client-a"))
			assert.True(t, rl.Allow("client-a"))
			assert.True(t, rl.Allow("client-a"))
			assert.False(t, rl.Allow("client-a"))
		})
	})

	t.Run("with separate keys", func(t *testing.T) {
		t.Run("tracks each key independently", func(t *testing.T) {
			start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
			rl, _ := newWithClock(map[string]interface{}{
				"window": "1m",
				"limit":  2,
			}, start)

			assert.True(t, rl.Allow("client-a"))
			assert.True(t, rl.Allow("client-b"))
			assert.True(t, rl.Allow("client-a"))
			assert.True(t, rl.Allow("client-b"))
			assert.False(t, rl.Allow("client-a"))
			assert.False(t, rl.Allow("client-b"))
		})
	})

	t.Run("across window boundaries", func(t *testing.T) {
		t.Run("smoothly transitions when crossing into the next window", func(t *testing.T) {
			start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
			rl, clock := newWithClock(map[string]interface{}{
				"window": "1m",
				"limit":  10,
			}, start)

			for range 10 {
				assert.True(t, rl.Allow("client-a"))
			}
			assert.False(t, rl.Allow("client-a"))

			clock.Advance(1 * time.Minute)

			assert.False(t, rl.Allow("client-a"))
		})

		t.Run("rejects burst at boundary that would pass with fixed window", func(t *testing.T) {
			start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
			rl, clock := newWithClock(map[string]interface{}{
				"window": "1m",
				"limit":  10,
			}, start)

			clock.Advance(50 * time.Second)
			for range 10 {
				assert.True(t, rl.Allow("client-a"))
			}

			clock.Advance(15 * time.Second)

			rejected := 0
			for range 10 {
				if !rl.Allow("client-a") {
					rejected++
				}
			}

			assert.Positive(t, rejected)
		})

		t.Run("forgets previous window completely after two full windows", func(t *testing.T) {
			start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
			rl, clock := newWithClock(map[string]interface{}{
				"window": "1m",
				"limit":  10,
			}, start)

			for range 10 {
				assert.True(t, rl.Allow("client-a"))
			}

			clock.Advance(2*time.Minute + time.Second)

			for range 10 {
				assert.True(t, rl.Allow("client-a"))
			}
		})
	})
}

func TestRateLimitCleanup(t *testing.T) {
	t.Run("removes entries that became stale", func(t *testing.T) {
		start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
		rl, clock := newWithClock(map[string]interface{}{
			"window": "1m",
			"limit":  10,
		}, start)

		rl.Allow("stale-client")
		assert.Contains(t, rl.buckets, "stale-client")

		clock.Advance(3 * time.Minute)

		rl.cleanup()

		assert.NotContains(t, rl.buckets, "stale-client")
	})

	t.Run("preserves entries within two-window window", func(t *testing.T) {
		start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
		rl, clock := newWithClock(map[string]interface{}{
			"window": "1m",
			"limit":  10,
		}, start)

		rl.Allow("recent-client")

		clock.Advance(90 * time.Second)

		rl.cleanup()

		assert.Contains(t, rl.buckets, "recent-client")
	})

	t.Run("preserves entries that were just rolled into a new window", func(t *testing.T) {
		start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
		rl, clock := newWithClock(map[string]interface{}{
			"window": "1m",
			"limit":  10,
		}, start)

		rl.Allow("active-client")

		clock.Advance(70 * time.Second)
		rl.Allow("active-client")

		rl.cleanup()

		assert.Contains(t, rl.buckets, "active-client")
	})
}

func TestRateLimitStop(t *testing.T) {
	t.Run("can be called multiple times without panicking", func(t *testing.T) {
		rl := New(map[string]interface{}{"window": "1s", "limit": 1})

		require.NoError(t, rl.Stop())
		assert.NotPanics(t, func() { _ = rl.Stop() })
		assert.NotPanics(t, func() { _ = rl.Stop() })
	})

	t.Run("stops the cleanup goroutine", func(t *testing.T) {
		rl := New(map[string]interface{}{"window": "1s", "limit": 1})
		require.NoError(t, rl.Start())

		require.NoError(t, rl.Stop())

		assert.Eventually(t, func() bool {
			select {
			case <-rl.stopCh:
				return true
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond)
	})
}
