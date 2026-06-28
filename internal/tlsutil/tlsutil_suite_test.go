package tlsutil_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTlsutil(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Tlsutil Suite")
}
