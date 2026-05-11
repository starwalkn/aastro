package main_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRecoverer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Recoverer Suite")
}
