package main

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Camelify", func() {
	DescribeTable("snakeToCamel",
		func(input, expected string) {
			got := snakeToCamel(input)
			Expect(got).To(Equal(expected))
		},
		Entry("simple snake string", "simple_test", "simpleTest"),
		Entry("verbose snake string", "camel_case_string_for_test", "camelCaseStringForTest"),
		Entry("already camel string", "alreadyCamel", "alreadyCamel"),
		Entry("empty string", "", ""),
	)
})
