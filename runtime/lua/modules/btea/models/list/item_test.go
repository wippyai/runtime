package list

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestLuaItem(t *testing.T) {
	logger := zap.NewNop()
	vm, err := engine.NewVM(logger)
	require.NoError(t, err)
	defer vm.Close()

	// Register test metatable and helper
	mt := vm.State().NewTypeMetatable("btea.Item")
	methods := map[string]lua.LGFunction{
		"title": func(L *lua.LState) int {
			item := L.CheckUserData(1).Value.(*LuaItem)
			L.Push(lua.LString(item.Title()))
			return 1
		},
		"description": func(L *lua.LState) int {
			item := L.CheckUserData(1).Value.(*LuaItem)
			L.Push(lua.LString(item.Description()))
			return 1
		},
		"filter_value": func(L *lua.LState) int {
			item := L.CheckUserData(1).Value.(*LuaItem)
			L.Push(lua.LString(item.FilterValue()))
			return 1
		},
		"get_original_value": func(L *lua.LState) int {
			item := L.CheckUserData(1).Value.(*LuaItem)
			L.Push(item.GetOriginalValue())
			return 1
		},
	}
	vm.State().SetField(mt, "__index", vm.State().SetFuncs(vm.State().NewTable(), methods))

	// Set test helper
	vm.State().SetGlobal("TestCreateLuaItem", vm.State().NewFunction(func(L *lua.LState) int {
		value := L.CheckAny(1)
		item := &LuaItem{
			value:    value,
			luaState: L,
		}
		ud := L.NewUserData()
		ud.Value = item
		L.SetMetatable(ud, L.GetTypeMetatable("btea.Item"))
		L.Push(ud)
		return 1
	}))

	// Test item string methods
	t.Run("item string methods", func(t *testing.T) {
		err := vm.DoString(context.Background(), `
            local item = {
                filter_value = "test-filter",
                title = "Test Title",
                description = "Test Description"
            }
            
            local luaItem = TestCreateLuaItem(item)
            
            assert(luaItem:filter_value() == "test-filter", "filter value should match")
            assert(luaItem:title() == "Test Title", "title should match")
            assert(luaItem:description() == "Test Description", "description should match")
        `, "test_item_strings")
		require.NoError(t, err)
	})

	// Test function-based values
	t.Run("item function values", func(t *testing.T) {
		err := vm.DoString(context.Background(), `
            local item = {
                filter_value = function(self) return "dynamic-filter" end,
                title = function(self) return "Dynamic Title" end,
                description = function(self) return "Dynamic Description" end
            }
            
            local luaItem = TestCreateLuaItem(item)
            
            assert(luaItem:filter_value() == "dynamic-filter", "filter value function should work")
            assert(luaItem:title() == "Dynamic Title", "title function should work")
            assert(luaItem:description() == "Dynamic Description", "description function should work")
        `, "test_item_functions")
		require.NoError(t, err)
	})

	// Test nil values
	t.Run("item nil values", func(t *testing.T) {
		err := vm.DoString(context.Background(), `
            local item = {}            
            local luaItem = TestCreateLuaItem(item)
            
            assert(luaItem:filter_value() == "", "empty filter value should return empty string")
            assert(luaItem:title() == "", "empty title should return empty string")
            assert(luaItem:description() == "", "empty description should return empty string")
        `, "test_item_nil")
		require.NoError(t, err)
	})

	// Test value updates
	t.Run("item value updates", func(t *testing.T) {
		err := vm.DoString(context.Background(), `
            local item = {
                filter_value = "initial-filter",
                title = "Initial Title",
                description = "Initial Description"
            }
            
            local luaItem = TestCreateLuaItem(item)
            
            item.filter_value = "updated-filter"
            item.title = "Updated Title"
            item.description = "Updated Description"
            
            assert(luaItem:filter_value() == "updated-filter", "filter value should update")
            assert(luaItem:title() == "Updated Title", "title should update")
            assert(luaItem:description() == "Updated Description", "description should update")
        `, "test_item_updates")
		require.NoError(t, err)
	})

	// Test accessing original value
	t.Run("item original value access", func(t *testing.T) {
		err := vm.DoString(context.Background(), `
            local item = {
                filter_value = "test-filter",
                title = "Test Title",
                description = "Test Description",
                custom_field = "Custom Value"
            }
            
            local luaItem = TestCreateLuaItem(item)
            local original = luaItem:get_original_value()
            assert(original.custom_field == "Custom Value", "should retain custom fields")
        `, "test_item_original")
		require.NoError(t, err)
	})
}
