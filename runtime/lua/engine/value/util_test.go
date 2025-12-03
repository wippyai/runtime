package value

import (
	"testing"

	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
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
		// Create userdata via Go since newproxy is not available in gopher-lua
		ud := L.NewUserData()
		mt := L.NewTable()
		index := L.NewTable()
		index.RawSetString("test", lua.LString("userdata_value"))
		mt.RawSetString("__index", index)
		L.SetMetatable(ud, mt)

		if value, ok := GetField(L, ud, "test"); !ok || value != lua.LString("userdata_value") {
			t.Errorf("GetField(userdata, \"test\") = %v, %v, want userdata_value, true", value, ok)
		}
	})

	t.Run("userdata with __index function", func(t *testing.T) {
		// Create userdata with __index function
		ud := L.NewUserData()
		mt := L.NewTable()
		indexFn := L.NewFunction(func(ls *lua.LState) int {
			key := ls.CheckString(2)
			ls.Push(lua.LString("userdata_" + key))
			return 1
		})
		mt.RawSetString("__index", indexFn)
		L.SetMetatable(ud, mt)

		if value, ok := GetField(L, ud, "test"); !ok || value != lua.LString("userdata_test") {
			t.Errorf("GetField(userdata, \"test\") = %v, %v, want userdata_test, true", value, ok)
		}
	})
}

func TestGetFunc(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	t.Run("direct table function", func(t *testing.T) {
		assert.NoError(t, L.DoString(`
            local tbl = {
                fn = function() return "direct" end
            }
            return tbl
        `))
		tbl := L.Get(-1)
		L.Pop(1)

		fn, ok := GetFunc(L, tbl, "fn")
		if !ok {
			t.Fatal("expected to find function")
		}

		L.Push(fn)
		if err := L.PCall(0, 1, nil); err != nil {
			t.Fatal(err)
		}
		if result := L.ToString(-1); result != "direct" {
			t.Errorf("got %v, want 'direct'", result)
		}
		L.Pop(1)
	})

	t.Run("metatable __index function returning function", func(t *testing.T) {
		assert.NoError(t, L.DoString(`
            local tbl = {}
            local mt = {
                __index = function(t, k)
                    if k == "fn" then
                        return function() return "meta" end
                    end
                end
            }
            setmetatable(tbl, mt)
            return tbl
        `))
		tbl := L.Get(-1)
		L.Pop(1)

		fn, ok := GetFunc(L, tbl, "fn")
		if !ok {
			t.Fatal("expected to find function")
		}

		L.Push(fn)
		if err := L.PCall(0, 1, nil); err != nil {
			t.Fatal(err)
		}
		if result := L.ToString(-1); result != "meta" {
			t.Errorf("got %v, want 'meta'", result)
		}
		L.Pop(1)
	})

	t.Run("metatable __index table with function", func(t *testing.T) {
		assert.NoError(t, L.DoString(`
            local tbl = {}
            local mt = {
                __index = {
                    fn = function() return "index_table" end
                }
            }
            setmetatable(tbl, mt)
            return tbl
        `))
		tbl := L.Get(-1)
		L.Pop(1)

		fn, ok := GetFunc(L, tbl, "fn")
		if !ok {
			t.Fatal("expected to find function")
		}

		L.Push(fn)
		if err := L.PCall(0, 1, nil); err != nil {
			t.Fatal(err)
		}
		if result := L.ToString(-1); result != "index_table" {
			t.Errorf("got %v, want 'index_table'", result)
		}
		L.Pop(1)
	})

	t.Run("non-function field", func(t *testing.T) {
		assert.NoError(t, L.DoString(`
            local tbl = {
                notfn = "string"
            }
            return tbl
        `))
		tbl := L.Get(-1)
		L.Pop(1)

		if fn, ok := GetFunc(L, tbl, "notfn"); ok || fn != nil {
			t.Error("expected not to find function")
		}
	})

	t.Run("userdata with function", func(t *testing.T) {
		// Spawn userdata
		ud := L.NewUserData()

		// Spawn metatable with __index function
		mt := L.CreateTable(0, 1)
		indexTbl := L.CreateTable(0, 1)
		fn := L.NewFunction(func(L *lua.LState) int {
			L.Push(lua.LString("userdata"))
			return 1
		})
		indexTbl.RawSetString("fn", fn)
		mt.RawSetString("__index", indexTbl)

		L.SetMetatable(ud, mt)

		gotFn, ok := GetFunc(L, ud, "fn")
		if !ok || gotFn == nil {
			t.Fatal("expected to find function")
		}

		L.Push(gotFn)
		if err := L.PCall(0, 1, nil); err != nil {
			t.Fatal(err)
		}
		if result := L.ToString(-1); result != "userdata" {
			t.Errorf("got %v, want 'userdata'", result)
		}
		L.Pop(1)
	})

	t.Run("non-existent field", func(t *testing.T) {
		tbl := L.CreateTable(0, 0)
		if fn, ok := GetFunc(L, tbl, "nonexistent"); ok || fn != nil {
			t.Error("expected not to find function")
		}
	})
}
