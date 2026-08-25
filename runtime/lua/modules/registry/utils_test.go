// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	regapi "github.com/wippyai/runtime/api/registry"
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
		value any
		name  string
		want  bool
	}{
		{uint(42), "uint", true},
		{uint8(42), "uint8", true},
		{uint16(42), "uint16", true},
		{uint32(42), "uint32", true},
		{uint64(42), "uint64", true},
		{float32(3.14), "float32", true},
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
		value any
		name  string
		want  float64
	}{
		{uint(42), "uint", 42.0},
		{uint8(42), "uint8", 42.0},
		{uint16(42), "uint16", 42.0},
		{uint32(42), "uint32", 42.0},
		{uint64(42), "uint64", 42.0},
		{int8(42), "int8", 42.0},
		{int16(42), "int16", 42.0},
		{int32(42), "int32", 42.0},
		{float32(3.5), "float32", 3.5},
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

// root marks an ns.dependency selected as a deployment root and is the sole
// authority for that status: meta is user space and carries no trust. A Lua
// write path that cannot read or emit the field silently demotes every root.
// Root-ness never lands on the entry payload: the table field is view data.
func TestLuaTableToEntryIgnoresRoot(t *testing.T) {
	l := newTestState()
	defer l.Close()

	table := l.CreateTable(0, 3)
	table.RawSetString("id", lua.LString("app.deps:crm"))
	table.RawSetString("kind", lua.LString("ns.dependency"))
	table.RawSetString("root", lua.LTrue)

	entry, err := luaTableToEntry(l, table)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.DependencyRoot {
		t.Error("entry payloads never carry the deployment-root flag")
	}
}

func TestLuaTableToEntryDefaultsRootFalse(t *testing.T) {
	l := newTestState()
	defer l.Close()

	table := l.CreateTable(0, 2)
	table.RawSetString("id", lua.LString("app.deps:crm"))
	table.RawSetString("kind", lua.LString("ns.dependency"))

	entry, err := luaTableToEntry(l, table)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.DependencyRoot {
		t.Error("expected root to default to false when absent")
	}
}

// A decoded legacy payload still emits its flag for display; converting the
// table back never rebuilds it on the entry.
func TestEntryLuaRoundTripDropsRootFromPayload(t *testing.T) {
	l := newTestState()
	defer l.Close()

	original := regapi.Entry{
		ID:             regapi.ParseID("app.deps:crm"),
		Kind:           "ns.dependency",
		Meta:           attrs.Bag{"module": "kickside/app"},
		DependencyRoot: true,
	}

	table, err := entryToLuaTable(l, original)
	if err != nil {
		t.Fatalf("unexpected error converting entry to table: %v", err)
	}
	if table.RawGetString("root") != lua.LTrue {
		t.Fatal("expected root to be emitted onto the Lua table")
	}

	back, err := luaTableToEntry(l, table)
	if err != nil {
		t.Fatalf("unexpected error converting table to entry: %v", err)
	}
	if back.DependencyRoot {
		t.Error("entry payloads never carry the deployment-root flag")
	}
}
