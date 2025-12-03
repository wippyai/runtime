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
