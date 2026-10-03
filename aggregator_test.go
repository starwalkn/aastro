package aastro

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestAggregator_MergeStrategy(t *testing.T) {
	agg := &defaultAggregator{}
	responses := []upstreamResponse{
		okResponse(`{"a":1,"b":2}`),
		okResponse(`{"b":99,"c":4}`),
	}

	t.Run("conflict policies", func(t *testing.T) {
		tests := []struct {
			name     string
			policy   conflictPolicy
			expected string
		}{
			{"overwrite uses last value", conflictPolicyOverwrite, `{"a":1,"b":99,"c":4}`},
			{"first preserves earliest value", conflictPolicyFirst, `{"a":1,"b":2,"c":4}`},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := agg.aggregate(nil, responses, aggregation{
					strategy:       strategyMerge,
					conflictPolicy: tt.policy,
				}, zap.NewNop())

				assert.Empty(t, result.errors)
				jsonEqual(t, tt.expected, result.data)
			})
		}
	})

	t.Run("returns conflict error when policy is error", func(t *testing.T) {
		result := agg.aggregate(nil, responses, aggregation{
			strategy:       strategyMerge,
			conflictPolicy: conflictPolicyError,
		}, zap.NewNop())

		assert.Nil(t, result.data)
		assert.ElementsMatch(t, []ClientError{ClientErrValueConflict}, result.errors)
	})

	t.Run("returns partial result when best effort and one upstream fails", func(t *testing.T) {
		result := agg.aggregate(
			nil,
			[]upstreamResponse{
				okResponse(`{"a":1}`),
				errResponse(upstreamTimeout),
			},
			aggregation{
				strategy:       strategyMerge,
				bestEffort:     true,
				conflictPolicy: conflictPolicyOverwrite,
			},
			zap.NewNop(),
		)

		assert.True(t, result.partial)
		assert.ElementsMatch(t, []ClientError{ClientErrUpstreamUnavailable}, result.errors)
		jsonEqual(t, `{"a":1}`, result.data)
	})
}

func TestAggregator_ArrayStrategy(t *testing.T) {
	agg := &defaultAggregator{}

	t.Run("aggregates responses into an array", func(t *testing.T) {
		result := agg.aggregate(
			nil,
			[]upstreamResponse{
				okResponse(`{"x":1}`),
				okResponse(`{"y":2}`),
			},
			aggregation{strategy: strategyArray},
			zap.NewNop(),
		)

		assert.Empty(t, result.errors)
		assert.False(t, result.partial)
		jsonEqual(t, `[{"x":1},{"y":2}]`, result.data)
	})

	t.Run("skips failed upstream when best effort", func(t *testing.T) {
		result := agg.aggregate(
			nil,
			[]upstreamResponse{
				okResponse(`{"x":1}`),
				errResponse(upstreamBadStatus),
			},
			aggregation{strategy: strategyArray, bestEffort: true},
			zap.NewNop(),
		)

		assert.True(t, result.partial)
		assert.ElementsMatch(t, []ClientError{ClientErrUpstreamError}, result.errors)
		jsonEqual(t, `[{"x":1}]`, result.data)
	})
}

func TestAggregator_NamespaceStrategy(t *testing.T) {
	agg := &defaultAggregator{}

	t.Run("groups responses by upstream name", func(t *testing.T) {
		result := agg.aggregate(
			mockUpstreams("users", "orders"),
			[]upstreamResponse{
				okResponse(`{"id":1}`),
				okResponse(`{"total":99}`),
			},
			aggregation{strategy: strategyNamespace},
			zap.NewNop(),
		)

		assert.Empty(t, result.errors)
		jsonEqual(t, `{"users":{"id":1},"orders":{"total":99}}`, result.data)
	})

	t.Run("omits failed upstream when best effort", func(t *testing.T) {
		result := agg.aggregate(
			mockUpstreams("users", "orders"),
			[]upstreamResponse{
				okResponse(`{"id":1}`),
				errResponse(upstreamTimeout),
			},
			aggregation{strategy: strategyNamespace, bestEffort: true},
			zap.NewNop(),
		)

		assert.True(t, result.partial)
		assert.Len(t, result.errors, 1)

		var got map[string]any
		require.NoError(t, decodeJSONInto(result.data, &got))
		assert.Contains(t, got, "users")
		assert.NotContains(t, got, "orders")
	})
}

func TestMapUpstreamError(t *testing.T) {
	tests := []struct {
		name string
		kind upstreamErrorKind
		want ClientError
	}{
		{"timeout → upstream unavailable", upstreamTimeout, ClientErrUpstreamUnavailable},
		{"connection → upstream unavailable", upstreamConnection, ClientErrUpstreamUnavailable},
		{"bad status → upstream error", upstreamBadStatus, ClientErrUpstreamError},
		{"body too large → upstream body too large", upstreamBodyTooLarge, ClientErrUpstreamBodyTooLarge},
		{"internal → internal", upstreamInternal, ClientErrInternal},
		{"read error → internal", upstreamReadError, ClientErrInternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapUpstreamError(&upstreamError{
				kind: tt.kind,
				err:  errors.New("err"),
			})
			assert.Equal(t, tt.want, got)
		})
	}
}
