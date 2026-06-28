package certwatcher_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCertwatcher(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Certwatcher Suite")
}
