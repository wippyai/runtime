package sql

import (
	"errors"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestToGoValueEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input lua.LValue
		want  interface{}
	}{
		{"empty string", lua.LString(""), ""},
		{"zero number", lua.LNumber(0), 0.0},
		{"zero integer", lua.LInteger(0), int64(0)},
		{"negative number", lua.LNumber(-3.14), -3.14},
		{"negative integer", lua.LInteger(-42), int64(-42)},
		{"large integer", lua.LInteger(999999), int64(999999)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toGoValue(tt.input)
			if got != tt.want {
				t.Errorf("toGoValue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckParamsWithSingleParam(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(1, 0)
	tbl.RawSetInt(1, lua.LString("single"))

	l.Push(tbl)

	params, err := checkParams(l, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(params))
	}

	if params[0] != "single" {
		t.Errorf("expected 'single', got %v", params[0])
	}
}

func TestLuaTableToMapWithBooleans(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 2)
	tbl.RawSetString("active", lua.LTrue)
	tbl.RawSetString("deleted", lua.LFalse)

	result := luaTableToMap(tbl)

	if result["active"] != true {
		t.Errorf("expected active to be true, got %v", result["active"])
	}

	if result["deleted"] != false {
		t.Errorf("expected deleted to be false, got %v", result["deleted"])
	}
}

func TestLuaTableToMapWithNumbers(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 2)
	tbl.RawSetString("count", lua.LNumber(5))
	tbl.RawSetString("total", lua.LInteger(100))

	result := luaTableToMap(tbl)

	if result["count"] != float64(5) {
		t.Errorf("expected count to be 5.0, got %v", result["count"])
	}

	if result["total"] != int64(100) {
		t.Errorf("expected total to be 100, got %v", result["total"])
	}
}

func TestGoValueToLuaInteger(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	result := goValueToLua(l, 42)
	if num, ok := result.(lua.LInteger); !ok {
		t.Errorf("expected LInteger, got %T", result)
	} else if num != 42 {
		t.Errorf("expected 42, got %d", num)
	}
}

func TestGoValueToLuaInt64(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	result := goValueToLua(l, int64(999))
	if num, ok := result.(lua.LInteger); !ok {
		t.Errorf("expected LInteger, got %T", result)
	} else if num != 999 {
		t.Errorf("expected 999, got %d", num)
	}
}

func TestGoValueToLuaFloat64(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	result := goValueToLua(l, 3.14)
	if num, ok := result.(lua.LNumber); !ok {
		t.Errorf("expected LNumber, got %T", result)
	} else if num != 3.14 {
		t.Errorf("expected 3.14, got %f", num)
	}
}

func TestGoValueToLuaString(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	result := goValueToLua(l, "test")
	if str, ok := result.(lua.LString); !ok {
		t.Errorf("expected LString, got %T", result)
	} else if str != "test" {
		t.Errorf("expected 'test', got %s", str)
	}
}

func TestGoValueToLuaBytes(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	result := goValueToLua(l, []byte("data"))
	if str, ok := result.(lua.LString); !ok {
		t.Errorf("expected LString, got %T", result)
	} else if str != "data" {
		t.Errorf("expected 'data', got %s", str)
	}
}

func TestGoValueToLuaNil(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	result := goValueToLua(l, nil)
	if result != lua.LNil {
		t.Errorf("expected LNil, got %v", result)
	}
}

func TestGoValueToLuaTrue(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	result := goValueToLua(l, true)
	if result != lua.LTrue {
		t.Errorf("expected LTrue, got %v", result)
	}
}

func TestGoValueToLuaFalse(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	result := goValueToLua(l, false)
	if result != lua.LFalse {
		t.Errorf("expected LFalse, got %v", result)
	}
}

func TestGoValueToLuaUnknownType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	result := goValueToLua(l, struct{}{})
	if _, ok := result.(lua.LString); !ok {
		t.Errorf("expected LString for unknown type, got %T", result)
	}
}

func TestGoArgsToLuaTableSingle(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	args := []any{"single"}
	result := goArgsToLuaTable(l, args)

	if result.Len() != 1 {
		t.Errorf("expected table length 1, got %d", result.Len())
	}

	if result.RawGetInt(1) != lua.LString("single") {
		t.Errorf("expected 'single', got %v", result.RawGetInt(1))
	}
}

func TestErrorDetailsNil(t *testing.T) {
	err := &Error{
		message: "test",
	}

	if err.Details() != nil {
		t.Error("expected nil details")
	}
}

func TestErrorUnwrapNil(t *testing.T) {
	err := &Error{
		message: "test",
	}

	if errors.Unwrap(err) != nil {
		t.Error("expected nil unwrap")
	}
}
