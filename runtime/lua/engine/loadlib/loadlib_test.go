package loadlib

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestOpenRestrictedPackage(t *testing.T) {
	state := lua.NewState()
	defer state.Close()

	OpenRestrictedPackage(state)

	// Get the package table
	packageTable := state.GetField(state.Get(lua.EnvironIndex), "package")
	if packageTable == lua.LNil {
		t.Fatal("package table not found")
	}

	if _, ok := packageTable.(*lua.LTable); !ok {
		t.Fatal("package table is not a table")
	}

	// Check path and cpath are empty strings
	path := state.GetField(packageTable, "path")
	if path != lua.LString("") {
		t.Errorf("expected empty path, got %v", path)
	}

	cpath := state.GetField(packageTable, "cpath")
	if cpath != lua.LString("") {
		t.Errorf("expected empty cpath, got %v", cpath)
	}
}

func TestRestrictedLoadLib(t *testing.T) {
	state := lua.NewState()
	defer state.Close()

	OpenRestrictedPackage(state)

	packageTable := state.GetField(state.Get(lua.EnvironIndex), "package")
	loadlibFunc := state.GetField(packageTable, "loadlib")

	if loadlibFunc.Type() != lua.LTFunction {
		t.Fatal("loadlib is not a function")
	}

	state.Push(loadlibFunc)
	state.Push(lua.LString("test_module"))
	err := state.PCall(1, 1, nil)
	if err != nil {
		t.Fatalf("loadlib call failed: %v", err)
	}

	result := state.Get(-1)
	expected := "cannot load module 'test_module': loadlib disabled"
	if result.String() != expected {
		t.Errorf("expected '%s', got '%s'", expected, result.String())
	}
}

func TestRequireFromGlobal(t *testing.T) {
	state := lua.NewState()
	defer state.Close()

	OpenRestrictedPackage(state)

	// Set module in _G
	testModule := state.CreateTable(0, 1)
	testModule.RawSetString("value", lua.LString("test_value"))
	state.SetGlobal("testmod", testModule)

	// require should return the global
	err := state.DoString(`
		local mod = require("testmod")
		assert(mod.value == "test_value", "expected test_value")
	`)
	if err != nil {
		t.Fatalf("require from global failed: %v", err)
	}
}

func TestRequireModuleNotFound(t *testing.T) {
	state := lua.NewState()
	defer state.Close()

	OpenRestrictedPackage(state)

	err := state.DoString(`require("nonexistent")`)
	if err == nil {
		t.Fatal("expected error for non-existent module")
	}
}

func TestRequireReturnsSameInstance(t *testing.T) {
	state := lua.NewState()
	defer state.Close()

	OpenRestrictedPackage(state)

	// Set module in _G
	testModule := state.CreateTable(0, 1)
	testModule.RawSetString("count", lua.LNumber(0))
	state.SetGlobal("countermod", testModule)

	// Multiple requires should return same instance
	err := state.DoString(`
		local mod1 = require("countermod")
		mod1.count = mod1.count + 1

		local mod2 = require("countermod")
		assert(mod2.count == 1, "expected same instance with count 1")
	`)
	if err != nil {
		t.Fatalf("require same instance failed: %v", err)
	}
}

func TestSeeAll(t *testing.T) {
	state := lua.NewState()
	defer state.Close()

	OpenRestrictedPackage(state)

	err := state.DoString(`
		local mod = {}
		package.seeall(mod)

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

func TestPackageModuleIsImmutable(t *testing.T) {
	state := lua.NewState()
	defer state.Close()

	OpenRestrictedPackage(state)

	// The package table should be immutable after warmup
	pkg := state.GetGlobal("package")
	if tbl, ok := pkg.(*lua.LTable); ok {
		if !tbl.Immutable {
			t.Error("expected package table to be immutable")
		}
	}
}
