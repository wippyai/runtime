package gostruct

import (
	"reflect"
	"strings"
	"testing"
)

func TestReplacer(t *testing.T) {
	testCases := []struct {
		name         string
		data         any
		replacements map[string]string
		expected     any
		expectErr    bool
	}{
		{
			name: "Simple String Replacement",
			data: map[string]string{
				"key1": "${value1}", // Using placeholder
				"key2": "value2",
			},
			replacements: map[string]string{
				"value1": "replacedValue1",
			},
			expected: map[string]string{
				"key1": "replacedValue1",
				"key2": "value2",
			},
			expectErr: false,
		},
		{
			name: "Nested Map Replacement",
			data: map[string]any{
				"key1": "${value1}", // Using placeholder
				"key2": map[string]string{
					"nestedKey1": "nestedValue1",
					"nestedKey2": "${nestedValue2}", // Using placeholder
				},
			},
			replacements: map[string]string{
				"value1":       "replacedValue1",
				"nestedValue2": "replacedNestedValue2",
			},
			expected: map[string]any{
				"key1": "replacedValue1",
				"key2": map[string]string{
					"nestedKey1": "nestedValue1",
					"nestedKey2": "replacedNestedValue2",
				},
			},
			expectErr: false,
		},
		{
			name: "Slice Replacement",
			data: []string{
				"value1",
				"${value2}", // Using placeholder
				"value3",
			},
			replacements: map[string]string{
				"value2": "replacedValue2",
			},
			expected: []string{
				"value1",
				"replacedValue2",
				"value3",
			},
			expectErr: false,
		},
		{
			name: "Struct Replacement",
			data: struct {
				Field1 string
				Field2 int
				Field3 struct {
					NestedField string
				}
			}{
				Field1: "${value1}", // Using placeholder
				Field2: 123,
				Field3: struct {
					NestedField string
				}{
					NestedField: "${nestedValue}", // Using placeholder
				},
			},
			replacements: map[string]string{
				"value1":      "replacedValue1",
				"nestedValue": "replacedNestedValue",
			},
			expected: struct {
				Field1 string
				Field2 int
				Field3 struct {
					NestedField string
				}
			}{
				Field1: "replacedValue1",
				Field2: 123,
				Field3: struct {
					NestedField string
				}{
					NestedField: "replacedNestedValue",
				},
			},
			expectErr: false,
		},
		{
			name: "Pointer Replacement",
			data: &struct {
				Field1 string
			}{
				Field1: "${value1}", // Using placeholder
			},
			replacements: map[string]string{
				"value1": "replacedValue1",
			},
			expected: &struct {
				Field1 string
			}{
				Field1: "replacedValue1",
			},
			expectErr: false,
		},
		{
			name: "No Replacement",
			data: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
			replacements: map[string]string{
				"value3": "replacedValue3",
			},
			expected: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
			expectErr: false,
		},
		{
			name: "Nil Pointer",
			data: map[string]*string{
				"key1": nil,
			},
			replacements: map[string]string{},
			expected: map[string]*string{
				"key1": nil,
			},
			expectErr: false,
		},
		{
			name:         "Empty Map",
			data:         map[string]string{},
			replacements: map[string]string{},
			expected:     map[string]string{},
			expectErr:    false,
		},
		{
			name:         "Empty Slice",
			data:         []string{},
			replacements: map[string]string{},
			expected:     []string{},
			expectErr:    false,
		},
		{
			name:         "Empty String",
			data:         "",
			replacements: map[string]string{},
			expected:     "",
			expectErr:    false,
		},
		{
			name: "Deeply Nested Map Replacement",
			data: map[string]any{
				"key1": "value1",
				"key2": map[string]any{
					"nestedKey1": map[string]any{
						"deeplyNestedKey": "${deeplyNestedValue}", // Using placeholder
					},
				},
			},
			replacements: map[string]string{
				"deeplyNestedValue": "replacedDeeplyNestedValue",
			},
			expected: map[string]any{
				"key1": "value1",
				"key2": map[string]any{
					"nestedKey1": map[string]any{
						"deeplyNestedKey": "replacedDeeplyNestedValue",
					},
				},
			},
			expectErr: false,
		},
		{
			name: "Array Replacement",
			data: [3]string{
				"value1",
				"${value2}", // Using placeholder
				"value3",
			},
			replacements: map[string]string{
				"value2": "replacedValue2",
			},
			expected: [3]string{
				"value1",
				"replacedValue2",
				"value3",
			},
			expectErr: false,
		},
		{
			name: "Interface Replacement",
			data: map[string]interface{}{
				"key1": "${value1}", // Using placeholder
				"key2": 123,
			},
			replacements: map[string]string{
				"value1": "replacedValue1",
			},
			expected: map[string]interface{}{
				"key1": "replacedValue1",
				"key2": 123,
			},
			expectErr: false,
		},
		{
			name: "Key Replacement",
			data: map[string]string{
				"${keyToReplace}": "value1", // Using placeholder in key
			},
			replacements: map[string]string{
				"keyToReplace": "replacedKey",
			},
			expected: map[string]string{
				"replacedKey": "value1",
			},
			expectErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			replacer := NewReplacer(tc.replacements)
			actual, err := replacer.Replace(tc.data)

			if tc.expectErr && err == nil {
				t.Errorf("Expected error, but got nil")
			}

			if !tc.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !reflect.DeepEqual(actual, tc.expected) {
				t.Errorf("Expected: %v\nGot: %v", tc.expected, actual)
			}
		})
	}
}

func TestReplacerTypedStruct(t *testing.T) {
	type MyStruct struct {
		Name  string
		Value string
	}

	data := MyStruct{
		Name:  "${name}",
		Value: "static",
	}
	replacements := map[string]string{
		"name": "replacedName",
	}
	expected := MyStruct{
		Name:  "replacedName",
		Value: "static",
	}

	replacer := NewReplacer(replacements)
	actual, err := replacer.Replace(data)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Expected: %v\nGot: %v", expected, actual)
	}
}

func TestReplacerUnexportedFields(t *testing.T) {
	type MyStruct struct {
		ExportedField   string
		unexportedField string
	}

	data := MyStruct{
		ExportedField:   "${replaceMe}",
		unexportedField: "keepMe",
	}
	replacements := map[string]string{
		"replaceMe": "replaced",
	}

	replacer := NewReplacer(replacements)
	_, err := replacer.Replace(data) // We only check for the error here

	if err == nil {
		t.Errorf("Expected error due to unexported field, but got nil")
	} else if !strings.Contains(err.Error(), "cannot set unexported field") {
		t.Errorf("Expected error message to contain 'cannot set unexported field', but got: %v", err)
	}
}
