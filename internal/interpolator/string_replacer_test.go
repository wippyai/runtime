package interpolator

import (
	"errors"
	"reflect"
	"testing"
)

// TestReplaceString tests the basic string replacement functionality.
func TestReplaceString(t *testing.T) {
	tests := []struct {
		expectedError  error
		name           string
		input          string
		replacement    string
		expectedOutput string
	}{
		{
			name:           "simple replacement",
			input:          "hello world",
			replacement:    "HELLO WORLD",
			expectedOutput: "HELLO WORLD",
			expectedError:  nil,
		},
		{
			name:           "no replacement",
			input:          "hello world",
			replacement:    "",
			expectedOutput: "hello world",
			expectedError:  nil,
		},
		{
			name:           "error case",
			input:          "error",
			replacement:    "",
			expectedOutput: "",
			expectedError:  errors.New("forced error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var replacer *replacer
			if tt.name == "error case" {
				replacer = newStringReplacer(func(string, any) (string, error) {
					return "", errors.New("forced error")
				})
			} else {
				replacer = newStringReplacer(func(s string, _ any) (string, error) {
					if s == tt.input && tt.replacement != "" {
						return tt.replacement, nil
					}
					return s, nil
				})
			}

			result, err := replacer.replaceString(tt.input, nil) // Pass nil context
			if err != nil && tt.expectedError != nil {
				if err.Error() != tt.expectedError.Error() {
					t.Errorf("replaceString() error = %v, expectedError %v", err, tt.expectedError)
					return
				}
			} else if !errors.Is(err, tt.expectedError) {
				t.Errorf("replaceString() error = %v, expectedError %v", err, tt.expectedError)
				return
			}
			if result != tt.expectedOutput {
				t.Errorf("replaceString() = %v, expected %v", result, tt.expectedOutput)
			}
		})
	}
}

// TestReplace tests the Replace method with various data structures.
func TestReplace(t *testing.T) {
	tests := []struct {
		input           any
		expectedOutput  any
		expectedError   error
		replacementFunc stringReplacer
		name            string
	}{
		{
			name:  "simple string",
			input: "hello world",
			replacementFunc: func(string, any) (string, error) {
				return "HELLO WORLD", nil
			},
			expectedOutput: "HELLO WORLD",
			expectedError:  nil,
		},
		{
			name: "map of strings",
			input: map[string]string{
				"key1": "hello",
				"key2": "world",
			},
			replacementFunc: func(s string, _ any) (string, error) {
				switch s {
				case "key1":
					return "keyA", nil
				case "key2":
					return "keyB", nil
				case "hello":
					return "valueA", nil
				case "world":
					return "valueB", nil
				default:
					return s, nil
				}
			},
			expectedOutput: map[string]string{
				"keyA": "valueA",
				"keyB": "valueB",
			},
			expectedError: nil,
		},
		{
			name: "map of any",
			input: map[any]any{
				"key1": "hello",
				2:      "world",
				"key3": map[string]string{
					"nestedKey": "nestedValue",
				},
			},
			replacementFunc: func(s string, _ any) (string, error) {
				switch s {
				case "key1":
					return "keyA", nil
				case "key3":
					return "keyC", nil
				case "nestedKey":
					return "nestedKeyA", nil
				case "nestedValue":
					return "NESTED_VALUE", nil
				case "hello":
					return "valueA", nil
				case "world":
					return "valueB", nil
				default:
					return "REPLACED", nil
				}
			},
			expectedOutput: map[any]any{
				"keyA": "valueA",
				2:      "valueB",
				"keyC": map[string]string{
					"nestedKeyA": "NESTED_VALUE",
				},
			},
			expectedError: nil,
		},
		{
			name: "slice of strings",
			input: []string{
				"hello",
				"world",
			},
			replacementFunc: func(_ string, _ any) (string, error) {
				return "VALUE", nil
			},
			expectedOutput: []string{
				"VALUE",
				"VALUE",
			},
			expectedError: nil,
		},
		{
			name: "array of strings",
			input: [2]string{
				"hello",
				"world",
			},
			replacementFunc: func(_ string, _ any) (string, error) {
				return "VALUE", nil
			},
			expectedOutput: [2]string{
				"VALUE",
				"VALUE",
			},
			expectedError: nil,
		},
		{
			name: "struct with strings",
			input: struct {
				Field1 string
				Field2 string
				field4 string
				Field3 int
			}{
				Field1: "hello",
				Field2: "world",
				Field3: 123,
				field4: "unexported",
			},
			replacementFunc: func(string, any) (string, error) {
				return "VALUE", nil
			},
			// Modify expectedOutput to have empty unexported field since it can't be accessed via reflection
			expectedOutput: struct {
				Field1 string
				Field2 string
				field4 string
				Field3 int
			}{
				Field1: "VALUE",
				Field2: "VALUE",
				Field3: 123,
				field4: "", // This will be the zero value since we can't access it
			},
			expectedError: nil,
		},
		{
			name: "nested struct",
			input: struct {
				Field1 string
				Field2 struct {
					NestedField string
				}
			}{
				Field1: "hello",
				Field2: struct {
					NestedField string
				}{
					NestedField: "world",
				},
			},
			replacementFunc: func(string, any) (string, error) {
				return "VALUE", nil
			},
			expectedOutput: struct {
				Field1 string
				Field2 struct {
					NestedField string
				}
			}{
				Field1: "VALUE",
				Field2: struct {
					NestedField string
				}{
					NestedField: "VALUE",
				},
			},
			expectedError: nil,
		},
		{
			name: "pointer to string",
			input: func() *string {
				s := "hello"
				return &s
			}(),
			replacementFunc: func(string, any) (string, error) {
				return "VALUE", nil
			},
			expectedOutput: func() *string {
				s := "VALUE"
				return &s
			}(),
			expectedError: nil,
		},
		{
			name: "pointer to struct",
			input: &struct {
				Field string
			}{
				Field: "hello",
			},
			replacementFunc: func(string, any) (string, error) {
				return "VALUE", nil
			},
			expectedOutput: &struct {
				Field string
			}{
				Field: "VALUE",
			},
			expectedError: nil,
		},
		{
			name:  "nil pointer",
			input: (*string)(nil),
			replacementFunc: func(string, any) (string, error) {
				return "VALUE", nil
			},
			expectedOutput: (*string)(nil),
			expectedError:  nil,
		},
		{
			name:  "interface with string",
			input: interface{}("hello"),
			replacementFunc: func(string, any) (string, error) {
				return "VALUE", nil
			},
			expectedOutput: "VALUE",
			expectedError:  nil,
		},
		{
			name:  "interface with struct",
			input: interface{}(struct{ Field string }{Field: "hello"}),
			replacementFunc: func(string, any) (string, error) {
				return "VALUE", nil
			},
			expectedOutput: struct{ Field string }{Field: "VALUE"},
			expectedError:  nil,
		},
		{
			name: "complex nested structure",
			input: map[string]interface{}{
				"key1": []interface{}{
					"hello",
					map[string]string{
						"nestedKey": "world",
					},
					&struct{ Field string }{Field: "structValue"},
				},
				"key2": "another",
			},
			replacementFunc: func(s string, _ any) (string, error) {
				switch s {
				case "key1":
					return "keyA", nil
				case "key2":
					return "keyB", nil
				case "nestedKey":
					return "nestedKeyA", nil
				case "hello":
					return "valueA", nil
				case "world":
					return "valueB", nil
				case "structValue":
					return "valueC", nil
				case "another":
					return "valueD", nil
				default:
					return s, nil
				}
			},
			expectedOutput: map[string]interface{}{
				"keyA": []interface{}{
					"valueA",
					map[string]string{
						"nestedKeyA": "valueB",
					},
					&struct{ Field string }{Field: "valueC"},
				},
				"keyB": "valueD",
			},
			expectedError: nil,
		},
		{
			name:  "error in replacement",
			input: "hello",
			replacementFunc: func(string, any) (string, error) {
				return "", errors.New("forced error")
			},
			expectedOutput: nil,
			expectedError:  errors.New("forced error"),
		},
		{
			name: "no replacement function",
			input: map[string]string{
				"key1": "hello",
				"key2": "world",
			},
			replacementFunc: nil,
			expectedOutput: map[string]string{
				"key1": "hello",
				"key2": "world",
			},
			expectedError: nil,
		},
		{
			name:  "context in replacement",
			input: "hello",
			replacementFunc: func(_ string, ctx any) (string, error) {
				if ctx == nil {
					return "", errors.New("context is nil")
				}
				if val, ok := ctx.(map[string]string); ok {
					if val["key"] == "value" {
						return "VALUE_WITH_CONTEXT", nil
					}
					return "", errors.New("wrong context value")
				}
				return "", errors.New("wrong context type")
			},
			expectedOutput: "VALUE_WITH_CONTEXT",
			expectedError:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			replacer := newStringReplacer(tt.replacementFunc)
			var result any
			var err error
			if tt.name == "context in replacement" {
				result, err = replacer.Replace(tt.input, map[string]string{"key": "value"})
			} else {
				result, err = replacer.Replace(tt.input, nil)
			}
			if err != nil && tt.expectedError != nil {
				if err.Error() != tt.expectedError.Error() {
					t.Errorf("Replace() error = %v, expectedError %v", err, tt.expectedError)
					return
				}
			} else if !errors.Is(err, tt.expectedError) {
				t.Errorf("Replace() error = %v, expectedError %v", err, tt.expectedError)
				return
			}
			if result == nil && tt.expectedOutput == nil { //nolint:revive // ok for now
				// OK
			} else if !reflect.DeepEqual(result, tt.expectedOutput) {
				t.Errorf("Replace() = %v, expected %v", result, tt.expectedOutput)
			}
		})
	}
}

func TestReplaceStructWithMap(t *testing.T) {
	type MyStruct struct {
		Data map[string]string
		Name string
	}

	input := MyStruct{
		Name: "hello",
		Data: map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
	}

	replacer := newStringReplacer(func(s string, _ any) (string, error) {
		switch s {
		case "hello":
			return "HELLO", nil
		case "value1":
			return "VALUE1", nil
		case "value2":
			return "VALUE2", nil
		case "key1":
			return "KEY1", nil
		case "key2":
			return "KEY2", nil
		default:
			return s, nil
		}
	})

	result, err := replacer.Replace(input, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// type assertion check
	resultStruct, ok := result.(MyStruct)
	if !ok {
		t.Errorf("expected result to be type MyStruct, got %T", result)
	}

	// Verify expected values
	expected := MyStruct{
		Name: "HELLO",
		Data: map[string]string{
			"KEY1": "VALUE1",
			"KEY2": "VALUE2",
		},
	}

	if !reflect.DeepEqual(resultStruct, expected) {
		t.Errorf("got %+v, want %+v", resultStruct, expected)
	}
}
