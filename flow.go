package aastro

import (
	"github.com/starwalkn/aastro/sdk"
)

type flow struct {
	path   string
	method string

	// aggregation is only read when len(upstreams) > 1 - a single-upstream
	// flow is proxied directly (Router.buildProxyResponse) and never
	// aggregates, streaming or not. See Router.dispatch.
	aggregation aggregation
	upstreams   []upstream

	plugins     []sdk.Plugin
	middlewares []sdk.Middleware

	// streaming enables unbuffered proxy mode.
	// When true: only one upstream is allowed, aggregation is skipped, and
	// the response body is piped directly to the client (SSE-safe) - request
	// plugins still run, but response plugins do not (see handleStreaming).
	streaming bool
}

type aggregation struct {
	bestEffort        bool
	strategy          aggregationStrategy
	conflictPolicy    conflictPolicy // Conflict policy be set only for 'merge' aggregation strategy.
	preferredUpstream int            // Preferred upstream used only for 'prefer' conflict policy.
}

type aggregationStrategy uint8

const (
	strategyMerge aggregationStrategy = iota
	strategyArray
	strategyNamespace
)

func (s aggregationStrategy) String() string {
	switch s {
	case strategyArray:
		return "array"
	case strategyMerge:
		return "merge"
	case strategyNamespace:
		return "namespace"
	default:
		return "unknown"
	}
}

type conflictPolicy uint8

const (
	conflictPolicyOverwrite conflictPolicy = iota
	conflictPolicyError
	conflictPolicyFirst
	conflictPolicyPrefer
)

func (c conflictPolicy) String() string {
	switch c {
	case conflictPolicyOverwrite:
		return "overwrite"
	case conflictPolicyError:
		return "error"
	case conflictPolicyFirst:
		return "first"
	case conflictPolicyPrefer:
		return "prefer"
	default:
		return "unknown"
	}
}
