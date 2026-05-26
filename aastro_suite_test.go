package aastro

import (
	"fmt"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/starwalkn/aastro/internal/metric"
)

var testMetrics *metric.Metrics

func TestAastro(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Aastro Suite")
}

var _ = BeforeSuite(func() {
	tm, err := metric.New()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "init test metrics: %v\n", err)
		os.Exit(1)
	}

	testMetrics = tm
})
