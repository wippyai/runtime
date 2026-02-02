package sql

import (
	"testing"

	lua "github.com/wippyai/go-lua"
)

func TestLuaTableToMapSimple(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 2)
	tbl.RawSetString("name", lua.LString("Alice"))
	tbl.RawSetString("age", lua.LNumber(25))

	result := luaTableToMap(tbl)

	if len(result) != 2 {
		t.Errorf("expected 2 entries, got %d", len(result))
	}

	if result["name"] != "Alice" {
		t.Errorf("expected name 'Alice', got %v", result["name"])
	}

	if result["age"] != float64(25) {
		t.Errorf("expected age 25.0, got %v", result["age"])
	}
}

func TestLuaTableToMapWithNonStringKeys(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(2, 1)
	tbl.RawSetInt(1, lua.LString("value1"))
	tbl.RawSetString("key2", lua.LString("value2"))

	result := luaTableToMap(tbl)

	if _, exists := result["key2"]; !exists {
		t.Error("expected key2 to exist")
	}
}

func TestLuaTableToMapWithTypedValue(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 1)

	typedUD := l.NewUserData()
	typedUD.Value = &TypedValue{Type: "int", Value: int64(42)}
	tbl.RawSetString("typed", typedUD)

	result := luaTableToMap(tbl)

	if result["typed"] != int64(42) {
		t.Errorf("expected typed value 42, got %v", result["typed"])
	}
}

func TestLuaTableToMapEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 0)

	result := luaTableToMap(tbl)

	if len(result) != 0 {
		t.Errorf("expected empty map, got %d entries", len(result))
	}
}

func TestLuaTableToMapWithAllTypes(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 5)
	tbl.RawSetString("bool", lua.LTrue)
	tbl.RawSetString("number", lua.LNumber(3.14))
	tbl.RawSetString("integer", lua.LInteger(100))
	tbl.RawSetString("string", lua.LString("test"))
	tbl.RawSetString("nil", lua.LNil)

	result := luaTableToMap(tbl)

	if result["bool"] != true {
		t.Errorf("expected bool true, got %v", result["bool"])
	}

	if result["number"] != 3.14 {
		t.Errorf("expected number 3.14, got %v", result["number"])
	}

	if result["integer"] != int64(100) {
		t.Errorf("expected integer 100, got %v", result["integer"])
	}

	if result["string"] != "test" {
		t.Errorf("expected string 'test', got %v", result["string"])
	}

	if result["nil"] != nil {
		t.Errorf("expected nil, got %v", result["nil"])
	}
}
