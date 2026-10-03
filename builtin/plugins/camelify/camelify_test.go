package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSnakeToCamel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "simple snake string", input: "simple_test", expected: "simpleTest"},
		{name: "verbose snake string", input: "camel_case_string_for_test", expected: "camelCaseStringForTest"},
		{name: "already camel string", input: "alreadyCamel", expected: "alreadyCamel"},
		{name: "empty string", input: "", expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := snakeToCamel(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}
