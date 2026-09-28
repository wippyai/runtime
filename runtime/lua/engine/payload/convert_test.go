// SPDX-License-Identifier: MPL-2.0

package payload

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	jsonlua "github.com/wippyai/runtime/runtime/lua/modules/json"
)

func TestToGoAny(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tests := []struct {
		input lua.LValue
		want  any
		name  string
	}{
		{lua.LNil, nil, "Nil"},
		{lua.LBool(true), true, "Bool"},
		{lua.LNumber(42.5), 42.5, "Number"},
		{lua.LString("hello"), "hello", "String"},
		{
			func() lua.LValue {
				tbl := l.NewTable()
				l.SetTable(tbl, lua.LString("key1"), lua.LString("value1"))
				l.SetTable(tbl, lua.LString("key2"), lua.LNumber(2))
				return tbl
			}(),
			map[string]any{"key1": "value1", "key2": 2.0},
			"Table (map)",
		},
		{
			func() lua.LValue {
				tbl := l.NewTable()
				l.SetTable(tbl, lua.LNumber(1), lua.LString("one"))
				l.SetTable(tbl, lua.LNumber(2), lua.LNumber(2))
				l.SetTable(tbl, lua.LNumber(3), lua.LBool(true))
				return tbl
			}(),
			[]any{"one", 2.0, true},
			"Table (array)",
		},
		{
			l.NewFunction(func(*lua.LState) int { return 0 }),
			"function", // We'll just check if the result is a string (and optionally contains "function")
			"Other",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := value.ToGoAny(tt.input)

			if tt.name == "Other" {
				// Special handling for the "Other" case
				gotString, ok := got.(string)
				if !ok {
					t.Errorf("value.ToGoAny() for function: expected a string, got %T", got)
				}
				if !strings.Contains(gotString, "function") {
					t.Errorf("value.ToGoAny() for function: expected string to contain 'function', got '%s'", gotString)
				}
			} else if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("value.ToGoAny() = %v, want %v", got, tt.want)
			}
		})
	}
}
func TestGoToLua(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tests := []struct {
		input   any
		want    lua.LValue
		name    string
		wantErr bool
	}{
		{"hello", lua.LString("hello"), "String", false},
		{42.5, lua.LNumber(42.5), "Float64", false},
		{10, lua.LInteger(10), "Int", false},
		{uint32(10), lua.LInteger(10), "Uint32", false},
		{true, lua.LBool(true), "Bool", false},
		{nil, lua.LNil, "Nil", false},
		{[]int{1, 2, 3}, func() lua.LValue {
			tbl := l.NewTable()
			l.SetTable(tbl, lua.LNumber(1), lua.LInteger(1))
			l.SetTable(tbl, lua.LNumber(2), lua.LInteger(2))
			l.SetTable(tbl, lua.LNumber(3), lua.LInteger(3))
			return tbl
		}(), "Int Array", false},
		{[]string{"a", "b"}, func() lua.LValue {
			tbl := l.NewTable()
			l.SetTable(tbl, lua.LNumber(1), lua.LString("a"))
			l.SetTable(tbl, lua.LNumber(2), lua.LString("b"))
			return tbl
		}(), "String Array", false},
		{
			map[string]any{"name": "John", "age": 30},
			func() lua.LValue {
				tbl := l.NewTable()
				l.SetTable(tbl, lua.LString("name"), lua.LString("John"))
				l.SetTable(tbl, lua.LString("age"), lua.LInteger(30))
				return tbl
			}(),
			"Map[string]any",
			false,
		},
		{
			map[string]string{"name": "John", "city": "New York"},
			func() lua.LValue {
				tbl := l.NewTable()
				l.SetTable(tbl, lua.LString("name"), lua.LString("John"))
				l.SetTable(tbl, lua.LString("city"), lua.LString("New York"))
				return tbl
			}(),
			"Map[string]string",
			false,
		},
		{
			[]any{"hello", 42, true},
			func() lua.LValue {
				tbl := l.NewTable()
				l.SetTable(tbl, lua.LNumber(1), lua.LString("hello"))
				l.SetTable(tbl, lua.LNumber(2), lua.LInteger(42))
				l.SetTable(tbl, lua.LNumber(3), lua.LBool(true))
				return tbl
			}(),
			"Any Array",
			false,
		},
		{^uint64(0), nil, "Uint64 Overflow", true},
		{make(chan int), nil, "Unsupported", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GoToLua(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			// Deep comparison for tables, otherwise use reflect.DeepEqual
			if tt.want != nil && tt.want.Type() == lua.LTTable {
				if got.Type() != lua.LTTable {
					t.Errorf("GoToLua() type = %v, want %v", got.Type(), tt.want.Type())
					return
				}
				wantTable := tt.want.(*lua.LTable)
				gotTable := got.(*lua.LTable)

				// Compare lengths (for arrays)
				if wantTable.MaxN() > 0 && wantTable.MaxN() != gotTable.MaxN() {
					t.Errorf("GoToLua() table length = %v, want %v", gotTable.MaxN(), wantTable.MaxN())
				}

				// Compare keys and values
				wantTable.ForEach(func(k lua.LValue, v lua.LValue) {
					gotValue := gotTable.RawGet(k)
					if !reflect.DeepEqual(value.ToGoAny(gotValue), value.ToGoAny(v)) {
						t.Errorf("GoToLua() table value for key %v = %v, want %v", k, value.ToGoAny(gotValue), value.ToGoAny(v))
					}
				})
			} else if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GoToLua() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGoToLuaExtended(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	type Address struct {
		Street string `json:"street"`
		City   string `json:"city"`
	}

	type Person struct {
		CreatedAt  time.Time `json:"created_at"`
		Address    *Address  `json:"address"`
		Metadata   map[string]any
		Name       string `json:"name"`
		unexported string
		Tags       []string `json:"tags"`
		Age        int      `json:"age"`
	}

	fixedTime := time.Date(2024, 2, 19, 12, 30, 0, 0, time.UTC)

	tests := []struct {
		input   any
		want    map[string]any
		name    string
		wantErr bool
	}{
		{
			name:  "Time.Time",
			input: fixedTime,
			want: map[string]any{
				"_raw": float64(fixedTime.Unix()),
			},
			wantErr: false,
		},
		{
			name: "Simple struct",
			input: Address{
				Street: "123 Main St",
				City:   "New York",
			},
			want: map[string]any{
				"street": "123 Main St",
				"city":   "New York",
			},
			wantErr: false,
		},
		{
			name: "Struct pointer",
			input: &Address{
				Street: "123 Main St",
				City:   "New York",
			},
			want: map[string]any{
				"street": "123 Main St",
				"city":   "New York",
			},
			wantErr: false,
		},
		{
			name: "Complex struct with nested types",
			input: Person{
				Name:      "John Doe",
				Age:       30,
				CreatedAt: fixedTime,
				Address: &Address{
					Street: "123 Main St",
					City:   "New York",
				},
				Tags: []string{"user", "admin"},
				Metadata: map[string]any{
					"visits": 42,
					"preferences": map[string]any{
						"theme": "dark",
					},
				},
				unexported: "this should be ignored",
			},
			want: map[string]any{
				"name":       "John Doe",
				"age":        int64(30),
				"created_at": float64(fixedTime.Unix()),
				"address": map[string]any{
					"street": "123 Main St",
					"city":   "New York",
				},
				"tags": []any{"user", "admin"},
				"Metadata": map[string]any{
					"visits": int64(42),
					"preferences": map[string]any{
						"theme": "dark",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Nil pointer struct field",
			input: Person{
				Name:      "John Doe",
				Age:       30,
				CreatedAt: fixedTime,
				Address:   nil,
				Tags:      []string{},
				Metadata:  nil,
			},
			want: map[string]any{
				"name":       "John Doe",
				"age":        int64(30),
				"created_at": float64(fixedTime.Unix()),
				"tags":       []any{},          // empty slice keeps its list shape
				"Metadata":   map[string]any{}, // nil map becomes an empty object
			},
			wantErr: false,
		},
		{
			name: "cancel event",
			input: topology.CancelEvent{
				Kind: topology.Cancel,
				At:   fixedTime,
				From: pid.PID{
					Node:   "node",
					Host:   "host",
					UniqID: "id",
				},
				Reason: "boom",
			},
			want: map[string]any{
				"at":     float64(fixedTime.Unix()),
				"kind":   "pid.cancel",
				"from":   "{node@host|id}",
				"reason": "boom",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GoToLua(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			// For the Time.Time conversion test, handle separately
			if tt.name == "Time.Time" {
				n, ok := got.(lua.LNumber)
				assert.True(t, ok, "expected LNumber for time conversion")
				assert.Equal(t, float64(fixedTime.Unix()), float64(n))
				return
			}

			gotMap := value.ToGoAny(got)
			assert.Equal(t, tt.want, gotMap)
		})
	}
}

// TestGoToLua_EmptyMapShape pins that an empty Go map reaches Lua with an
// object allocation, so it stays an object through encoding and round trips.
func TestGoToLua_EmptyMapShape(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	t.Run("empty map is object shaped", func(t *testing.T) {
		got, err := GoToLua(map[string]any{})
		assert.NoError(t, err)
		tbl, ok := got.(*lua.LTable)
		assert.True(t, ok, "got %T", got)
		assert.NotNil(t, tbl.Strdict, "empty map must keep a hash allocation")
		assert.Equal(t, 0, tbl.MaxN())
		assert.Equal(t, 0, tbl.Len())
	})

	t.Run("nil map is object shaped", func(t *testing.T) {
		got, err := GoToLua(map[string]any(nil))
		assert.NoError(t, err)
		tbl, ok := got.(*lua.LTable)
		assert.True(t, ok, "got %T", got)
		assert.NotNil(t, tbl.Strdict, "nil map must keep a hash allocation")
	})

	t.Run("struct nil map field is object shaped", func(t *testing.T) {
		type config struct {
			Fields map[string]string
		}
		got, err := GoToLua(config{})
		assert.NoError(t, err)
		tbl, ok := got.(*lua.LTable)
		assert.True(t, ok, "got %T", got)
		field, ok := tbl.RawGetString("Fields").(*lua.LTable)
		assert.True(t, ok, "Fields = %T", tbl.RawGetString("Fields"))
		assert.NotNil(t, field.Strdict, "nil map field must keep a hash allocation")
	})

	t.Run("nested empty map stays object shaped", func(t *testing.T) {
		got, err := GoToLua(map[string]any{"inner": map[string]any{}})
		assert.NoError(t, err)
		outer, ok := got.(*lua.LTable)
		assert.True(t, ok, "got %T", got)
		inner, ok := outer.RawGetString("inner").(*lua.LTable)
		assert.True(t, ok, "inner = %T", outer.RawGetString("inner"))
		assert.NotNil(t, inner.Strdict, "nested empty map must keep a hash allocation")
	})

	t.Run("empty slice stays array shaped", func(t *testing.T) {
		got, err := GoToLua([]any{})
		assert.NoError(t, err)
		tbl, ok := got.(*lua.LTable)
		assert.True(t, ok, "got %T", got)
		assert.Nil(t, tbl.Strdict, "empty slice must not gain a hash allocation")
	})
}

// TestEmptyTableShapeRoundTripThroughJSON proves both directions compose with
// the json module's empty-table encoding: an object stays {}, a list stays [].
func TestEmptyTableShapeRoundTripThroughJSON(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	encodeLua := func(v lua.LValue) string {
		encoded, err := jsonlua.Encode(v)
		assert.NoError(t, err)
		return string(encoded)
	}

	t.Run("lua to go to json keeps object", func(t *testing.T) {
		table := l.CreateTable(0, 1)
		converted, err := GoToLua(value.ToGoAny(table))
		assert.NoError(t, err)
		assert.Equal(t, "{}", encodeLua(converted))
	})

	t.Run("lua to go to json keeps array", func(t *testing.T) {
		table := l.CreateTable(1, 0)
		converted, err := GoToLua(value.ToGoAny(table))
		assert.NoError(t, err)
		assert.Equal(t, "[]", encodeLua(converted))
	})

	t.Run("go empty map to lua to json keeps object", func(t *testing.T) {
		converted, err := GoToLua(map[string]any{})
		assert.NoError(t, err)
		assert.Equal(t, "{}", encodeLua(converted))
	})

	t.Run("go empty slice to lua to json keeps array", func(t *testing.T) {
		converted, err := GoToLua([]any{})
		assert.NoError(t, err)
		assert.Equal(t, "[]", encodeLua(converted))
	})

	t.Run("go map decodes back to a map", func(t *testing.T) {
		decoded, err := jsonlua.Decode([]byte("{}"))
		assert.NoError(t, err)
		_, ok := value.ToGoAny(decoded).(map[string]any)
		assert.True(t, ok)
	})

	t.Run("go list decodes back to a slice", func(t *testing.T) {
		decoded, err := jsonlua.Decode([]byte("[]"))
		assert.NoError(t, err)
		_, ok := value.ToGoAny(decoded).([]any)
		assert.True(t, ok)
	})
}
