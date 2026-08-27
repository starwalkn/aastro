package aastro

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("kindTable", func() {
	// This is the property init() already enforces at startup (it panics on
	// a missing entry) - asserting it here too gives a readable failure in
	// `go test` instead of only a panic trace, and documents the invariant
	// next to the other kindProps-consumer tests below.
	It("has an entry for every upstreamErrorKind constant", func() {
		for _, k := range allUpstreamErrorKinds {
			_, ok := kindTable[k]
			Expect(ok).To(BeTrue(), "missing kindTable entry for %q", k)
		}
	})

	It("falls back to a safe internal-error classification for an unknown kind", func() {
		got := upstreamErrorKind("some_future_kind").props()

		Expect(got.breaker).To(Equal(breakerNoSignal))
		Expect(got.retry).To(Equal(retryNever))
		Expect(got.propagatable).To(BeFalse())
		Expect(got.clientErr).To(Equal(ClientErrInternal))
	})

	DescribeTable("isPropagatable",
		func(kind upstreamErrorKind, want bool) {
			Expect(isPropagatable(kind)).To(Equal(want))
		},
		Entry("client error is propagatable", upstreamClientError, true),
		Entry("redirect is propagatable", upstreamRedirect, true),
		Entry("bad status is not propagatable", upstreamBadStatus, false),
		Entry("timeout is not propagatable", upstreamTimeout, false),
	)
})
