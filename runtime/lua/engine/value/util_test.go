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

func TestRegisterTypeMethods(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	t.Run("register metamethods only", func(t *testing.T) {
		typeName := "test_meta_" + t.Name()
		metamethods := map[string]lua.LGoFunc{
			"__tostring": func(L *lua.LState) int {
				L.Push(lua.LString("test"))
				return 1
			},
		}

		mt := RegisterMetamethods(L, typeName, metamethods)
		assert.NotNil(t, mt)
		assert.True(t, IsTypeRegistered(typeName))

		// Verify metatable is immutable
		assert.True(t, mt.Immutable)

		// Verify can retrieve
		retrieved := GetTypeMetatable(L, typeName)
		assert.Equal(t, mt, retrieved)
	})

	t.Run("register methods only", func(t *testing.T) {
		typeName := "test_methods_" + t.Name()
		methods := map[string]lua.LGoFunc{
			"test": func(L *lua.LState) int {
				L.Push(lua.LString("test"))
				return 1
			},
		}

		mt := RegisterMethods(L, typeName, methods)
		assert.NotNil(t, mt)
		assert.True(t, IsTypeRegistered(typeName))

		// Verify __index table exists
		indexVal := mt.RawGetString("__index")
		assert.NotNil(t, indexVal)
		indexTbl, ok := indexVal.(*lua.LTable)
		assert.True(t, ok)
		assert.True(t, indexTbl.Immutable)
	})

	t.Run("register both metamethods and methods", func(t *testing.T) {
		typeName := "test_both_" + t.Name()
		metamethods := map[string]lua.LGoFunc{
			"__len": func(L *lua.LState) int {
				L.Push(lua.LNumber(42))
				return 1
			},
		}
		methods := map[string]lua.LGoFunc{
			"getValue": func(L *lua.LState) int {
				L.Push(lua.LNumber(100))
				return 1
			},
		}

		mt := RegisterTypeMethods(L, typeName, metamethods, methods)
		assert.NotNil(t, mt)
		assert.True(t, IsTypeRegistered(typeName))

		// Verify __len exists
		lenFn := mt.RawGetString("__len")
		assert.NotNil(t, lenFn)

		// Verify __index table exists with getValue
		indexVal := mt.RawGetString("__index")
		indexTbl, ok := indexVal.(*lua.LTable)
		assert.True(t, ok)
		getValueFn := indexTbl.RawGetString("getValue")
		assert.NotNil(t, getValueFn)
	})

	t.Run("unregistered type returns nil", func(t *testing.T) {
		mt := GetTypeMetatable(L, "nonexistent_type_xyz")
		assert.Nil(t, mt)
		assert.False(t, IsTypeRegistered("nonexistent_type_xyz"))
	})
}

func TestPushUserData(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	t.Run("push userdata with metatable", func(t *testing.T) {
		mt := L.CreateTable(0, 1)
		mt.RawSetString("__tostring", L.NewFunction(func(L *lua.LState) int {
			L.Push(lua.LString("custom"))
			return 1
		}))

		type testData struct{ value int }
		data := &testData{value: 42}

		ud := PushUserData(L, data, mt)
		assert.NotNil(t, ud)
		assert.Equal(t, data, ud.Value)
		assert.Equal(t, mt, ud.Metatable)

		// Verify it's on the stack
		top := L.Get(-1)
		assert.Equal(t, ud, top)
		L.Pop(1)
	})
}

func TestPushTypedUserData(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	t.Run("push typed userdata", func(t *testing.T) {
		typeName := "typed_test_" + t.Name()
		methods := map[string]lua.LGoFunc{
			"getValue": func(L *lua.LState) int {
				ud := L.CheckUserData(1)
				if val, ok := ud.Value.(int); ok {
					L.Push(lua.LNumber(val))
					return 1
				}
				return 0
			},
		}
		RegisterMethods(L, typeName, methods)

		ud := PushTypedUserData(L, 123, typeName)
		assert.NotNil(t, ud)
		assert.Equal(t, 123, ud.Value)
		L.Pop(1)
	})

	t.Run("push typed userdata with unregistered type", func(t *testing.T) {
		ud := PushTypedUserData(L, "test", "unregistered_type_abc")
		assert.Nil(t, ud)
	})
}

func TestToGoAny(t *testing.T) {
	t.Run("nil value", func(t *testing.T) {
		result := ToGoAny(lua.LNil)
		assert.Nil(t, result)
	})

	t.Run("nil interface", func(t *testing.T) {
		result := ToGoAny(nil)
		assert.Nil(t, result)
	})

	t.Run("bool true", func(t *testing.T) {
		result := ToGoAny(lua.LTrue)
		assert.Equal(t, true, result)
	})

	t.Run("bool false", func(t *testing.T) {
		result := ToGoAny(lua.LFalse)
		assert.Equal(t, false, result)
	})

	t.Run("number", func(t *testing.T) {
		result := ToGoAny(lua.LNumber(3.14))
		assert.Equal(t, 3.14, result)
	})

	t.Run("integer", func(t *testing.T) {
		result := ToGoAny(lua.LInteger(42))
		assert.Equal(t, int64(42), result)
	})

	t.Run("string", func(t *testing.T) {
		result := ToGoAny(lua.LString("hello"))
		assert.Equal(t, "hello", result)
	})

	t.Run("array table", func(t *testing.T) {
		L := lua.NewState()
		defer L.Close()

		tbl := L.CreateTable(3, 0)
		tbl.RawSetInt(1, lua.LNumber(1))
		tbl.RawSetInt(2, lua.LNumber(2))
		tbl.RawSetInt(3, lua.LNumber(3))

		result := ToGoAny(tbl)
		arr, ok := result.([]any)
		assert.True(t, ok)
		assert.Len(t, arr, 3)
		assert.Equal(t, float64(1), arr[0])
		assert.Equal(t, float64(2), arr[1])
		assert.Equal(t, float64(3), arr[2])
	})

	t.Run("map table", func(t *testing.T) {
		L := lua.NewState()
		defer L.Close()

		tbl := L.CreateTable(0, 2)
		tbl.RawSetString("key1", lua.LString("value1"))
		tbl.RawSetString("key2", lua.LNumber(42))

		result := ToGoAny(tbl)
		m, ok := result.(map[string]any)
		assert.True(t, ok)
		assert.Equal(t, "value1", m["key1"])
		assert.Equal(t, float64(42), m["key2"])
	})

	t.Run("function returns string representation", func(t *testing.T) {
		L := lua.NewState()
		defer L.Close()

		fn := L.NewFunction(func(*lua.LState) int { return 0 })
		result := ToGoAny(fn)
		_, ok := result.(string)
		assert.True(t, ok)
	})

	t.Run("userdata returns string representation", func(t *testing.T) {
		L := lua.NewState()
		defer L.Close()

		ud := L.NewUserData()
		result := ToGoAny(ud)
		_, ok := result.(string)
		assert.True(t, ok)
	})
}

func TestTableToMap(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	tbl := L.CreateTable(0, 3)
	tbl.RawSetString("str", lua.LString("hello"))
	tbl.RawSetString("num", lua.LNumber(42))
	tbl.RawSetString("bool", lua.LTrue)

	result := TableToMap(tbl)
	assert.Len(t, result, 3)
	assert.Equal(t, "hello", result["str"])
	assert.Equal(t, float64(42), result["num"])
	assert.Equal(t, true, result["bool"])
}

func TestTableToSlice(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	tbl := L.CreateTable(4, 0)
	tbl.RawSetInt(1, lua.LString("first"))
	tbl.RawSetInt(2, lua.LNumber(2))
	tbl.RawSetInt(3, lua.LTrue)
	tbl.RawSetInt(4, lua.LNil)

	result := TableToSlice(tbl, 4)
	assert.Len(t, result, 4)
	assert.Equal(t, "first", result[0])
	assert.Equal(t, float64(2), result[1])
	assert.Equal(t, true, result[2])
	assert.Nil(t, result[3])
}
