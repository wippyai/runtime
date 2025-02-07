package engine

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestGetField(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	t.Run("direct table access", func(t *testing.T) {
		// Create a table with a field
		tbl := L.CreateTable(0, 1)
		tbl.RawSetString("test", lua.LString("value"))

		// Test successful retrieval
		if value, ok := GetField(L, tbl, "test"); !ok || value != lua.LString("value") {
			t.Errorf("GetField(tbl, \"test\") = %v, %v, want %v, true", value, ok, lua.LString("value"))
		}

		// Test non-existent field
		if value, ok := GetField(L, tbl, "nonexistent"); ok || value != lua.LNil {
			t.Errorf("GetField(tbl, \"nonexistent\") = %v, %v, want nil, false", value, ok)
		}
	})

	t.Run("metatable with __index function", func(t *testing.T) {
		// Create test table and metatable
		tbl := L.CreateTable(0, 0)
		mt := L.CreateTable(0, 1)

		// Create __index function that returns uppercase of field name
		indexFn := L.NewFunction(func(L *lua.LState) int {
			field := L.ToString(2)
			L.Push(lua.LString(field + "_value"))
			return 1
		})
		mt.RawSetString("__index", indexFn)
		L.SetMetatable(tbl, mt)

		// Test field access through metatable
		if value, ok := GetField(L, tbl, "test"); !ok || value != lua.LString("test_value") {
			t.Errorf("GetField(tbl, \"test\") = %v, %v, want %v, true", value, ok, lua.LString("test_value"))
		}
	})

	t.Run("metatable with __index table", func(t *testing.T) {
		// Create main table and metatable
		tbl := L.CreateTable(0, 0)
		mt := L.CreateTable(0, 1)

		// Create __index table with values
		indexTbl := L.CreateTable(0, 1)
		indexTbl.RawSetString("test", lua.LString("inherited"))
		mt.RawSetString("__index", indexTbl)
		L.SetMetatable(tbl, mt)

		// Test field access through metatable
		if value, ok := GetField(L, tbl, "test"); !ok || value != lua.LString("inherited") {
			t.Errorf("GetField(tbl, \"test\") = %v, %v, want %v, true", value, ok, lua.LString("inherited"))
		}

		// Test non-existent field
		if value, ok := GetField(L, tbl, "nonexistent"); ok || value != lua.LNil {
			t.Errorf("GetField(tbl, \"nonexistent\") = %v, %v, want nil, false", value, ok)
		}
	})

	t.Run("non-table value", func(t *testing.T) {
		// Test with string value
		str := lua.LString("test")
		if value, ok := GetField(L, str, "anything"); ok || value != lua.LNil {
			t.Errorf("GetField(str, \"anything\") = %v, %v, want nil, false", value, ok)
		}

		// Test with nil value
		if value, ok := GetField(L, lua.LNil, "anything"); ok || value != lua.LNil {
			t.Errorf("GetField(nil, \"anything\") = %v, %v, want nil, false", value, ok)
		}
	})

	t.Run("metatable error handling", func(t *testing.T) {
		// Create table with metatable that has __index function that errors
		tbl := L.CreateTable(0, 0)
		mt := L.CreateTable(0, 1)

		indexFn := L.NewFunction(func(L *lua.LState) int {
			L.RaiseError("test error")
			return 0
		})
		mt.RawSetString("__index", indexFn)
		L.SetMetatable(tbl, mt)

		// Test that error in __index function is handled correctly
		if value, ok := GetField(L, tbl, "test"); ok || value != lua.LNil {
			t.Errorf("GetField(tbl, \"test\") = %v, %v, want nil, false", value, ok)
		}
	})
}
