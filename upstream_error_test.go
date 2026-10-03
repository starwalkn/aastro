package aastro

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// This is the property init() already enforces at startup (it panics on
// a missing entry) - asserting it here too gives a readable failure in
// `go test` instead of only a panic trace, and documents the invariant
// next to the other kindProps-consumer tests below.
func TestKindTable_HasEntryForEveryKind(t *testing.T) {
	for _, k := range allUpstreamErrorKinds {
		_, ok := kindTable[k]
		assert.True(t, ok, "missing kindTable entry for %q", k)
	}
}

func TestKindTable_UnknownKindFallsBackToInternalError(t *testing.T) {
	got := upstreamErrorKind("some_future_kind").props()

	assert.Equal(t, breakerNoSignal, got.breaker)
	assert.Equal(t, retryNever, got.retry)
	assert.False(t, got.propagatable)
	assert.Equal(t, ClientErrInternal, got.clientErr)
}

func TestIsPropagatable(t *testing.T) {
	tests := []struct {
		name string
		kind upstreamErrorKind
		want bool
	}{
		{"client error is propagatable", upstreamClientError, true},
		{"redirect is propagatable", upstreamRedirect, true},
		{"bad status is not propagatable", upstreamBadStatus, false},
		{"timeout is not propagatable", upstreamTimeout, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isPropagatable(tt.kind))
		})
	}
}
