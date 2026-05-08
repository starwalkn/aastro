package ratelimit

import (
	"sync"
	"time"
)

const (
	defaultLimit  = 60
	defaultWindow = 60 * time.Second
	cleanupEvery  = 10 * time.Second
)

type entry struct {
	currentCount  int
	previousCount int
	windowStart   time.Time
}

type RateLimit struct {
	limit   int
	window  time.Duration
	mu      sync.Mutex
	buckets map[string]*entry

	stopCh   chan struct{}
	stopOnce sync.Once

	now func() time.Time // injected for tests; defaults to time.Now
}

func New(cfg map[string]interface{}) *RateLimit {
	windowStr, _ := cfg["window"].(string)

	window, err := time.ParseDuration(windowStr)
	if err != nil {
		window = defaultWindow
	}

	return &RateLimit{
		limit:   intFrom(cfg, "limit", defaultLimit),
		window:  window,
		buckets: make(map[string]*entry),
		stopCh:  make(chan struct{}),
		now:     time.Now,
	}
}

func (rl *RateLimit) Start() error {
	go func() {
		ticker := time.NewTicker(cleanupEvery)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				rl.cleanup()
			case <-rl.stopCh:
				return
			}
		}
	}()

	return nil
}

func (rl *RateLimit) Stop() error {
	rl.stopOnce.Do(func() {
		close(rl.stopCh)
	})

	return nil
}

func (rl *RateLimit) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()

	ent, ok := rl.buckets[key]
	if !ok {
		ent = &entry{
			windowStart: now.Truncate(rl.window),
		}

		rl.buckets[key] = ent
	}

	elapsed := now.Sub(ent.windowStart)
	if elapsed >= rl.window {
		if elapsed >= 2*rl.window {
			ent.previousCount = 0
		} else {
			ent.previousCount = ent.currentCount
		}

		ent.currentCount = 0
		ent.windowStart = now.Truncate(rl.window)
	}

	elapsed = now.Sub(ent.windowStart)
	weight := 1.0 - float64(elapsed)/float64(rl.window)
	estimated := float64(ent.previousCount)*weight + float64(ent.currentCount)

	if int(estimated) >= rl.limit {
		return false
	}

	ent.currentCount++

	return true
}

func (rl *RateLimit) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := rl.now().Add(-2 * rl.window)

	for key, ent := range rl.buckets {
		if ent.windowStart.Before(cutoff) {
			delete(rl.buckets, key)
		}
	}
}

func intFrom(cfg map[string]interface{}, key string, def int) int {
	if v, ok := cfg[key]; ok {
		switch val := v.(type) {
		case float64:
			return int(val)
		case int:
			return val
		}
	}

	return def
}
