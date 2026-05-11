package main

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Snakeify", func() {
	DescribeTable("camelToSnake",
		func(input, expected string) {
			got := camelToSnake(input)
			Expect(got).To(Equal(expected))
		},
		Entry("simple camel string", "simpleTest", "simple_test"),
		Entry("verbose camel string", "camelCaseStringForTest", "camel_case_string_for_test"),
		Entry("uppercase first letter", "Test", "test"),
		Entry("already snake string", "already_snake", "already_snake"),
		Entry("uppercase abbreviation prefix", "HTTPServerResponse", "http_server_response"),
		Entry("uppercase abbreviation suffix", "userID", "user_id"),
		Entry("empty string", "", ""),
	)
})
