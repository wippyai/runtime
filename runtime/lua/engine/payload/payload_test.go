package payload

import (
	"encoding/json"
	"math"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

// skipUnlessStress skips the test unless WIPPY_STRESS_TESTS=1
func skipUnlessStress(t *testing.T) {
	if os.Getenv("WIPPY_STRESS_TESTS") != "1" {
		t.Skip("Skipping stress test (set WIPPY_STRESS_TESTS=1 to run)")
	}
}

// =============================================================================
// UNIT TESTS - GoToLua
// =============================================================================

func TestGoToLua_BasicTypes(t *testing.T) {
	tests := []struct {
		input   any
		check   func(t *testing.T, lv lua.LValue)
		name    string
		wantErr bool
	}{
		{
			name:  "nil",
			input: nil,
			check: func(t *testing.T, lv lua.LValue) {
				assert.Equal(t, lua.LTNil, lv.Type())
			},
		},
		{
			name:  "string",
			input: "hello world",
			check: func(t *testing.T, lv lua.LValue) {
				assert.Equal(t, lua.LTString, lv.Type())
				assert.Equal(t, "hello world", string(lv.(lua.LString)))
			},
		},
		{
			name:  "empty string",
			input: "",
			check: func(t *testing.T, lv lua.LValue) {
				assert.Equal(t, lua.LTString, lv.Type())
				assert.Equal(t, "", string(lv.(lua.LString)))
			},
		},
		{
			name:  "unicode string",
			input: "こんにちは世界🌍",
			check: func(t *testing.T, lv lua.LValue) {
				assert.Equal(t, lua.LTString, lv.Type())
				assert.Equal(t, "こんにちは世界🌍", string(lv.(lua.LString)))
			},
		},
		{
			name:  "float64",
			input: 42.5,
			check: func(t *testing.T, lv lua.LValue) {
				assert.Equal(t, lua.LTNumber, lv.Type())
				assert.Equal(t, 42.5, float64(lv.(lua.LNumber)))
			},
		},
		{
			name:  "float64 max",
			input: math.MaxFloat64,
			check: func(t *testing.T, lv lua.LValue) {
				assert.Equal(t, lua.LTNumber, lv.Type())
				assert.Equal(t, math.MaxFloat64, float64(lv.(lua.LNumber)))
			},
		},
		{
			name:  "float64 min",
			input: math.SmallestNonzeroFloat64,
			check: func(t *testing.T, lv lua.LValue) {
				assert.Equal(t, lua.LTNumber, lv.Type())
				assert.Equal(t, math.SmallestNonzeroFloat64, float64(lv.(lua.LNumber)))
			},
		},
		{
			name:  "float64 negative",
			input: -123.456,
			check: func(t *testing.T, lv lua.LValue) {
				assert.Equal(t, lua.LTNumber, lv.Type())
				assert.Equal(t, -123.456, float64(lv.(lua.LNumber)))
			},
		},
		{
			name:  "int",
			input: 42,
			check: func(t *testing.T, lv lua.LValue) {
				assert.Equal(t, lua.LTInteger, lv.Type())
				assert.Equal(t, int64(42), int64(lv.(lua.LInteger)))
			},
		},
		{
			name:  "int32",
			input: int32(42),
			check: func(t *testing.T, lv lua.LValue) {
				assert.Equal(t, lua.LTInteger, lv.Type())
				assert.Equal(t, int64(42), int64(lv.(lua.LInteger)))
			},
		},
		{
			name:  "int64",
			input: int64(42),
			check: func(t *testing.T, lv lua.LValue) {
				assert.Equal(t, lua.LTInteger, lv.Type())
				assert.Equal(t, int64(42), int64(lv.(lua.LInteger)))
			},
		},
		{
			name:  "uint32",
			input: uint32(42),
			check: func(t *testing.T, lv lua.LValue) {
				assert.Equal(t, lua.LTInteger, lv.Type())
				assert.Equal(t, int64(42), int64(lv.(lua.LInteger)))
			},
		},
		{
			name:  "uint64 within int64 range",
			input: uint64(42),
			check: func(t *testing.T, lv lua.LValue) {
				assert.Equal(t, lua.LTInteger, lv.Type())
				assert.Equal(t, int64(42), int64(lv.(lua.LInteger)))
			},
		},
		{
			name:  "int64 large",
			input: int64(9007199254740992), // 2^53, max safe integer in float64
			check: func(t *testing.T, lv lua.LValue) {
				assert.Equal(t, lua.LTInteger, lv.Type())
				assert.Equal(t, int64(9007199254740992), int64(lv.(lua.LInteger)))
			},
		},
		{
			name:    "uint64 overflow",
			input:   ^uint64(0),
			wantErr: true,
		},
		{
			name:  "bool true",
			input: true,
			check: func(t *testing.T, lv lua.LValue) {
				assert.Equal(t, lua.LTBool, lv.Type())
				assert.Equal(t, true, bool(lv.(lua.LBool)))
			},
		},
		{
			name:  "bool false",
			input: false,
			check: func(t *testing.T, lv lua.LValue) {
				assert.Equal(t, lua.LTBool, lv.Type())
				assert.Equal(t, false, bool(lv.(lua.LBool)))
			},
		},
		{
			name:  "[]byte",
			input: []byte("hello bytes"),
			check: func(t *testing.T, lv lua.LValue) {
				assert.Equal(t, lua.LTString, lv.Type())
				assert.Equal(t, "hello bytes", string(lv.(lua.LString)))
			},
		},
		{
			name:  "[]byte with binary",
			input: []byte{0x00, 0xFF, 0x01, 0xFE},
			check: func(t *testing.T, lv lua.LValue) {
				assert.Equal(t, lua.LTString, lv.Type())
				assert.Equal(t, []byte{0x00, 0xFF, 0x01, 0xFE}, []byte(lv.(lua.LString)))
			},
		},
		{
			name:  "time.Time",
			input: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
			check: func(t *testing.T, lv lua.LValue) {
				assert.Equal(t, lua.LTNumber, lv.Type())
				expected := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC).Unix()
				assert.Equal(t, float64(expected), float64(lv.(lua.LNumber)))
			},
		},
		{
			name:    "channel (unsupported)",
			input:   make(chan int),
			wantErr: true,
		},
		{
			name:    "func (unsupported)",
			input:   func() {},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lv, err := GoToLua(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			tt.check(t, lv)
		})
	}
}

func TestGoToLua_Slices(t *testing.T) {
	tests := []struct {
		input any
		check func(t *testing.T, lv lua.LValue)
		name  string
	}{
		{
			name:  "empty slice",
			input: []int{},
			check: func(t *testing.T, lv lua.LValue) {
				assert.Equal(t, lua.LTTable, lv.Type())
				tbl := lv.(*lua.LTable)
				assert.Equal(t, 0, tbl.MaxN())
			},
		},
		{
			name:  "nil slice",
			input: ([]int)(nil),
			check: func(t *testing.T, lv lua.LValue) {
				assert.Equal(t, lua.LTNil, lv.Type())
			},
		},
		{
			name:  "int slice",
			input: []int{1, 2, 3},
			check: func(t *testing.T, lv lua.LValue) {
				tbl := lv.(*lua.LTable)
				assert.Equal(t, 3, tbl.MaxN())
				assert.Equal(t, int64(1), int64(tbl.RawGetInt(1).(lua.LInteger)))
				assert.Equal(t, int64(2), int64(tbl.RawGetInt(2).(lua.LInteger)))
				assert.Equal(t, int64(3), int64(tbl.RawGetInt(3).(lua.LInteger)))
			},
		},
		{
			name:  "string slice",
			input: []string{"a", "b", "c"},
			check: func(t *testing.T, lv lua.LValue) {
				tbl := lv.(*lua.LTable)
				assert.Equal(t, 3, tbl.MaxN())
				assert.Equal(t, "a", string(tbl.RawGetInt(1).(lua.LString)))
				assert.Equal(t, "b", string(tbl.RawGetInt(2).(lua.LString)))
				assert.Equal(t, "c", string(tbl.RawGetInt(3).(lua.LString)))
			},
		},
		{
			name:  "nested slice",
			input: [][]int{{1, 2}, {3, 4}},
			check: func(t *testing.T, lv lua.LValue) {
				tbl := lv.(*lua.LTable)
				assert.Equal(t, 2, tbl.MaxN())
				inner1 := tbl.RawGetInt(1).(*lua.LTable)
				assert.Equal(t, int64(1), int64(inner1.RawGetInt(1).(lua.LInteger)))
				assert.Equal(t, int64(2), int64(inner1.RawGetInt(2).(lua.LInteger)))
			},
		},
		{
			name:  "any slice with mixed types",
			input: []any{"string", 42, true, nil},
			check: func(t *testing.T, lv lua.LValue) {
				tbl := lv.(*lua.LTable)
				// MaxN returns last non-nil contiguous index; trailing nil doesn't extend it
				assert.Equal(t, 3, tbl.MaxN())
				assert.Equal(t, "string", string(tbl.RawGetInt(1).(lua.LString)))
				assert.Equal(t, int64(42), int64(tbl.RawGetInt(2).(lua.LInteger)))
				assert.Equal(t, true, bool(tbl.RawGetInt(3).(lua.LBool)))
				assert.Equal(t, lua.LTNil, tbl.RawGetInt(4).Type())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lv, err := GoToLua(tt.input)
			require.NoError(t, err)
			tt.check(t, lv)
		})
	}
}

func TestGoToLua_Maps(t *testing.T) {
	tests := []struct {
		input any
		check func(t *testing.T, lv lua.LValue)
		name  string
	}{
		{
			name:  "nil map",
			input: (map[string]int)(nil),
			check: func(t *testing.T, lv lua.LValue) {
				assert.Equal(t, lua.LTTable, lv.Type())
				tbl := lv.(*lua.LTable)
				count := 0
				tbl.ForEach(func(_, _ lua.LValue) { count++ })
				assert.Equal(t, 0, count)
			},
		},
		{
			name:  "empty map",
			input: map[string]int{},
			check: func(t *testing.T, lv lua.LValue) {
				assert.Equal(t, lua.LTTable, lv.Type())
			},
		},
		{
			name:  "string->int map",
			input: map[string]int{"a": 1, "b": 2},
			check: func(t *testing.T, lv lua.LValue) {
				tbl := lv.(*lua.LTable)
				assert.Equal(t, int64(1), int64(tbl.RawGetString("a").(lua.LInteger)))
				assert.Equal(t, int64(2), int64(tbl.RawGetString("b").(lua.LInteger)))
			},
		},
		{
			name:  "int->string map",
			input: map[int]string{1: "a", 2: "b"},
			check: func(t *testing.T, lv lua.LValue) {
				tbl := lv.(*lua.LTable)
				assert.Equal(t, "a", string(tbl.RawGetString("1").(lua.LString)))
				assert.Equal(t, "b", string(tbl.RawGetString("2").(lua.LString)))
			},
		},
		{
			name: "nested map",
			input: map[string]any{
				"outer": map[string]any{
					"inner": "value",
				},
			},
			check: func(t *testing.T, lv lua.LValue) {
				tbl := lv.(*lua.LTable)
				outer := tbl.RawGetString("outer").(*lua.LTable)
				assert.Equal(t, "value", string(outer.RawGetString("inner").(lua.LString)))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lv, err := GoToLua(tt.input)
			require.NoError(t, err)
			tt.check(t, lv)
		})
	}
}

func TestGoToLua_Structs(t *testing.T) {
	type Inner struct {
		Value string `json:"value"`
	}

	type Outer struct {
		Inner    *Inner            `json:"inner"`
		Metadata map[string]string `json:"metadata"`
		Name     string            `json:"name"`
		Tags     []string          `json:"tags"`
		Count    int               `json:"count"`
	}

	tests := []struct {
		input any
		check func(t *testing.T, lv lua.LValue)
		name  string
	}{
		{
			name: "simple struct",
			input: struct {
				Name string `json:"name"`
				Age  int    `json:"age"`
			}{Name: "test", Age: 42},
			check: func(t *testing.T, lv lua.LValue) {
				tbl := lv.(*lua.LTable)
				assert.Equal(t, "test", string(tbl.RawGetString("name").(lua.LString)))
				assert.Equal(t, int64(42), int64(tbl.RawGetString("age").(lua.LInteger)))
			},
		},
		{
			name: "struct with nil pointer",
			input: Outer{
				Name:  "test",
				Count: 10,
				Inner: nil,
				Tags:  nil,
			},
			check: func(t *testing.T, lv lua.LValue) {
				tbl := lv.(*lua.LTable)
				assert.Equal(t, "test", string(tbl.RawGetString("name").(lua.LString)))
				assert.Equal(t, lua.LTNil, tbl.RawGetString("inner").Type())
				assert.Equal(t, lua.LTNil, tbl.RawGetString("tags").Type())
			},
		},
		{
			name: "struct with nested pointer",
			input: Outer{
				Name:  "test",
				Count: 10,
				Inner: &Inner{Value: "nested"},
			},
			check: func(t *testing.T, lv lua.LValue) {
				tbl := lv.(*lua.LTable)
				inner := tbl.RawGetString("inner").(*lua.LTable)
				assert.Equal(t, "nested", string(inner.RawGetString("value").(lua.LString)))
			},
		},
		{
			name: "pointer to struct",
			input: &Outer{
				Name:  "test",
				Count: 10,
			},
			check: func(t *testing.T, lv lua.LValue) {
				tbl := lv.(*lua.LTable)
				assert.Equal(t, "test", string(tbl.RawGetString("name").(lua.LString)))
			},
		},
		{
			name:  "nil pointer to struct",
			input: (*Outer)(nil),
			check: func(t *testing.T, lv lua.LValue) {
				assert.Equal(t, lua.LTNil, lv.Type())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lv, err := GoToLua(tt.input)
			require.NoError(t, err)
			tt.check(t, lv)
		})
	}
}

func TestGoToLua_LuaValue_Passthrough(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tests := []struct {
		input lua.LValue
		name  string
	}{
		{lua.LNil, "LNil"},
		{lua.LTrue, "LTrue"},
		{lua.LFalse, "LFalse"},
		{lua.LNumber(42), "LNumber"},
		{lua.LString("hello"), "LString"},
		{l.NewTable(), "LTable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lv, err := GoToLua(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.input, lv)
		})
	}
}

// =============================================================================
// UNIT TESTS - Export (Deep Copy & Immutability)
// =============================================================================

func TestExportPayload_DeepCopy(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	// Create a nested table
	inner := l.NewTable()
	inner.RawSetString("key", lua.LString("value"))

	outer := l.NewTable()
	outer.RawSetString("inner", inner)
	outer.RawSetInt(1, lua.LString("array_element"))

	// Export
	p := ExportPayload(outer)
	require.NotNil(t, p)

	exported := p.Data().(lua.LValue)
	exportedTable := exported.(*lua.LTable)

	// Verify it's a copy (different pointer)
	assert.NotSame(t, outer, exportedTable)

	// Verify nested table is also copied
	exportedInner := exportedTable.RawGetString("inner").(*lua.LTable)
	assert.NotSame(t, inner, exportedInner)

	// Verify values are preserved
	assert.Equal(t, "value", string(exportedInner.RawGetString("key").(lua.LString)))
	assert.Equal(t, "array_element", string(exportedTable.RawGetInt(1).(lua.LString)))
}

func TestExportPayload_CircularReference(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	// Create circular reference
	table1 := l.NewTable()
	table2 := l.NewTable()
	table1.RawSetString("ref", table2)
	table2.RawSetString("ref", table1)

	// Should not panic or infinite loop
	p := ExportPayload(table1)
	require.NotNil(t, p)
}

func TestExportPayload_UserData(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	table := l.NewTable()
	ud := l.NewUserData()
	ud.Value = "some value"
	table.RawSetString("userdata", ud)

	p := ExportPayload(table)
	exported := p.Data().(*lua.LTable)

	// UserData should be converted to nil
	assert.Equal(t, lua.LTNil, exported.RawGetString("userdata").Type())
}

func TestExportPayload_Primitives(t *testing.T) {
	tests := []struct {
		input lua.LValue
		name  string
	}{
		{lua.LNil, "nil"},
		{lua.LTrue, "bool"},
		{lua.LNumber(42), "number"},
		{lua.LString("hello"), "string"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := ExportPayload(tt.input)
			require.NotNil(t, p)
			assert.Equal(t, tt.input, p.Data())
		})
	}
}

// =============================================================================
// UNIT TESTS - JSON Transcoding
// =============================================================================

func TestJSONToLua_Transcode(t *testing.T) {
	tests := []struct {
		check   func(t *testing.T, lv lua.LValue)
		name    string
		json    string
		wantErr bool
	}{
		{
			name: "simple object",
			json: `{"name":"test","count":42}`,
			check: func(t *testing.T, lv lua.LValue) {
				tbl := lv.(*lua.LTable)
				assert.Equal(t, "test", string(tbl.RawGetString("name").(lua.LString)))
				assert.Equal(t, int64(42), int64(tbl.RawGetString("count").(lua.LInteger)))
			},
		},
		{
			name: "array",
			json: `[1,2,3]`,
			check: func(t *testing.T, lv lua.LValue) {
				tbl := lv.(*lua.LTable)
				assert.Equal(t, int64(1), int64(tbl.RawGetInt(1).(lua.LInteger)))
				assert.Equal(t, int64(2), int64(tbl.RawGetInt(2).(lua.LInteger)))
				assert.Equal(t, int64(3), int64(tbl.RawGetInt(3).(lua.LInteger)))
			},
		},
		{
			name: "nested object",
			json: `{"outer":{"inner":"value"}}`,
			check: func(t *testing.T, lv lua.LValue) {
				tbl := lv.(*lua.LTable)
				outer := tbl.RawGetString("outer").(*lua.LTable)
				assert.Equal(t, "value", string(outer.RawGetString("inner").(lua.LString)))
			},
		},
		{
			name: "null value",
			json: `{"key":null}`,
			check: func(t *testing.T, lv lua.LValue) {
				tbl := lv.(*lua.LTable)
				assert.Equal(t, lua.LTNil, tbl.RawGetString("key").Type())
			},
		},
		{
			name: "boolean values",
			json: `{"yes":true,"no":false}`,
			check: func(t *testing.T, lv lua.LValue) {
				tbl := lv.(*lua.LTable)
				assert.Equal(t, true, bool(tbl.RawGetString("yes").(lua.LBool)))
				assert.Equal(t, false, bool(tbl.RawGetString("no").(lua.LBool)))
			},
		},
		{
			name: "empty object",
			json: `{}`,
			check: func(t *testing.T, lv lua.LValue) {
				tbl := lv.(*lua.LTable)
				assert.Equal(t, lua.LTTable, lv.Type())
				count := 0
				tbl.ForEach(func(_, _ lua.LValue) { count++ })
				assert.Equal(t, 0, count)
			},
		},
		{
			name: "empty array",
			json: `[]`,
			check: func(t *testing.T, lv lua.LValue) {
				tbl := lv.(*lua.LTable)
				assert.Equal(t, lua.LTTable, lv.Type())
				assert.Equal(t, 0, tbl.MaxN())
			},
		},
		{
			name:    "invalid json",
			json:    `{invalid`,
			wantErr: true,
		},
	}

	transcoder := &JSONToLua{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := payload.NewPayload([]byte(tt.json), payload.JSON)
			result, err := transcoder.Transcode(p)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			tt.check(t, result.Data().(lua.LValue))
		})
	}
}

func TestLuaToJSON_Transcode(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tests := []struct {
		name   string
		setup  func() lua.LValue
		expect string
	}{
		{
			name: "simple table",
			setup: func() lua.LValue {
				tbl := l.NewTable()
				tbl.RawSetString("name", lua.LString("test"))
				tbl.RawSetString("count", lua.LNumber(42))
				return tbl
			},
			expect: `{"count":42,"name":"test"}`,
		},
		{
			name: "array",
			setup: func() lua.LValue {
				tbl := l.NewTable()
				tbl.RawSetInt(1, lua.LNumber(1))
				tbl.RawSetInt(2, lua.LNumber(2))
				tbl.RawSetInt(3, lua.LNumber(3))
				return tbl
			},
			expect: `[1,2,3]`,
		},
		{
			name: "string",
			setup: func() lua.LValue {
				return lua.LString("hello")
			},
			expect: `"hello"`,
		},
		{
			name: "number",
			setup: func() lua.LValue {
				return lua.LNumber(42)
			},
			expect: `42`,
		},
		{
			name: "bool",
			setup: func() lua.LValue {
				return lua.LTrue
			},
			expect: `true`,
		},
		{
			name: "empty table from LState",
			setup: func() lua.LValue {
				return l.NewTable()
			},
			// Empty tables from LState encode as {} since Strdict is initialized
			expect: `{}`,
		},
		{
			name: "empty table from CreateTable",
			setup: func() lua.LValue {
				return lua.CreateTable(0, 0)
			},
			// Empty tables without initialized dicts encode as []
			expect: `[]`,
		},
	}

	transcoder := &ToJSON{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := payload.NewPayload(tt.setup(), payload.Lua)
			result, err := transcoder.Transcode(p)
			require.NoError(t, err)
			assert.JSONEq(t, tt.expect, string(result.Data().([]byte)))
		})
	}
}

func TestJSONRoundTrip(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	// Create a complex Lua table
	inner := l.NewTable()
	inner.RawSetString("nested", lua.LString("value"))

	original := l.NewTable()
	original.RawSetString("string", lua.LString("hello"))
	original.RawSetString("number", lua.LNumber(42.5))
	original.RawSetString("bool", lua.LTrue)
	original.RawSetString("inner", inner)

	// Lua -> JSON
	toLua := &ToJSON{}
	jsonPayload, err := toLua.Transcode(payload.NewPayload(original, payload.Lua))
	require.NoError(t, err)

	// JSON -> Lua
	toJSON := &JSONToLua{}
	luaPayload, err := toJSON.Transcode(jsonPayload)
	require.NoError(t, err)

	// Verify
	result := luaPayload.Data().(*lua.LTable)
	assert.Equal(t, "hello", string(result.RawGetString("string").(lua.LString)))
	assert.Equal(t, 42.5, float64(result.RawGetString("number").(lua.LNumber)))
	assert.Equal(t, true, bool(result.RawGetString("bool").(lua.LBool)))

	innerResult := result.RawGetString("inner").(*lua.LTable)
	assert.Equal(t, "value", string(innerResult.RawGetString("nested").(lua.LString)))
}

// =============================================================================
// UNIT TESTS - String/Bytes Transcoding
// =============================================================================

func TestLuaToString_AllTypes(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	transcoder := &ToString{}
	tests := []struct {
		name   string
		input  lua.LValue
		expect string
	}{
		{"string", lua.LString("hello"), "hello"},
		{"number", lua.LNumber(42.5), "42.5"},
		{"integer number", lua.LNumber(42), "42"},
		{"bool true", lua.LTrue, "true"},
		{"bool false", lua.LFalse, "false"},
		{"nil", lua.LNil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := payload.NewPayload(tt.input, payload.Lua)
			result, err := transcoder.Transcode(p)
			require.NoError(t, err)
			assert.Equal(t, tt.expect, result.Data().(string))
		})
	}
}

func TestLuaToBytes_AllTypes(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	transcoder := &ToBytes{}
	tests := []struct {
		name   string
		input  lua.LValue
		expect []byte
	}{
		{"string", lua.LString("hello"), []byte("hello")},
		{"number", lua.LNumber(42.5), []byte("42.5")},
		{"binary", lua.LString([]byte{0x00, 0xFF}), []byte{0x00, 0xFF}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := payload.NewPayload(tt.input, payload.Lua)
			result, err := transcoder.Transcode(p)
			require.NoError(t, err)
			assert.Equal(t, tt.expect, result.Data().([]byte))
		})
	}
}

// =============================================================================
// EDGE CASE TESTS
// =============================================================================

func TestGoToLua_EdgeCases(t *testing.T) {
	t.Run("deeply nested structure", func(t *testing.T) {
		// Create 100 levels deep
		var data any = "leaf"
		for i := 0; i < 100; i++ {
			data = map[string]any{"nested": data}
		}

		lv, err := GoToLua(data)
		require.NoError(t, err)

		// Traverse 99 levels (stops at innermost table containing the leaf)
		tbl := lv.(*lua.LTable)
		for i := 0; i < 99; i++ {
			tbl = tbl.RawGetString("nested").(*lua.LTable)
		}
		// Verify the leaf value
		leaf := tbl.RawGetString("nested").(lua.LString)
		assert.Equal(t, "leaf", string(leaf))
	})

	t.Run("large array", func(t *testing.T) {
		arr := make([]int, 10000)
		for i := range arr {
			arr[i] = i
		}

		lv, err := GoToLua(arr)
		require.NoError(t, err)

		tbl := lv.(*lua.LTable)
		assert.Equal(t, 10000, tbl.MaxN())
		assert.Equal(t, int64(9999), int64(tbl.RawGetInt(10000).(lua.LInteger)))
	})

	t.Run("large map", func(t *testing.T) {
		m := make(map[string]int, 10000)
		for i := 0; i < 10000; i++ {
			m[string(rune('a'+i%26))+string(rune(i))] = i
		}

		lv, err := GoToLua(m)
		require.NoError(t, err)
		assert.Equal(t, lua.LTTable, lv.Type())
	})

	t.Run("special float values", func(t *testing.T) {
		tests := []float64{
			0.0,
			math.Copysign(0, -1),
			math.Inf(1),
			math.Inf(-1),
			// NaN is tricky to test because NaN != NaN
		}

		for _, f := range tests {
			lv, err := GoToLua(f)
			require.NoError(t, err)
			assert.Equal(t, f, float64(lv.(lua.LNumber)))
		}
	})

	t.Run("error type", func(t *testing.T) {
		err := assert.AnError
		lv, convErr := GoToLua(err)
		require.NoError(t, convErr)
		// Should return a valid LValue
		assert.NotNil(t, lv)
	})
}

// =============================================================================
// STRESS TESTS
// =============================================================================

func TestStress_ConcurrentGoToLua(t *testing.T) {
	skipUnlessStress(t)
	const (
		goroutines = 100
		iterations = 1000
	)

	var wg sync.WaitGroup
	var errors atomic.Int64

	data := map[string]any{
		"string": "hello",
		"number": 42,
		"bool":   true,
		"array":  []int{1, 2, 3},
		"nested": map[string]any{"key": "value"},
	}

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				lv, err := GoToLua(data)
				if err != nil {
					errors.Add(1)
					continue
				}
				// Verify basic structure
				tbl := lv.(*lua.LTable)
				if tbl.RawGetString("string").Type() != lua.LTString {
					errors.Add(1)
				}
			}
		}()
	}

	wg.Wait()
	assert.Equal(t, int64(0), errors.Load(), "concurrent conversions should not produce errors")
}

func TestStress_ConcurrentExportPayload(t *testing.T) {
	skipUnlessStress(t)
	const (
		goroutines = 100
		iterations = 1000
	)

	var wg sync.WaitGroup
	var errors atomic.Int64

	l := lua.NewState()
	defer l.Close()

	// Create a shared table (read-only access)
	original := l.NewTable()
	original.RawSetString("key", lua.LString("value"))
	inner := l.NewTable()
	inner.RawSetString("nested", lua.LString("data"))
	original.RawSetString("inner", inner)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				p := ExportPayload(original)
				if p == nil {
					errors.Add(1)
					continue
				}
				// Verify it's a valid copy
				tbl := p.Data().(*lua.LTable)
				if tbl.RawGetString("key").Type() != lua.LTString {
					errors.Add(1)
				}
			}
		}()
	}

	wg.Wait()
	assert.Equal(t, int64(0), errors.Load())
}

func TestStress_ConcurrentJSONTranscode(t *testing.T) {
	skipUnlessStress(t)
	const (
		goroutines = 50
		iterations = 500
	)

	var wg sync.WaitGroup
	var errors atomic.Int64

	jsonData := `{"name":"test","count":42,"tags":["a","b","c"],"nested":{"key":"value"}}`
	transcoder := &JSONToLua{}

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				p := payload.NewPayload([]byte(jsonData), payload.JSON)
				result, err := transcoder.Transcode(p)
				if err != nil {
					errors.Add(1)
					continue
				}
				tbl := result.Data().(*lua.LTable)
				if string(tbl.RawGetString("name").(lua.LString)) != "test" {
					errors.Add(1)
				}
			}
		}()
	}

	wg.Wait()
	assert.Equal(t, int64(0), errors.Load())
}

func TestStress_StructFieldCache(t *testing.T) {
	skipUnlessStress(t)
	const (
		goroutines = 100
		iterations = 1000
	)

	type TestStruct struct {
		Field1 string `json:"field1"`
		Field2 int    `json:"field2"`
		Field3 bool   `json:"field3"`
	}

	var wg sync.WaitGroup
	var errors atomic.Int64

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				input := TestStruct{
					Field1: "test",
					Field2: 42,
					Field3: true,
				}
				lv, err := GoToLua(input)
				if err != nil {
					errors.Add(1)
					continue
				}
				tbl := lv.(*lua.LTable)
				if string(tbl.RawGetString("field1").(lua.LString)) != "test" {
					errors.Add(1)
				}
			}
		}()
	}

	wg.Wait()
	assert.Equal(t, int64(0), errors.Load())
}

// =============================================================================
// BENCHMARKS
// =============================================================================

func BenchmarkGoToLua_String(b *testing.B) {
	input := "hello world"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = GoToLua(input)
	}
}

func BenchmarkGoToLua_Int(b *testing.B) {
	input := 42
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = GoToLua(input)
	}
}

func BenchmarkGoToLua_SmallMap(b *testing.B) {
	input := map[string]any{
		"name": "test",
		"age":  30,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = GoToLua(input)
	}
}

func BenchmarkGoToLua_LargeMap(b *testing.B) {
	input := make(map[string]any, 100)
	for i := 0; i < 100; i++ {
		input[string(rune('a'+i%26))+string(rune(i))] = i
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = GoToLua(input)
	}
}

func BenchmarkGoToLua_SmallSlice(b *testing.B) {
	input := []int{1, 2, 3, 4, 5}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = GoToLua(input)
	}
}

func BenchmarkGoToLua_LargeSlice(b *testing.B) {
	input := make([]int, 1000)
	for i := range input {
		input[i] = i
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = GoToLua(input)
	}
}

func BenchmarkGoToLua_Struct(b *testing.B) {
	type TestStruct struct {
		Meta  map[string]string `json:"meta"`
		Name  string            `json:"name"`
		Tags  []string          `json:"tags"`
		Count int               `json:"count"`
	}
	input := TestStruct{
		Name:  "test",
		Count: 42,
		Tags:  []string{"a", "b", "c"},
		Meta:  map[string]string{"key": "value"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = GoToLua(input)
	}
}

func BenchmarkGoToLua_NestedStruct(b *testing.B) {
	type Inner struct {
		Value string `json:"value"`
	}
	type Outer struct {
		Inner *Inner `json:"inner"`
		Name  string `json:"name"`
	}
	input := Outer{
		Name:  "test",
		Inner: &Inner{Value: "nested"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = GoToLua(input)
	}
}

func BenchmarkExportPayload_SimpleTable(b *testing.B) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.NewTable()
	tbl.RawSetString("key", lua.LString("value"))
	tbl.RawSetString("count", lua.LNumber(42))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ExportPayload(tbl)
	}
}

func BenchmarkExportPayload_NestedTable(b *testing.B) {
	l := lua.NewState()
	defer l.Close()

	inner := l.NewTable()
	inner.RawSetString("nested", lua.LString("value"))

	outer := l.NewTable()
	outer.RawSetString("inner", inner)
	outer.RawSetString("key", lua.LString("value"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ExportPayload(outer)
	}
}

func BenchmarkExportPayload_LargeTable(b *testing.B) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.NewTable()
	for i := 0; i < 100; i++ {
		tbl.RawSetString(string(rune('a'+i%26))+string(rune(i)), lua.LNumber(i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ExportPayload(tbl)
	}
}

func BenchmarkJSONToLua_Simple(b *testing.B) {
	jsonData := []byte(`{"name":"test","count":42}`)
	transcoder := &JSONToLua{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := payload.NewPayload(jsonData, payload.JSON)
		_, _ = transcoder.Transcode(p)
	}
}

func BenchmarkJSONToLua_Complex(b *testing.B) {
	jsonData := []byte(`{"name":"test","count":42,"tags":["a","b","c"],"nested":{"key":"value","array":[1,2,3]}}`)
	transcoder := &JSONToLua{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := payload.NewPayload(jsonData, payload.JSON)
		_, _ = transcoder.Transcode(p)
	}
}

func BenchmarkLuaToJSON_Simple(b *testing.B) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.NewTable()
	tbl.RawSetString("name", lua.LString("test"))
	tbl.RawSetString("count", lua.LNumber(42))

	transcoder := &ToJSON{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := payload.NewPayload(tbl, payload.Lua)
		_, _ = transcoder.Transcode(p)
	}
}

func BenchmarkToGoAny_Table(b *testing.B) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.NewTable()
	tbl.RawSetString("name", lua.LString("test"))
	tbl.RawSetString("count", lua.LNumber(42))
	tbl.RawSetString("active", lua.LTrue)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = value.ToGoAny(tbl)
	}
}

func BenchmarkStructFieldCache(b *testing.B) {
	type LargeStruct struct {
		Field1  string `json:"field1"`
		Field2  string `json:"field2"`
		Field3  string `json:"field3"`
		Field4  string `json:"field4"`
		Field5  string `json:"field5"`
		Field6  string `json:"field6"`
		Field7  string `json:"field7"`
		Field8  string `json:"field8"`
		Field9  string `json:"field9"`
		Field10 string `json:"field10"`
	}

	input := LargeStruct{
		Field1: "1", Field2: "2", Field3: "3", Field4: "4", Field5: "5",
		Field6: "6", Field7: "7", Field8: "8", Field9: "9", Field10: "10",
	}

	// Warm up cache
	_, _ = GoToLua(input)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = GoToLua(input)
	}
}

// Parallel benchmarks
func BenchmarkGoToLua_Parallel(b *testing.B) {
	input := map[string]any{
		"name":  "test",
		"count": 42,
		"tags":  []string{"a", "b", "c"},
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = GoToLua(input)
		}
	})
}

func BenchmarkExportPayload_Parallel(b *testing.B) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.NewTable()
	tbl.RawSetString("key", lua.LString("value"))
	tbl.RawSetString("count", lua.LNumber(42))

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = ExportPayload(tbl)
		}
	})
}

func BenchmarkJSONToLua_Parallel(b *testing.B) {
	jsonData := []byte(`{"name":"test","count":42,"tags":["a","b","c"]}`)
	transcoder := &JSONToLua{}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			p := payload.NewPayload(jsonData, payload.JSON)
			_, _ = transcoder.Transcode(p)
		}
	})
}

// Memory allocation benchmark
func BenchmarkGoToLua_Allocs(b *testing.B) {
	input := map[string]any{
		"string": "hello",
		"number": 42,
		"bool":   true,
		"array":  []int{1, 2, 3, 4, 5},
		"nested": map[string]any{"key": "value"},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = GoToLua(input)
	}
}

// =============================================================================
// RACE CONDITION TESTS
// =============================================================================

func TestRace_GlobalJSONState(*testing.T) {
	// This test specifically targets the global `state` variable in json.go
	// which could cause race conditions
	const (
		goroutines = 50
		iterations = 100
	)

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			transcoder := &JSONToLua{}
			for j := 0; j < iterations; j++ {
				jsonData := []byte(`{"key":"value","num":42}`)
				p := payload.NewPayload(jsonData, payload.JSON)
				_, _ = transcoder.Transcode(p)
			}
		}()
	}

	wg.Wait()
}

// =============================================================================
// FUZZ-LIKE TESTS
// =============================================================================

func TestGoToLua_RandomTypes(t *testing.T) {
	// Test various type combinations
	inputs := []any{
		nil,
		true,
		false,
		0,
		-1,
		int8(127),
		int16(32767),
		int32(2147483647),
		int64(9223372036854775807),
		float32(3.14),
		3.14159265358979,
		"",
		"a",
		"hello world",
		[]byte{},
		[]byte{0, 1, 2, 3},
		[]int{},
		[]int{1},
		[]string{"a", "b"},
		map[string]int{},
		map[string]any{"nested": map[string]any{}},
		struct{ X int }{X: 1},
		&struct{ Y string }{Y: "test"},
		time.Now(),
		time.Duration(100),
	}

	for i, input := range inputs {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			// Should not panic
			lv, err := GoToLua(input)
			if err == nil {
				assert.NotNil(t, lv)
			}
		})
	}
}

func TestGoToLua_JSONUnmarshaledData(t *testing.T) {
	// Test converting data that came from JSON unmarshal (common use case)
	jsonInputs := []string{
		`null`,
		`true`,
		`false`,
		`42`,
		`3.14`,
		`"hello"`,
		`[]`,
		`[1,2,3]`,
		`{}`,
		`{"key":"value"}`,
		`{"nested":{"deep":{"value":42}}}`,
		`{"array":[1,"two",true,null]}`,
	}

	for _, jsonInput := range jsonInputs {
		t.Run(jsonInput, func(t *testing.T) {
			var data any
			err := json.Unmarshal([]byte(jsonInput), &data)
			require.NoError(t, err)

			lv, err := GoToLua(data)
			require.NoError(t, err)
			assert.NotNil(t, lv)
		})
	}
}

// Memory pressure test
func TestStress_MemoryPressure(t *testing.T) {
	skipUnlessStress(t)

	// Force GC before starting
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	startAlloc := m.TotalAlloc

	l := lua.NewState()
	defer l.Close()

	// Create and convert many tables
	for i := 0; i < 10000; i++ {
		tbl := l.NewTable()
		for j := 0; j < 10; j++ {
			tbl.RawSetString(string(rune('a'+j)), lua.LNumber(j))
		}
		_ = ExportPayload(tbl)
	}

	// Force GC and check memory
	runtime.GC()
	runtime.ReadMemStats(&m)
	allocDiff := m.TotalAlloc - startAlloc

	// Should not allocate excessive memory (less than 100MB for this test)
	assert.Less(t, allocDiff, uint64(100*1024*1024), "excessive memory allocation detected")
}
