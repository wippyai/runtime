package handler

import (
	"strings"
	"testing"
)

func TestNewJsonValidator(t *testing.T) {
	testCases := []struct {
		name      string
		schema    any
		expectErr bool
	}{
		{
			name:      "valid string schema",
			schema:    `{"type": "object", "properties": {"name": {"type": "string"}}}`,
			expectErr: false,
		},
		{
			name:      "valid byte schema",
			schema:    []byte(`{"type": "object", "properties": {"name": {"type": "string"}}}`),
			expectErr: false,
		},
		{
			name: "valid object schema",
			schema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type": "string",
					},
				},
			},
			expectErr: false,
		},
		{
			name:      "invalid string schema",
			schema:    `{"type": "obj`,
			expectErr: true,
		},
		{
			name:      "invalid byte schema",
			schema:    []byte(`{"type": "obj`),
			expectErr: true,
		},
		{
			name:      "invalid object schema",
			schema:    make(chan int), // Using a channel to cause marshaling error
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newJsonValidator(tc.schema)
			if (err != nil) != tc.expectErr {
				t.Errorf("newJsonValidator() error = %v, expectErr %v", err, tc.expectErr)
			}
		})
	}
}

func TestJsonValidator_Validate(t *testing.T) {
	validSchema := `{"type": "object", "properties": {"name": {"type": "string"}, "age": {"type": "integer"}}, "required": ["name"]}`
	invalidSchema := `{"type": "obj`

	testCases := []struct {
		name      string
		schema    string
		data      []byte
		expectErr bool
		errMsg    string // Added for specific error message checks
	}{
		{
			name:      "valid data",
			schema:    validSchema,
			data:      []byte(`{"name": "John Doe", "age": 30}`),
			expectErr: false,
		},
		{
			name:      "missing required field",
			schema:    validSchema,
			data:      []byte(`{"age": 30}`),
			expectErr: true,
			errMsg:    "validation failed: (root): name is required",
		},
		{
			name:      "wrong data type",
			schema:    validSchema,
			data:      []byte(`{"name": "John Doe", "age": "30"}`),
			expectErr: true,
			errMsg:    "Expected: integer, given: string",
		},
		{
			name:      "invalid json data",
			schema:    validSchema,
			data:      []byte(`{"name": "John Doe", "age": 30`),
			expectErr: true,
		},
		{
			name:      "invalid schema",
			schema:    invalidSchema,
			data:      []byte(`{"name": "John Doe", "age": 30}`),
			expectErr: true,
		},
		{
			name:      "empty data",
			schema:    validSchema,
			data:      []byte(``),
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			validator, err := newJsonValidator(tc.schema)
			if err != nil {
				if !tc.expectErr {
					// we didn't expect an error but got one during validator creation
					t.Fatalf("newJsonValidator() error = %v, expected no error", err)
				} else {
					// we expected an error and got one, which is okay for this test case
					return
				}
			}

			err = validator.Validate(tc.data)
			if (err != nil) != tc.expectErr {
				t.Errorf("Validate() error = %v, expectErr %v", err, tc.expectErr)
			}

			if tc.expectErr && err != nil && tc.errMsg != "" {
				if strings.Contains(err.Error(), tc.errMsg) {
					return
				}

				t.Errorf("Validate() error = %v, expected error message: %v", err, tc.errMsg)
			}
		})
	}
}
