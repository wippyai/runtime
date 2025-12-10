package registry

import (
	"testing"
)

func TestMapsEqualNested(t *testing.T) {
	a := map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{
				"key": "value",
			},
		},
	}
	b := map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{
				"key": "value",
			},
		},
	}

	if !mapsEqual(a, b) {
		t.Error("expected deeply nested maps to be equal")
	}
}

func TestMapsEqualNestedDifferent(t *testing.T) {
	a := map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{
				"key": "value1",
			},
		},
	}
	b := map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{
				"key": "value2",
			},
		},
	}

	if mapsEqual(a, b) {
		t.Error("expected deeply nested maps with different values to be unequal")
	}
}

func TestMapsEqualWithMixedTypes(t *testing.T) {
	a := map[string]any{
		"int":    42,
		"float":  3.14,
		"string": "test",
		"bool":   true,
		"nil":    nil,
	}
	b := map[string]any{
		"int":    42,
		"float":  3.14,
		"string": "test",
		"bool":   true,
		"nil":    nil,
	}

	if !mapsEqual(a, b) {
		t.Error("expected maps with mixed types to be equal")
	}
}

func TestValuesEqualNestedMaps(t *testing.T) {
	a := map[string]any{"inner": "value"}
	b := map[string]any{"inner": "value"}

	if !valuesEqual(a, b) {
		t.Error("expected nested maps to be equal")
	}
}

func TestValuesEqualDifferentNestedMaps(t *testing.T) {
	a := map[string]any{"inner": "value1"}
	b := map[string]any{"inner": "value2"}

	if valuesEqual(a, b) {
		t.Error("expected different nested maps to be unequal")
	}
}

func TestValuesEqualArrays(t *testing.T) {
	a := []any{1, 2, 3}
	b := []any{1, 2, 3}

	if !valuesEqual(a, b) {
		t.Error("expected equal arrays to be equal")
	}
}

func TestValuesEqualDifferentArrays(t *testing.T) {
	a := []any{1, 2, 3}
	b := []any{1, 2, 4}

	if valuesEqual(a, b) {
		t.Error("expected different arrays to be unequal")
	}
}

func TestValuesEqualMixedTypeArrays(t *testing.T) {
	a := []any{1, "test", true}
	b := []any{1, "test", true}

	if !valuesEqual(a, b) {
		t.Error("expected mixed type arrays to be equal")
	}
}

func TestValuesEqualNestedArrays(t *testing.T) {
	a := []any{[]any{1, 2}, []any{3, 4}}
	b := []any{[]any{1, 2}, []any{3, 4}}

	if !valuesEqual(a, b) {
		t.Error("expected nested arrays to be equal")
	}
}

func TestIsNumericAllTypes(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  bool
	}{
		{"uint", uint(42), true},
		{"uint8", uint8(42), true},
		{"uint16", uint16(42), true},
		{"uint32", uint32(42), true},
		{"uint64", uint64(42), true},
		{"float32", float32(3.14), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNumeric(tt.value); got != tt.want {
				t.Errorf("isNumeric(%v) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestToFloat64AllTypes(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  float64
	}{
		{"uint", uint(42), 42.0},
		{"uint8", uint8(42), 42.0},
		{"uint16", uint16(42), 42.0},
		{"uint32", uint32(42), 42.0},
		{"uint64", uint64(42), 42.0},
		{"int8", int8(42), 42.0},
		{"int16", int16(42), 42.0},
		{"int32", int32(42), 42.0},
		{"float32", float32(3.5), 3.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toFloat64(tt.value); got != tt.want {
				t.Errorf("toFloat64(%v) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestMapsEqualArrayOfMaps(t *testing.T) {
	a := map[string]any{
		"items": []any{
			map[string]any{"id": 1},
			map[string]any{"id": 2},
		},
	}
	b := map[string]any{
		"items": []any{
			map[string]any{"id": 1},
			map[string]any{"id": 2},
		},
	}

	if !mapsEqual(a, b) {
		t.Error("expected maps with array of maps to be equal")
	}
}

func TestValuesEqualNonMapNonArray(t *testing.T) {
	if !valuesEqual("string", "string") {
		t.Error("expected equal strings to be equal")
	}

	if valuesEqual("string1", "string2") {
		t.Error("expected different strings to be unequal")
	}
}

func TestValuesEqualMapVsNonMap(t *testing.T) {
	a := map[string]any{"key": "value"}
	b := "not a map"

	if valuesEqual(a, b) {
		t.Error("expected map and non-map to be unequal")
	}
}

func TestValuesEqualArrayVsNonArray(t *testing.T) {
	a := []any{1, 2, 3}
	b := "not an array"

	if valuesEqual(a, b) {
		t.Error("expected array and non-array to be unequal")
	}
}
