package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMaskKeys(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name:     "masks single field",
			input:    map[string]interface{}{"username": "alex", "password": "secret"},
			expected: map[string]interface{}{"username": "alex", "password": "***"},
		},
		{
			name:     "masks multiple fields",
			input:    map[string]interface{}{"card_number": "4111", "cvv": "123", "amount": 100},
			expected: map[string]interface{}{"card_number": "***", "cvv": "***", "amount": 100},
		},
		{
			name: "masks nested fields recursively",
			input: map[string]interface{}{
				"user": map[string]interface{}{
					"name":     "alex",
					"password": "secret",
				},
			},
			expected: map[string]interface{}{
				"user": map[string]interface{}{
					"name":     "alex",
					"password": "***",
				},
			},
		},
		{
			name: "masks fields inside array",
			input: []interface{}{
				map[string]interface{}{"cvv": "123", "number": "1234"},
				map[string]interface{}{"cvv": "456", "number": "5678"},
			},
			expected: []interface{}{
				map[string]interface{}{"cvv": "***", "number": "1234"},
				map[string]interface{}{"cvv": "***", "number": "5678"},
			},
		},
		{
			name:     "ignores non-matching fields",
			input:    map[string]interface{}{"username": "alex", "email": "a@b.com"},
			expected: map[string]interface{}{"username": "alex", "email": "a@b.com"},
		},
		{
			name:     "primitive value passthrough",
			input:    "just a string",
			expected: "just a string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Plugin{
				fields: map[string]struct{}{
					"password":    {},
					"card_number": {},
					"cvv":         {},
				},
			}

			got := p.maskKeys(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}
