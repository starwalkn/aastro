//go:build integration

package integration

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRetry(t *testing.T) {
	t.Parallel()

	// max_retries: 2, backoff_delay: 50ms -> at least ~100ms on a persistent 429.
	start := time.Now()
	get(t, env.client, env.base+"/single-retry/1?status=429", nil)
	assert.GreaterOrEqual(t, time.Since(start), 90*time.Millisecond, "429 should be retried with backoff")
}

func TestTimeout(t *testing.T) {
	t.Parallel()

	start := time.Now()
	resp, _ := get(t, env.client, env.base+"/single-fast-timeout/1?delay=1s", nil)
	elapsed := time.Since(start)

	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	assert.GreaterOrEqual(t, elapsed, 300*time.Millisecond, "should wait for the 300ms timeout")
	assert.Less(t, elapsed, time.Second, "should not wait for the slow upstream")
}

// The breaker is stateful, so the steps run in order on a flow of their own
// (max_failures: 1, reset_timeout: 2s).
func TestCircuitBreaker(t *testing.T) {
	t.Parallel()

	const flow = "/single-breaker/1"

	expectStatus(t, flow+"?status=500", http.StatusBadGateway) // 5xx #1 opens the breaker
	expectStatus(t, flow+"?status=500", http.StatusBadGateway) // 5xx #2 short-circuits

	time.Sleep(2100 * time.Millisecond) // > reset_timeout
	expectStatus(t, flow, http.StatusOK)

	// 4xx is the client's problem, not the upstream's: the breaker stays closed.
	expectStatus(t, flow+"?status=404", http.StatusNotFound)
	expectStatus(t, flow+"?status=404", http.StatusNotFound)
	expectStatus(t, flow, http.StatusOK)
}
