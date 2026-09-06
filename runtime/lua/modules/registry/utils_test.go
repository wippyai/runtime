// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"strings"
	"testing"

	lua "github.com/wippyai/go-lua"
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

func TestLuaTableToEntryDoesNotAcceptRegistryMetadata(t *testing.T) {
	l := newTestState()
	defer l.Close()

	table := l.CreateTable(0, 3)
	table.RawSetString("id", lua.LString("app:handler"))
	table.RawSetString("kind", lua.LString("function.lua"))
	metadata := l.CreateTable(0, 2)
	metadata.RawSetString("owner", lua.LString("forged/module"))
	metadata.RawSetString("root", lua.LTrue)
	table.RawSetString("registry", metadata)

	entry, err := luaTableToEntry(l, table)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Registry != (regapi.EntryMetadata{}) {
		t.Fatalf("registry metadata must not be accepted from Lua: %#v", entry.Registry)
	}
}

func TestLuaTableToEntryAcceptsDependencyRootControl(t *testing.T) {
	l := newTestState()
	defer l.Close()

	table := l.CreateTable(0, 3)
	table.RawSetString("id", lua.LString("app.deps:knowledge"))
	table.RawSetString("kind", lua.LString("ns.dependency"))
	table.RawSetString("dependency_root", lua.LTrue)

	entry, err := luaTableToEntry(l, table)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !entry.Registry.Root {
		t.Fatal("dependency_root must select the registry-owned root bit")
	}
	if entry.Registry.Owner != "" {
		t.Fatalf("Lua must not assign registry ownership: %q", entry.Registry.Owner)
	}
}

func TestLuaTableToEntryRejectsInvalidDependencyRoot(t *testing.T) {
	l := newTestState()
	defer l.Close()

	table := l.CreateTable(0, 3)
	table.RawSetString("id", lua.LString("app.deps:knowledge"))
	table.RawSetString("kind", lua.LString("ns.dependency"))
	table.RawSetString("dependency_root", lua.LString("true"))

	_, err := luaTableToEntry(l, table)
	if err == nil {
		t.Fatal("expected invalid dependency_root to fail")
	}
}

// TestConvertFilterToMetadataShapes pins the filter selector contract: a
// top-level key is a root selector or a metadata selector, keys of the nested
// meta table name metadata fields, and anything else is rejected.
func TestConvertFilterToMetadataShapes(t *testing.T) {
	cases := []struct {
		build   func(l *lua.LState) *lua.LTable
		want    map[string]any
		name    string
		wantErr []string
	}{
		{
			name: "root kind selector",
			build: func(l *lua.LState) *lua.LTable {
				tbl := l.CreateTable(0, 1)
				tbl.RawSetString(".kind", lua.LString("process.lua"))
				return tbl
			},
			want: map[string]any{".kind": "process.lua"},
		},
		{
			name: "every root selector",
			build: func(l *lua.LState) *lua.LTable {
				tbl := l.CreateTable(0, 4)
				tbl.RawSetString(".kind", lua.LString("process.lua"))
				tbl.RawSetString(".ns", lua.LString("app"))
				tbl.RawSetString(".name", lua.LString("alpha"))
				tbl.RawSetString(".id", lua.LString("app:alpha"))
				return tbl
			},
			want: map[string]any{
				".kind": "process.lua",
				".ns":   "app",
				".name": "alpha",
				".id":   "app:alpha",
			},
		},
		{
			name: "metadata selector",
			build: func(l *lua.LState) *lua.LTable {
				tbl := l.CreateTable(0, 1)
				tbl.RawSetString("meta.type", lua.LString("tool"))
				return tbl
			},
			want: map[string]any{"meta.type": "tool"},
		},
		{
			name: "metadata selector with operator",
			build: func(l *lua.LState) *lua.LTable {
				tbl := l.CreateTable(0, 1)
				tbl.RawSetString("~meta.name", lua.LString("^x"))
				return tbl
			},
			want: map[string]any{"~meta.name": "^x"},
		},
		{
			name: "nested meta key",
			build: func(l *lua.LState) *lua.LTable {
				tbl := l.CreateTable(0, 1)
				meta := l.CreateTable(0, 1)
				meta.RawSetString("type", lua.LString("tool"))
				tbl.RawSetString("meta", meta)
				return tbl
			},
			want: map[string]any{"meta.type": "tool"},
		},
		{
			name: "nested meta key with operator",
			build: func(l *lua.LState) *lua.LTable {
				tbl := l.CreateTable(0, 1)
				meta := l.CreateTable(0, 1)
				meta.RawSetString("~name", lua.LString("^x"))
				tbl.RawSetString("meta", meta)
				return tbl
			},
			want: map[string]any{"~meta.name": "^x"},
		},
		{
			name: "root selector combined with nested meta",
			build: func(l *lua.LState) *lua.LTable {
				tbl := l.CreateTable(0, 2)
				tbl.RawSetString(".kind", lua.LString("process.lua"))
				meta := l.CreateTable(0, 1)
				meta.RawSetString("type", lua.LString("desktop.application"))
				tbl.RawSetString("meta", meta)
				return tbl
			},
			want: map[string]any{".kind": "process.lua", "meta.type": "desktop.application"},
		},
		{
			name: "empty filter stays empty",
			build: func(l *lua.LState) *lua.LTable {
				return l.CreateTable(0, 0)
			},
			want: map[string]any{},
		},
		{
			name: "bare kind is rejected",
			build: func(l *lua.LState) *lua.LTable {
				tbl := l.CreateTable(0, 1)
				tbl.RawSetString("kind", lua.LString("process.lua"))
				return tbl
			},
			wantErr: []string{`filter key "kind" is not a selector`, `".kind"`, `"meta.kind"`},
		},
		{
			name: "bare type is rejected",
			build: func(l *lua.LState) *lua.LTable {
				tbl := l.CreateTable(0, 1)
				tbl.RawSetString("type", lua.LString("tool"))
				return tbl
			},
			wantErr: []string{`filter key "type" is not a selector`, `"meta.type"`},
		},
		{
			name: "bare key with operator is rejected",
			build: func(l *lua.LState) *lua.LTable {
				tbl := l.CreateTable(0, 1)
				tbl.RawSetString("~name", lua.LString("^x"))
				return tbl
			},
			wantErr: []string{`filter key "~name" is not a selector`, `"~meta.name"`},
		},
		{
			name: "unknown root selector is rejected",
			build: func(l *lua.LState) *lua.LTable {
				tbl := l.CreateTable(0, 1)
				tbl.RawSetString(".type", lua.LString("tool"))
				return tbl
			},
			wantErr: []string{`filter key ".type" is not a root selector`, `"meta.type"`},
		},
		{
			name: "nested meta key repeating the prefix is rejected",
			build: func(l *lua.LState) *lua.LTable {
				tbl := l.CreateTable(0, 1)
				meta := l.CreateTable(0, 1)
				meta.RawSetString("meta.type", lua.LString("tool"))
				tbl.RawSetString("meta", meta)
				return tbl
			},
			wantErr: []string{`filter key "meta.type" in the meta table repeats`, `"type"`},
		},
		{
			name: "meta value that is not a table is rejected",
			build: func(l *lua.LState) *lua.LTable {
				tbl := l.CreateTable(0, 1)
				tbl.RawSetString("meta", lua.LString("tool"))
				return tbl
			},
			wantErr: []string{`filter key "meta" must be a table`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			got, err := convertFilterToMetadata(l, tc.build(l))

			if len(tc.wantErr) > 0 {
				if err == nil {
					t.Fatalf("expected error, got filter %v", map[string]any(got))
				}
				for _, fragment := range tc.wantErr {
					if !strings.Contains(err.Error(), fragment) {
						t.Fatalf("expected error to contain %q, got %q", fragment, err.Error())
					}
				}
				if got != nil {
					t.Fatalf("expected no filter on error, got %v", map[string]any(got))
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("expected %d filter keys, got %d: %v", len(tc.want), len(got), map[string]any(got))
			}
			for key, want := range tc.want {
				actual, ok := got[key]
				if !ok {
					t.Fatalf("expected filter key %q, got %v", key, map[string]any(got))
				}
				if actual != want {
					t.Fatalf("filter key %q: expected %v, got %v", key, want, actual)
				}
			}
		})
	}
}
