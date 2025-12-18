package code

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	base64mod "github.com/wippyai/runtime/runtime/lua/modules/base64"
	"github.com/yuin/gopher-lua/types"
	"github.com/yuin/gopher-lua/types/checker"
)

// testModuleTypes creates a simple module type manifest for testing.
// Simulates a module like:
//
//	local mylib = {}
//	function mylib.add(a: number, b: number): number
//	function mylib.concat(s1: string, s2: string): string
//	return mylib
func testModuleTypes() *types.TypeManifest {
	m := types.NewManifest("mylib")

	moduleType := &types.InterfaceType{
		Name: "mylib",
		Methods: map[string]*types.FunctionType{
			"add": types.NewFunction(
				[]types.Type{types.Number, types.Number},
				[]types.Type{types.Number},
			),
			"concat": types.NewFunction(
				[]types.Type{types.String, types.String},
				[]types.Type{types.String},
			),
			"greet": types.NewFunction(
				[]types.Type{types.String},
				[]types.Type{types.String, types.Optional(types.LuaError)},
			),
		},
	}

	m.SetExport(moduleType)
	return m
}

func TestTypeChecker_ModuleMethodCall_Valid(t *testing.T) {
	// Create checker with standard library
	chk := checker.NewWithConfig(checker.StrictConfig())
	chk.SetGlobals(types.StandardLibrary())

	// Import our test module
	imports := map[string]*types.TypeManifest{
		"mylib": testModuleTypes(),
	}

	// Valid code: correct types
	source := `
local mylib = require("mylib")

local x: number = mylib.add(1, 2)
local s: string = mylib.concat("hello", "world")
`

	manifest, errors, err := chk.CheckStringWithImports(source, "test.lua", imports)
	require.NoError(t, err)
	require.NotNil(t, manifest)

	if errors != nil && errors.HasErrors() {
		for _, e := range errors.Errors() {
			t.Logf("Unexpected error: %s", e.Error())
		}
	}
	assert.False(t, errors != nil && errors.HasErrors(), "expected no type errors")
}

func TestTypeChecker_ModuleMethodCall_WrongArgType(t *testing.T) {
	chk := checker.NewWithConfig(checker.StrictConfig())
	chk.SetGlobals(types.StandardLibrary())

	imports := map[string]*types.TypeManifest{
		"mylib": testModuleTypes(),
	}

	// Invalid: passing string to number parameter
	source := `
local mylib = require("mylib")

local x = mylib.add("not a number", 2)
`

	_, errors, err := chk.CheckStringWithImports(source, "test.lua", imports)
	require.NoError(t, err)
	require.NotNil(t, errors)
	require.True(t, errors.HasErrors(), "expected type error for wrong argument type")

	// Verify error location
	errs := errors.Errors()
	require.GreaterOrEqual(t, len(errs), 1)

	found := false
	for _, err := range errs {
		t.Logf("Error at line %d: %s", err.Pos.Line, err.Message)
		if err.Pos.Line == 4 {
			found = true
		}
	}
	assert.True(t, found, "expected error on line 4 where wrong argument is passed")
}

func TestTypeChecker_ModuleMethodCall_WrongReturnAssignment(t *testing.T) {
	chk := checker.NewWithConfig(checker.StrictConfig())
	chk.SetGlobals(types.StandardLibrary())

	imports := map[string]*types.TypeManifest{
		"mylib": testModuleTypes(),
	}

	// Invalid: assigning number result to string variable
	source := `
local mylib = require("mylib")

local s: string = mylib.add(1, 2)
`

	_, errors, err := chk.CheckStringWithImports(source, "test.lua", imports)
	require.NoError(t, err)
	require.NotNil(t, errors)
	require.True(t, errors.HasErrors(), "expected type error for wrong return assignment")

	for _, e := range errors.Errors() {
		t.Logf("Error at line %d: %s", e.Pos.Line, e.Message)
	}
}

func TestTypeChecker_ModuleMethodCall_UndefinedMethod(t *testing.T) {
	chk := checker.NewWithConfig(checker.StrictConfig())
	chk.SetGlobals(types.StandardLibrary())

	imports := map[string]*types.TypeManifest{
		"mylib": testModuleTypes(),
	}

	// Invalid: calling non-existent method
	source := `
local mylib = require("mylib")

local x = mylib.nonexistent(1, 2)
`

	_, errors, err := chk.CheckStringWithImports(source, "test.lua", imports)
	require.NoError(t, err)
	require.NotNil(t, errors)
	require.True(t, errors.HasErrors(), "expected type error for undefined method")

	found := false
	for _, e := range errors.Errors() {
		t.Logf("Error at line %d: %s", e.Pos.Line, e.Message)
		if e.Pos.Line == 4 {
			found = true
		}
	}
	assert.True(t, found, "expected error on line 4 where undefined method is called")
}

func TestTypeChecker_LibraryChunk_WithManifest(t *testing.T) {
	chk := checker.NewWithConfig(checker.StrictConfig())
	chk.SetGlobals(types.StandardLibrary())

	// Library chunk that exports typed functions
	libSource := `
---@param x number
---@param y number
---@return number
local function add(x, y)
    return x + y
end

---@param s string
---@return string
local function upper(s)
    return string.upper(s)
end

return {
    add = add,
    upper = upper
}
`

	// Type check the library and get its manifest
	libManifest, errors, err := chk.CheckString(libSource, "mathlib.lua")
	require.NoError(t, err)

	if errors != nil && errors.HasErrors() {
		for _, e := range errors.Errors() {
			t.Logf("Library error: %s", e.Error())
		}
	}
	require.NotNil(t, libManifest)

	// Log the manifest export type for debugging
	t.Logf("Library manifest export: %v", libManifest.Export)

	// Note: Full table return type inference is not yet implemented,
	// so the manifest export may be 'any'. This test documents current behavior.
	// The key functionality (Go-defined module types) is tested in other tests.
}

func TestTypeChecker_ErrorHandling_Pattern(t *testing.T) {
	chk := checker.NewWithConfig(checker.StrictConfig())
	chk.SetGlobals(types.StandardLibrary())

	imports := map[string]*types.TypeManifest{
		"mylib": testModuleTypes(),
	}

	// Test error handling pattern with Error type
	source := `
local mylib = require("mylib")

local result, err = mylib.greet("world")
if err then
    -- err should be Error type
    local msg: string = err:message()
    local kind: string = err:kind()
end
`

	_, errors, parseErr := chk.CheckStringWithImports(source, "test.lua", imports)
	require.NoError(t, parseErr)

	if errors != nil && errors.HasErrors() {
		for _, e := range errors.Errors() {
			t.Logf("Error: %s", e.Error())
		}
	}
	// This test documents the expected behavior
	// Error handling pattern should type check correctly
}

func TestTypeChecker_ManifestExport(t *testing.T) {
	chk := checker.NewWithConfig(checker.StrictConfig())
	chk.SetGlobals(types.StandardLibrary())

	// Check that manifest correctly captures export type
	manifest := testModuleTypes()

	require.NotNil(t, manifest.Export)
	assert.Equal(t, types.KindInterface, manifest.Export.Kind())

	iface, ok := manifest.Export.(*types.InterfaceType)
	require.True(t, ok)
	assert.Equal(t, "mylib", iface.Name)

	// Check methods exist
	addFn, ok := iface.Methods["add"]
	require.True(t, ok)
	assert.Len(t, addFn.Params, 2)
	assert.Len(t, addFn.Returns, 1)

	concatFn, ok := iface.Methods["concat"]
	require.True(t, ok)
	assert.Len(t, concatFn.Params, 2)
}

func TestTypeChecker_Base64Import(t *testing.T) {
	chk := checker.NewWithConfig(checker.StrictConfig())
	chk.SetGlobals(types.StandardLibrary())

	// Import base64 module types directly
	imports := map[string]*types.TypeManifest{
		"base64": base64mod.ModuleTypes(),
	}

	code := `
local base64 = require("base64")

local function test_encode(): boolean
    local input: string = "hello world"
    local encoded: string = base64.encode(input)
    return encoded == "aGVsbG8gd29ybGQ="
end

return { test_encode = test_encode }
`

	_, errors, err := chk.CheckStringWithImports(code, "test.lua", imports)
	require.NoError(t, err)

	if errors != nil && errors.HasErrors() {
		for _, e := range errors.Errors() {
			t.Errorf("unexpected error: %s", e.Error())
		}
	}
}

func TestTypeChecker_GenericFunctions(t *testing.T) {
	chk := checker.NewWithConfig(checker.StrictConfig())
	chk.SetGlobals(types.StandardLibrary())

	// Multiple generic functions with same type param name should not conflict
	code := `
local function identity<T>(x: T): T
    return x
end

local function wrap<T>(x: T): {T}
    return {x}
end

local function pair<K, V>(key: K, value: V): {key: K, value: V}
    return { key = key, value = value }
end

local n: number = identity(42)
local s: string = identity("hello")
local p: {key: string, value: number} = pair("age", 30)

return { identity = identity, wrap = wrap, pair = pair }
`

	_, errors, err := chk.CheckString(code, "test.lua")
	require.NoError(t, err)

	if errors != nil && errors.HasErrors() {
		for _, e := range errors.Errors() {
			t.Errorf("unexpected error: %s", e.Error())
		}
	}
}

// testSubmoduleTypes creates a module with submodules for testing.
// Simulates a module like text with text.regexp submodule.
func testSubmoduleTypes() *types.TypeManifest {
	m := types.NewManifest("mymod")

	// Define a userdata type returned by submodule method
	widgetType := &types.InterfaceType{
		Name: "mymod.Widget",
		Methods: map[string]*types.FunctionType{
			"process": types.NewFunction([]types.Type{types.String}, []types.Type{types.Boolean}),
			"value":   types.NewFunction(nil, []types.Type{types.String}),
		},
	}

	// Define submodule type
	factorySubType := &types.InterfaceType{
		Name: "mymod.factory",
		Methods: map[string]*types.FunctionType{
			"create": types.NewFunction([]types.Type{types.String}, []types.Type{widgetType, types.Optional(types.LuaError)}),
		},
	}

	m.DefineType("Widget", widgetType)

	moduleType := &types.InterfaceType{
		Name: "mymod",
		Fields: map[string]types.Type{
			"factory": factorySubType,
		},
	}

	m.SetExport(moduleType)
	return m
}

func TestTypeChecker_SubmoduleAccess(t *testing.T) {
	chk := checker.NewWithConfig(checker.StrictConfig())
	chk.SetGlobals(types.StandardLibrary())

	manifest := testSubmoduleTypes()
	imports := map[string]*types.TypeManifest{
		"mymod": manifest,
	}

	// Verify manifest is set up correctly
	require.NotNil(t, manifest.Export, "manifest export should not be nil")
	moduleType, ok := manifest.Export.(*types.InterfaceType)
	require.True(t, ok, "export should be InterfaceType")
	factoryField, ok := moduleType.Fields["factory"]
	require.True(t, ok, "should have factory field")
	factoryType, ok := factoryField.(*types.InterfaceType)
	require.True(t, ok, "factory should be InterfaceType")
	createMethod, ok := factoryType.Methods["create"]
	require.True(t, ok, "factory should have create method")
	require.NotEmpty(t, createMethod.Returns, "create should have returns")
	t.Logf("create method returns: %v", createMethod.Returns[0].String())

	// Test submodule method call
	code := `
local mymod = require("mymod")
local widget = mymod.factory.create("test")
local s: string = widget:value()
return s
`

	_, errors, err := chk.CheckStringWithImports(code, "test.lua", imports)
	require.NoError(t, err)

	if errors != nil && errors.HasErrors() {
		for _, e := range errors.Errors() {
			t.Errorf("unexpected error: %s", e.Error())
		}
	}
}
