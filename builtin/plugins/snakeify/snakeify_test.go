package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCamelToSnake(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "simple camel string", input: "simpleTest", expected: "simple_test"},
		{name: "verbose camel string", input: "camelCaseStringForTest", expected: "camel_case_string_for_test"},
		{name: "uppercase first letter", input: "Test", expected: "test"},
		{name: "already snake string", input: "already_snake", expected: "already_snake"},
		{name: "uppercase abbreviation prefix", input: "HTTPServerResponse", expected: "http_server_response"},
		{name: "uppercase abbreviation suffix", input: "userID", expected: "user_id"},
		{name: "empty string", input: "", expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := camelToSnake(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}
