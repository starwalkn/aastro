package main_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSnakeify(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Snakeify Suite")
}
