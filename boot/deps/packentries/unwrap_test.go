// SPDX-License-Identifier: MPL-2.0

package packentries

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- UnwrapPayloadData ---

func TestUnwrapPayloadData_NonMap(t *testing.T) {
	assert.Equal(t, "hello", UnwrapPayloadData("hello"))
	assert.Equal(t, 42, UnwrapPayloadData(42))
	assert.Nil(t, UnwrapPayloadData(nil))
}

func TestUnwrapPayloadData_MapWithoutDataFormat(t *testing.T) {
	m := map[string]any{"key": "value"}
	assert.Equal(t, m, UnwrapPayloadData(m))
}

func TestUnwrapPayloadData_MapWithDataFormat(t *testing.T) {
	m := map[string]any{
		"Data":   "inner-data",
		"Format": "json",
	}
	assert.Equal(t, "inner-data", UnwrapPayloadData(m))
}

func TestUnwrapPayloadData_MapWithExtraKeys(t *testing.T) {
	m := map[string]any{
		"Data":   "inner-data",
		"Format": "json",
		"Extra":  "ignored",
	}
	// 3 keys, not exactly 2, so not unwrapped
	assert.Equal(t, m, UnwrapPayloadData(m))
}

func TestUnwrapPayloadData_Cases(t *testing.T) {
	t.Run("returns non-map data as-is", func(t *testing.T) {
		result := UnwrapPayloadData("string value")
		if result != "string value" {
			t.Errorf("expected string value, got %v", result)
		}

		result = UnwrapPayloadData(42)
		if result != 42 {
			t.Errorf("expected 42, got %v", result)
		}

		result = UnwrapPayloadData(nil)
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})

	t.Run("unwraps payload wrapper structure", func(t *testing.T) {
		wrapped := map[string]any{
			"Data":   "inner value",
			"Format": "json",
		}
		result := UnwrapPayloadData(wrapped)
		if result != "inner value" {
			t.Errorf("expected 'inner value', got %v", result)
		}
	})

	t.Run("returns map as-is if not payload wrapper", func(t *testing.T) {
		regularMap := map[string]any{
			"key1": "value1",
			"key2": "value2",
		}
		result := UnwrapPayloadData(regularMap)
		resultMap, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("expected map, got %T", result)
		}
		if resultMap["key1"] != "value1" {
			t.Errorf("expected key1=value1, got %v", resultMap["key1"])
		}
	})

	t.Run("returns map with extra fields as-is", func(t *testing.T) {
		mapWithExtra := map[string]any{
			"Data":   "inner",
			"Format": "json",
			"Extra":  "field",
		}
		result := UnwrapPayloadData(mapWithExtra)
		resultMap, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("expected map, got %T", result)
		}
		if resultMap["Extra"] != "field" {
			t.Errorf("expected Extra=field in result")
		}
	})

	t.Run("handles map with only Data field", func(t *testing.T) {
		mapOnlyData := map[string]any{
			"Data": "value",
		}
		result := UnwrapPayloadData(mapOnlyData)
		resultMap, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("expected map, got %T", result)
		}
		if resultMap["Data"] != "value" {
			t.Errorf("expected Data=value in result")
		}
	})
}
