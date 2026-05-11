package ratelimit

import (
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

var _ = Describe("RateLimit", func() {
	Describe("New", func() {
		It("uses default window when config is empty", func() {
			rl := New(map[string]interface{}{})

			Expect(rl.window).To(Equal(defaultWindow))
			Expect(rl.limit).To(Equal(defaultLimit))
		})

		It("uses default window when window value is malformed", func() {
			rl := New(map[string]interface{}{
				"window": "not-a-duration",
				"limit":  100,
			})

			Expect(rl.window).To(Equal(defaultWindow))
			Expect(rl.limit).To(Equal(100))
		})

		It("does not panic when window is missing", func() {
			Expect(func() {
				New(map[string]interface{}{"limit": 50})
			}).NotTo(Panic())
		})

		It("parses valid window and limit", func() {
			rl := New(map[string]interface{}{
				"window": "30s",
				"limit":  100,
			})

			Expect(rl.window).To(Equal(30 * time.Second))
			Expect(rl.limit).To(Equal(100))
		})

		It("accepts limit as float64 (YAML number type)", func() {
			rl := New(map[string]interface{}{
				"window": "1m",
				"limit":  float64(42),
			})

			Expect(rl.limit).To(Equal(42))
		})
	})

	Describe("Allow", func() {
		Context("within a single window", func() {
			It("permits requests up to the limit", func() {
				start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
				rl, _ := newWithClock(map[string]interface{}{
					"window": "1m",
					"limit":  10,
				}, start)

				for range 10 {
					Expect(rl.Allow("client-a")).To(BeTrue())
				}
			})

			It("rejects the request that exceeds the limit", func() {
				start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
				rl, _ := newWithClock(map[string]interface{}{
					"window": "1m",
					"limit":  3,
				}, start)

				Expect(rl.Allow("client-a")).To(BeTrue())
				Expect(rl.Allow("client-a")).To(BeTrue())
				Expect(rl.Allow("client-a")).To(BeTrue())
				Expect(rl.Allow("client-a")).To(BeFalse())
			})
		})

		Context("with separate keys", func() {
			It("tracks each key independently", func() {
				start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
				rl, _ := newWithClock(map[string]interface{}{
					"window": "1m",
					"limit":  2,
				}, start)

				Expect(rl.Allow("client-a")).To(BeTrue())
				Expect(rl.Allow("client-b")).To(BeTrue())
				Expect(rl.Allow("client-a")).To(BeTrue())
				Expect(rl.Allow("client-b")).To(BeTrue())
				Expect(rl.Allow("client-a")).To(BeFalse())
				Expect(rl.Allow("client-b")).To(BeFalse())
			})
		})

		Context("across window boundaries", func() {
			It("smoothly transitions when crossing into the next window", func() {
				start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
				rl, clock := newWithClock(map[string]interface{}{
					"window": "1m",
					"limit":  10,
				}, start)

				for range 10 {
					Expect(rl.Allow("client-a")).To(BeTrue())
				}
				Expect(rl.Allow("client-a")).To(BeFalse())

				clock.Advance(1 * time.Minute)

				Expect(rl.Allow("client-a")).To(BeFalse())
			})

			It("rejects burst at boundary that would pass with fixed window", func() {
				start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
				rl, clock := newWithClock(map[string]interface{}{
					"window": "1m",
					"limit":  10,
				}, start)

				clock.Advance(50 * time.Second)
				for range 10 {
					Expect(rl.Allow("client-a")).To(BeTrue())
				}

				clock.Advance(15 * time.Second)

				rejected := 0
				for range 10 {
					if !rl.Allow("client-a") {
						rejected++
					}
				}

				Expect(rejected).To(BeNumerically(">", 0))
			})

			It("forgets previous window completely after two full windows", func() {
				start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
				rl, clock := newWithClock(map[string]interface{}{
					"window": "1m",
					"limit":  10,
				}, start)

				for range 10 {
					Expect(rl.Allow("client-a")).To(BeTrue())
				}

				clock.Advance(2*time.Minute + time.Second)

				for range 10 {
					Expect(rl.Allow("client-a")).To(BeTrue())
				}
			})
		})
	})

	Describe("cleanup", func() {
		It("removes entries that became stale", func() {
			start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
			rl, clock := newWithClock(map[string]interface{}{
				"window": "1m",
				"limit":  10,
			}, start)

			rl.Allow("stale-client")
			Expect(rl.buckets).To(HaveKey("stale-client"))

			clock.Advance(3 * time.Minute)

			rl.cleanup()

			Expect(rl.buckets).NotTo(HaveKey("stale-client"))
		})

		It("preserves entries within two-window window", func() {
			start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
			rl, clock := newWithClock(map[string]interface{}{
				"window": "1m",
				"limit":  10,
			}, start)

			rl.Allow("recent-client")

			clock.Advance(90 * time.Second)

			rl.cleanup()

			Expect(rl.buckets).To(HaveKey("recent-client"))
		})

		It("preserves entries that were just rolled into a new window", func() {
			start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
			rl, clock := newWithClock(map[string]interface{}{
				"window": "1m",
				"limit":  10,
			}, start)

			rl.Allow("active-client")

			clock.Advance(70 * time.Second)
			rl.Allow("active-client")

			rl.cleanup()

			Expect(rl.buckets).To(HaveKey("active-client"))
		})
	})

	Describe("Stop", func() {
		It("can be called multiple times without panicking", func() {
			rl := New(map[string]interface{}{"window": "1s", "limit": 1})

			Expect(rl.Stop()).To(Succeed())
			Expect(func() { _ = rl.Stop() }).NotTo(Panic())
			Expect(func() { _ = rl.Stop() }).NotTo(Panic())
		})

		It("stops the cleanup goroutine", func() {
			rl := New(map[string]interface{}{"window": "1s", "limit": 1})
			Expect(rl.Start()).To(Succeed())

			Expect(rl.Stop()).To(Succeed())

			Eventually(func() bool {
				select {
				case <-rl.stopCh:
					return true
				default:
					return false
				}
			}).Should(BeTrue())
		})
	})
})
