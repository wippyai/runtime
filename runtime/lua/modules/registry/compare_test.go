package registry

import (
	"testing"
)

func TestMapsEqualWithNilValues(t *testing.T) {
	a := map[string]any{
		"key1": nil,
		"key2": "value",
	}
	b := map[string]any{
		"key1": nil,
		"key2": "value",
	}

	if !mapsEqual(a, b) {
		t.Error("expected maps with nil values to be equal")
	}
}

func TestMapsEqualOneNilOneNotNil(t *testing.T) {
	a := map[string]any{
		"key": nil,
	}
	b := map[string]any{
		"key": "value",
	}

	if mapsEqual(a, b) {
		t.Error("expected maps with one nil and one non-nil to be unequal")
	}
}

func TestValuesEqualMixedNumbers(t *testing.T) {
	tests := []struct {
		name string
		a, b interface{}
		want bool
	}{
		{"int equals float", 42, 42.0, true},
		{"float equals int", 42.0, 42, true},
		{"int64 equals float64", int64(100), 100.0, true},
		{"different numbers", int(42), float64(43), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := valuesEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("valuesEqual(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestMapsEqualComplexNesting(t *testing.T) {
	a := map[string]any{
		"users": []any{
			map[string]any{
				"id":   1,
				"name": "Alice",
				"tags": []any{"admin", "user"},
			},
			map[string]any{
				"id":   2,
				"name": "Bob",
				"tags": []any{"user"},
			},
		},
	}
	b := map[string]any{
		"users": []any{
			map[string]any{
				"id":   1,
				"name": "Alice",
				"tags": []any{"admin", "user"},
			},
			map[string]any{
				"id":   2,
				"name": "Bob",
				"tags": []any{"user"},
			},
		},
	}

	if !mapsEqual(a, b) {
		t.Error("expected complex nested structures to be equal")
	}
}

func TestMapsEqualComplexNestingDifferent(t *testing.T) {
	a := map[string]any{
		"users": []any{
			map[string]any{
				"id":   1,
				"tags": []any{"admin", "user"},
			},
		},
	}
	b := map[string]any{
		"users": []any{
			map[string]any{
				"id":   1,
				"tags": []any{"user", "admin"},
			},
		},
	}

	if mapsEqual(a, b) {
		t.Error("expected different nested array orders to be unequal")
	}
}

func TestValuesEqualMapWithWrongType(t *testing.T) {
	a := map[string]any{"key": "value"}
	b := []any{"not", "a", "map"}

	if valuesEqual(a, b) {
		t.Error("expected map and array to be unequal")
	}
}

func TestValuesEqualArrayWithWrongType(t *testing.T) {
	a := []any{1, 2, 3}
	b := map[string]any{"not": "an array"}

	if valuesEqual(a, b) {
		t.Error("expected array and map to be unequal")
	}
}

func TestMapsEqualNestedMapVsNonMap(t *testing.T) {
	a := map[string]any{
		"nested": map[string]any{"key": "value"},
	}
	b := map[string]any{
		"nested": "not a map",
	}

	if mapsEqual(a, b) {
		t.Error("expected nested map and string to be unequal")
	}
}

func TestMapsEqualNestedArrayVsNonArray(t *testing.T) {
	a := map[string]any{
		"array": []any{1, 2, 3},
	}
	b := map[string]any{
		"array": "not an array",
	}

	if mapsEqual(a, b) {
		t.Error("expected nested array and string to be unequal")
	}
}
