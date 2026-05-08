package ratelimit

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRatelimit(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RateLimit Suite")
}
