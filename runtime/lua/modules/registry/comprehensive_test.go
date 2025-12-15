package registry

import (
	"testing"
)

func TestMapsEqualSameReference(t *testing.T) {
	a := map[string]any{
		"key": "value",
	}

	if !mapsEqual(a, a) {
		t.Error("expected map to equal itself")
	}
}

func TestValuesEqualSameReference(t *testing.T) {
	arr := []any{1, 2, 3}

	if !valuesEqual(arr, arr) {
		t.Error("expected array to equal itself")
	}
}

func TestValuesEqualNumberAndString(t *testing.T) {
	if valuesEqual(42, "42") {
		t.Error("expected number and string to be unequal")
	}
}

func TestMapsEqualNestedMapMismatch(t *testing.T) {
	a := map[string]any{
		"outer": map[string]any{
			"inner": "value1",
		},
	}
	b := map[string]any{
		"outer": "not a map",
	}

	if mapsEqual(a, b) {
		t.Error("expected map and non-map nested value to be unequal")
	}
}

func TestMapsEqualNestedArrayMismatch(t *testing.T) {
	a := map[string]any{
		"list": []any{1, 2},
	}
	b := map[string]any{
		"list": "not an array",
	}

	if mapsEqual(a, b) {
		t.Error("expected array and non-array nested value to be unequal")
	}
}

func TestValuesEqualNumericEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		a, b interface{}
		want bool
	}{
		{"zero int and zero float", 0, 0.0, true},
		{"negative equal", -5, -5.0, true},
		{"large numbers equal", int64(1000000), 1000000.0, true},
		{"different signs", 5, -5.0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := valuesEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("valuesEqual(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestIsNumericEdgeCases(t *testing.T) {
	if isNumeric(struct{}{}) {
		t.Error("expected struct to not be numeric")
	}

	if isNumeric([]int{1, 2, 3}) {
		t.Error("expected slice to not be numeric")
	}

	if isNumeric(map[string]int{}) {
		t.Error("expected map to not be numeric")
	}
}

func TestToFloat64Boundaries(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  float64
	}{
		{"max int8", int8(127), 127.0},
		{"min int8", int8(-128), -128.0},
		{"max uint8", uint8(255), 255.0},
		{"zero", 0, 0.0},
		{"negative", -42, -42.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toFloat64(tt.value); got != tt.want {
				t.Errorf("toFloat64(%v) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestMapsEqualBooleans(t *testing.T) {
	a := map[string]any{
		"flag1": true,
		"flag2": false,
	}
	b := map[string]any{
		"flag1": true,
		"flag2": false,
	}

	if !mapsEqual(a, b) {
		t.Error("expected maps with booleans to be equal")
	}
}

func TestMapsEqualBooleansDifferent(t *testing.T) {
	a := map[string]any{
		"flag": true,
	}
	b := map[string]any{
		"flag": false,
	}

	if mapsEqual(a, b) {
		t.Error("expected maps with different booleans to be unequal")
	}
}

func TestValuesEqualEmptyArrays(t *testing.T) {
	var a []any
	var b []any

	if !valuesEqual(a, b) {
		t.Error("expected empty arrays to be equal")
	}
}

func TestValuesEqualSingleElementArrays(t *testing.T) {
	a := []any{42}
	b := []any{42}

	if !valuesEqual(a, b) {
		t.Error("expected single element arrays to be equal")
	}
}
