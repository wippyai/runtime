package lua

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ponyruntime/go-lua"
)

func TestToGoAny(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tests := []struct {
		name  string
		input lua.LValue
		want  any
	}{
		{"Nil", lua.LNil, nil},
		{"Bool", lua.LBool(true), true},
		{"Number", lua.LNumber(42.5), 42.5},
		{"String", lua.LString("hello"), "hello"},
		{
			"Table (map)",
			func() lua.LValue {
				tbl := l.NewTable()
				l.SetTable(tbl, lua.LString("key1"), lua.LString("value1"))
				l.SetTable(tbl, lua.LString("key2"), lua.LNumber(2))
				return tbl
			}(),
			map[string]any{"key1": "value1", "key2": 2.0},
		},
		{
			"Table (array)",
			func() lua.LValue {
				tbl := l.NewTable()
				l.SetTable(tbl, lua.LNumber(1), lua.LString("one"))
				l.SetTable(tbl, lua.LNumber(2), lua.LNumber(2))
				l.SetTable(tbl, lua.LNumber(3), lua.LBool(true))
				return tbl
			}(),
			[]any{"one", 2.0, true},
		},
		{
			"Other",
			l.NewFunction(func(l *lua.LState) int { return 0 }),
			"function", // We'll just check if the result is a string (and optionally contains "function")
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToGoAny(tt.input)

			if tt.name == "Other" {
				// Special handling for the "Other" case
				gotString, ok := got.(string)
				if !ok {
					t.Errorf("ToGoAny() for function: expected a string, got %T", got)
				}
				if !strings.Contains(gotString, "function") {
					t.Errorf("ToGoAny() for function: expected string to contain 'function', got '%s'", gotString)
				}
			} else if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ToGoAny() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGoToLua(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tests := []struct {
		name  string
		input any
		want  lua.LValue
	}{
		{"String", "hello", lua.LString("hello")},
		{"Float64", 42.5, lua.LNumber(42.5)},
		{"Int", 10, lua.LNumber(10)},
		{"Bool", true, lua.LBool(true)},
		{"Nil", nil, lua.LNil},
		{"Int Array", []int{1, 2, 3}, func() lua.LValue {
			tbl := l.NewTable()
			l.SetTable(tbl, lua.LNumber(1), lua.LNumber(1))
			l.SetTable(tbl, lua.LNumber(2), lua.LNumber(2))
			l.SetTable(tbl, lua.LNumber(3), lua.LNumber(3))
			return tbl
		}()},
		{"String Array", []string{"a", "b"}, func() lua.LValue {
			tbl := l.NewTable()
			l.SetTable(tbl, lua.LNumber(1), lua.LString("a"))
			l.SetTable(tbl, lua.LNumber(2), lua.LString("b"))
			return tbl
		}()},
		{
			"Map[string]any",
			map[string]any{"name": "John", "age": 30},
			func() lua.LValue {
				tbl := l.NewTable()
				l.SetTable(tbl, lua.LString("name"), lua.LString("John"))
				l.SetTable(tbl, lua.LString("age"), lua.LNumber(30))
				return tbl
			}(),
		},
		{
			"Map[string]string",
			map[string]string{"name": "John", "city": "New York"},
			func() lua.LValue {
				tbl := l.NewTable()
				l.SetTable(tbl, lua.LString("name"), lua.LString("John"))
				l.SetTable(tbl, lua.LString("city"), lua.LString("New York"))
				return tbl
			}(),
		},
		{
			"Any Array",
			[]any{"hello", 42, true},
			func() lua.LValue {
				tbl := l.NewTable()
				l.SetTable(tbl, lua.LNumber(1), lua.LString("hello"))
				l.SetTable(tbl, lua.LNumber(2), lua.LNumber(42))
				l.SetTable(tbl, lua.LNumber(3), lua.LBool(true))
				return tbl
			}(),
		},
		{"Unsupported", make(chan int), lua.LNil}, // Test with an unsupported type
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GoToLua(l, tt.input)

			// Deep comparison for tables, otherwise use reflect.DeepEqual
			if tt.want.Type() == lua.LTTable {
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
					if !reflect.DeepEqual(ToGoAny(gotValue), ToGoAny(v)) {
						t.Errorf("GoToLua() table value for key %v = %v, want %v", k, ToGoAny(gotValue), ToGoAny(v))
					}
				})

			} else if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GoToLua() = %v, want %v", got, tt.want)
			}
		})
	}
}
