package engine

import (
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
	"testing"
)

func TestGetField(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	t.Run("direct table access", func(t *testing.T) {
		tbl := L.CreateTable(0, 1)
		tbl.RawSetString("test", lua.LString("value"))

		if value, ok := GetField(L, tbl, "test"); !ok || value != lua.LString("value") {
			t.Errorf("GetField(tbl, \"test\") = %v, %v, want %v, true", value, ok, lua.LString("value"))
		}
	})

	t.Run("metatable with __index function", func(t *testing.T) {
		assert.NoError(t, L.DoString(`
            local tbl = {}
            local mt = {
                __index = function(t, k)
                    return k .. "_value"
                end
            }
            setmetatable(tbl, mt)
            return tbl
        `))
		tbl := L.Get(-1)
		L.Pop(1)

		if value, ok := GetField(L, tbl, "test"); !ok || value != lua.LString("test_value") {
			t.Errorf("GetField(tbl, \"test\") = %v, %v, want %v, true", value, ok, lua.LString("test_value"))
		}
	})

	t.Run("metatable with __index table", func(t *testing.T) {
		assert.NoError(t, L.DoString(`
            local tbl = {}
            local mt = {
                __index = {
                    test = "inherited"
                }
            }
            setmetatable(tbl, mt)
            return tbl
        `))
		tbl := L.Get(-1)
		L.Pop(1)

		if value, ok := GetField(L, tbl, "test"); !ok || value != lua.LString("inherited") {
			t.Errorf("GetField(tbl, \"test\") = %v, %v, want %v, true", value, ok, lua.LString("inherited"))
		}
	})

	// Edge Cases
	t.Run("recursive metatable", func(t *testing.T) {
		assert.NoError(t, L.DoString(`
            local tbl = {}
            local mt = {__index = tbl}  -- recursive reference
            setmetatable(tbl, mt)
            tbl.test = "recursive"
            return tbl
        `))
		tbl := L.Get(-1)
		L.Pop(1)

		if value, ok := GetField(L, tbl, "test"); !ok || value != lua.LString("recursive") {
			t.Errorf("GetField(tbl, \"test\") = %v, %v, want %v, true", value, ok, lua.LString("recursive"))
		}
	})

	t.Run("chained metatables", func(t *testing.T) {
		assert.NoError(t, L.DoString(`
            local tbl1 = {}
            local tbl2 = {test = "chain2"}
            local tbl3 = {test = "chain3"}
            
            local mt1 = {__index = tbl2}
            local mt2 = {__index = tbl3}
            
            setmetatable(tbl1, mt1)
            setmetatable(tbl2, mt2)
            return tbl1
        `))
		tbl := L.Get(-1)
		L.Pop(1)

		if value, ok := GetField(L, tbl, "test"); !ok || value != lua.LString("chain2") {
			t.Errorf("GetField(tbl, \"test\") = %v, %v, want %v, true", value, ok, lua.LString("chain2"))
		}
	})

	t.Run("__index function returns nil", func(t *testing.T) {
		assert.NoError(t, L.DoString(`
            local tbl = {}
            local mt = {
                __index = function() return nil end
            }
            setmetatable(tbl, mt)
            return tbl
        `))
		tbl := L.Get(-1)
		L.Pop(1)

		if value, ok := GetField(L, tbl, "test"); ok || value != lua.LNil {
			t.Errorf("GetField(tbl, \"test\") = %v, %v, want nil, false", value, ok)
		}
	})

	t.Run("__index function errors", func(t *testing.T) {
		assert.NoError(t, L.DoString(`
            local tbl = {}
            local mt = {
                __index = function() error("intentional error") end
            }
            setmetatable(tbl, mt)
            return tbl
        `))
		tbl := L.Get(-1)
		L.Pop(1)

		if value, ok := GetField(L, tbl, "test"); ok || value != lua.LNil {
			t.Errorf("GetField(tbl, \"test\") = %v, %v, want nil, false", value, ok)
		}
	})

	t.Run("non-function non-table __index", func(t *testing.T) {
		assert.NoError(t, L.DoString(`
            local tbl = {}
            local mt = {
                __index = "not a function or table"
            }
            setmetatable(tbl, mt)
            return tbl
        `))
		tbl := L.Get(-1)
		L.Pop(1)

		if value, ok := GetField(L, tbl, "test"); ok || value != lua.LNil {
			t.Errorf("GetField(tbl, \"test\") = %v, %v, want nil, false", value, ok)
		}
	})

	t.Run("nil metatable", func(t *testing.T) {
		tbl := L.CreateTable(0, 0)
		// Explicitly ensure no metatable is set
		L.SetMetatable(tbl, lua.LNil)

		if value, ok := GetField(L, tbl, "test"); ok || value != lua.LNil {
			t.Errorf("GetField(tbl, \"test\") = %v, %v, want nil, false", value, ok)
		}
	})

	t.Run("metatable without __index", func(t *testing.T) {
		assert.NoError(t, L.DoString(`
            local tbl = {}
            local mt = {
                __newindex = function() end  -- some other metamethod
            }
            setmetatable(tbl, mt)
            return tbl
        `))
		tbl := L.Get(-1)
		L.Pop(1)

		if value, ok := GetField(L, tbl, "test"); ok || value != lua.LNil {
			t.Errorf("GetField(tbl, \"test\") = %v, %v, want nil, false", value, ok)
		}
	})

	t.Run("primitive values", func(t *testing.T) {
		values := []lua.LValue{
			lua.LNumber(42),
			lua.LString("string"),
			lua.LBool(true),
			lua.LNil,
		}

		for _, v := range values {
			if value, ok := GetField(L, v, "anything"); ok || value != lua.LNil {
				t.Errorf("GetField(%v, \"anything\") = %v, %v, want nil, false", v, value, ok)
			}
		}
	})
}

func TestGetField_Userdata(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	t.Run("userdata with __index table", func(t *testing.T) {
		// Create test userdata with metatable
		assert.NoError(t, L.DoString(`
            local ud = newproxy(true)  -- creates userdata with empty metatable
            local mt = getmetatable(ud)
            mt.__index = {
                test = "userdata_value"
            }
            return ud
        `))
		ud := L.Get(-1)
		L.Pop(1)

		if value, ok := GetField(L, ud, "test"); !ok || value != lua.LString("userdata_value") {
			t.Errorf("GetField(userdata, \"test\") = %v, %v, want userdata_value, true", value, ok)
		}
	})

	t.Run("userdata with __index function", func(t *testing.T) {
		assert.NoError(t, L.DoString(`
            local ud = newproxy(true)
            local mt = getmetatable(ud)
            mt.__index = function(t, k)
                return "userdata_" .. k
            end
            return ud
        `))
		ud := L.Get(-1)
		L.Pop(1)

		if value, ok := GetField(L, ud, "test"); !ok || value != lua.LString("userdata_test") {
			t.Errorf("GetField(userdata, \"test\") = %v, %v, want userdata_test, true", value, ok)
		}
	})
}
