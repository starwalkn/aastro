package main

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Masker", func() {
	DescribeTable("maskKeys",
		func(input, expected interface{}) {
			p := &Plugin{
				fields: map[string]struct{}{
					"password":    {},
					"card_number": {},
					"cvv":         {},
				},
			}

			got := p.maskKeys(input)
			Expect(got).To(Equal(expected))
		},
		Entry("masks single field",
			map[string]interface{}{"username": "alex", "password": "secret"},
			map[string]interface{}{"username": "alex", "password": "***"},
		),
		Entry("masks multiple fields",
			map[string]interface{}{"card_number": "4111", "cvv": "123", "amount": 100},
			map[string]interface{}{"card_number": "***", "cvv": "***", "amount": 100},
		),
		Entry("masks nested fields recursively",
			map[string]interface{}{
				"user": map[string]interface{}{
					"name":     "alex",
					"password": "secret",
				},
			},
			map[string]interface{}{
				"user": map[string]interface{}{
					"name":     "alex",
					"password": "***",
				},
			},
		),
		Entry("masks fields inside array",
			[]interface{}{
				map[string]interface{}{"cvv": "123", "number": "1234"},
				map[string]interface{}{"cvv": "456", "number": "5678"},
			},
			[]interface{}{
				map[string]interface{}{"cvv": "***", "number": "1234"},
				map[string]interface{}{"cvv": "***", "number": "5678"},
			},
		),
		Entry("ignores non-matching fields",
			map[string]interface{}{"username": "alex", "email": "a@b.com"},
			map[string]interface{}{"username": "alex", "email": "a@b.com"},
		),
		Entry("primitive value passthrough", "just a string", "just a string"),
	)
})
