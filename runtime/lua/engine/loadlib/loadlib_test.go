package loadlib

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestOpenRestrictedPackage(t *testing.T) {
	state := lua.NewState()
	defer state.Close()

	// Test opening the restricted package
	OpenRestrictedPackage(state)

	// Get the package table
	packageTable := state.GetField(state.Get(lua.EnvironIndex), "package")
	if packageTable == lua.LNil {
		t.Fatal("package table not found")
	}

	// Check that package table is a table
	if _, ok := packageTable.(*lua.LTable); !ok {
		t.Fatal("package table is not a table")
	}

	// Check required fields exist
	requiredFields := []string{"preload", "loaded", "path", "cpath"}
	for _, field := range requiredFields {
		value := state.GetField(packageTable, field)
		if value == lua.LNil {
			t.Errorf("required field '%s' not found in package table", field)
		}
	}

	// Check that path and cpath are empty strings
	path := state.GetField(packageTable, "path")
	if path != lua.LString("") {
		t.Errorf("expected empty path, got %v", path)
	}

	cpath := state.GetField(packageTable, "cpath")
	if cpath != lua.LString("") {
		t.Errorf("expected empty cpath, got %v", cpath)
	}

	// Check that preload table is empty initially
	preload := state.GetField(packageTable, "preload")
	if preloadTable, ok := preload.(*lua.LTable); ok {
		count := 0
		preloadTable.ForEach(func(_, _ lua.LValue) {
			count++
		})
		if count != 0 {
			t.Errorf("expected empty preload table, got %d items", count)
		}
	} else {
		t.Fatal("preload is not a table")
	}

	// Check that loaded table is empty initially
	loaded := state.GetField(packageTable, "loaded")
	if loadedTable, ok := loaded.(*lua.LTable); ok {
		count := 0
		loadedTable.ForEach(func(_, _ lua.LValue) {
			count++
		})
		if count != 0 {
			t.Errorf("expected empty loaded table, got %d items", count)
		}
	} else {
		t.Fatal("loaded is not a table")
	}
}

func TestRestrictedLoadLib(t *testing.T) {
	state := lua.NewState()
	defer state.Close()

	// Open the restricted package
	OpenRestrictedPackage(state)

	// Get the loadlib function
	packageTable := state.GetField(state.Get(lua.EnvironIndex), "package")
	loadlibFunc := state.GetField(packageTable, "loadlib")

	if loadlibFunc.Type() != lua.LTFunction {
		t.Fatal("loadlib is not a function")
	}

	// Test loadlib with a module name
	state.Push(loadlibFunc)
	state.Push(lua.LString("test_module"))
	err := state.PCall(1, 1, nil)
	if err != nil {
		t.Fatalf("loadlib call failed: %v", err)
	}

	// Check the result
	result := state.Get(-1)
	expected := "cannot load module 'test_module': loadlib disabled"
	if result.String() != expected {
		t.Errorf("expected '%s', got '%s'", expected, result.String())
	}
}

func TestPackageFunctionsMap(t *testing.T) {
	// Test that all required functions are in the map
	requiredFunctions := []string{"loadlib", "seeall"}
	for _, funcName := range requiredFunctions {
		if _, exists := packageFuncs[funcName]; !exists {
			t.Errorf("required function '%s' not found in packageFuncs", funcName)
		}
	}

	// Test that all functions in the map are functions
	for funcName, funcValue := range packageFuncs {
		if funcValue == nil {
			t.Errorf("function '%s' is nil", funcName)
		}
	}
}

func TestRequireTableModule(t *testing.T) {
	state := lua.NewState()
	defer state.Close()

	OpenRestrictedPackage(state)

	// Get preload table and add a table module
	reg := state.Get(lua.RegistryIndex).(*lua.LTable)
	preload := reg.RawGetString("_PRELOAD").(*lua.LTable)

	testModule := state.CreateTable(0, 1)
	testModule.RawSetString("value", lua.LString("test_value"))
	preload.RawSetString("testmod", testModule)

	// Test require returns the table directly
	err := state.DoString(`
		local mod = require("testmod")
		assert(mod.value == "test_value", "expected test_value")
	`)
	if err != nil {
		t.Fatalf("require table module failed: %v", err)
	}
}

func TestRequireFunctionModule(t *testing.T) {
	state := lua.NewState()
	defer state.Close()

	OpenRestrictedPackage(state)

	// Get preload table and add a function loader
	reg := state.Get(lua.RegistryIndex).(*lua.LTable)
	preload := reg.RawGetString("_PRELOAD").(*lua.LTable)

	loader := state.NewFunction(func(L *lua.LState) int {
		mod := L.CreateTable(0, 1)
		mod.RawSetString("loaded", lua.LTrue)
		L.Push(mod)
		return 1
	})
	preload.RawSetString("funcmod", loader)

	// Test require calls the function and returns result
	err := state.DoString(`
		local mod = require("funcmod")
		assert(mod.loaded == true, "expected loaded to be true")
	`)
	if err != nil {
		t.Fatalf("require function module failed: %v", err)
	}
}

func TestRequireGoFuncModule(t *testing.T) {
	state := lua.NewState()
	defer state.Close()

	OpenRestrictedPackage(state)

	// Get preload table and add a Go function loader
	reg := state.Get(lua.RegistryIndex).(*lua.LTable)
	preload := reg.RawGetString("_PRELOAD").(*lua.LTable)

	preload.RawSetString("gomod", lua.LGoFunc(func(L *lua.LState) int {
		mod := L.CreateTable(0, 1)
		mod.RawSetString("from_go", lua.LTrue)
		L.Push(mod)
		return 1
	}))

	// Test require calls the Go function and returns result
	err := state.DoString(`
		local mod = require("gomod")
		assert(mod.from_go == true, "expected from_go to be true")
	`)
	if err != nil {
		t.Fatalf("require go func module failed: %v", err)
	}
}

func TestRequireCachesModule(t *testing.T) {
	state := lua.NewState()
	defer state.Close()

	OpenRestrictedPackage(state)

	// Get preload table and add a function loader that tracks calls
	reg := state.Get(lua.RegistryIndex).(*lua.LTable)
	preload := reg.RawGetString("_PRELOAD").(*lua.LTable)

	callCount := 0
	loader := state.NewFunction(func(L *lua.LState) int {
		callCount++
		mod := L.CreateTable(0, 1)
		mod.RawSetString("count", lua.LNumber(callCount))
		L.Push(mod)
		return 1
	})
	preload.RawSetString("cachemod", loader)

	// First require
	err := state.DoString(`local mod1 = require("cachemod")`)
	if err != nil {
		t.Fatalf("first require failed: %v", err)
	}

	// Second require should return cached
	err = state.DoString(`local mod2 = require("cachemod")`)
	if err != nil {
		t.Fatalf("second require failed: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected loader to be called once, got %d", callCount)
	}
}

func TestRequireModuleNotFound(t *testing.T) {
	state := lua.NewState()
	defer state.Close()

	OpenRestrictedPackage(state)

	// Test require for non-existent module
	err := state.DoString(`require("nonexistent")`)
	if err == nil {
		t.Fatal("expected error for non-existent module")
	}
}

func TestRequireNilReturnsTrue(t *testing.T) {
	state := lua.NewState()
	defer state.Close()

	OpenRestrictedPackage(state)

	// Get preload table and add a function that returns nil
	reg := state.Get(lua.RegistryIndex).(*lua.LTable)
	preload := reg.RawGetString("_PRELOAD").(*lua.LTable)

	loader := state.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LNil)
		return 1
	})
	preload.RawSetString("nilmod", loader)

	// Require should return true (not nil) per Lua convention
	err := state.DoString(`
		local result = require("nilmod")
		assert(result == true, "expected true for nil-returning module")
	`)
	if err != nil {
		t.Fatalf("require nil module failed: %v", err)
	}
}

func TestSeeAll(t *testing.T) {
	state := lua.NewState()
	defer state.Close()

	OpenRestrictedPackage(state)

	// Test seeall function
	err := state.DoString(`
		local mod = {}
		package.seeall(mod)

		-- After seeall, mod should be able to access globals via __index
		local mt = getmetatable(mod)
		assert(mt ~= nil, "expected metatable")
		assert(mt.__index ~= nil, "expected __index")
	`)
	if err != nil {
		t.Fatalf("seeall test failed: %v", err)
	}
}

func TestSeeAllWithExistingMetatable(t *testing.T) {
	state := lua.NewState()
	defer state.Close()

	OpenRestrictedPackage(state)

	// Test seeall with existing metatable
	err := state.DoString(`
		local mod = {}
		local existingMt = { __tostring = function() return "mod" end }
		setmetatable(mod, existingMt)

		package.seeall(mod)

		local mt = getmetatable(mod)
		assert(mt.__tostring ~= nil, "expected __tostring to be preserved")
		assert(mt.__index ~= nil, "expected __index to be added")
	`)
	if err != nil {
		t.Fatalf("seeall with existing metatable failed: %v", err)
	}
}
