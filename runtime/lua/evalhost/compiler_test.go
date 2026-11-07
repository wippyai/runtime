// SPDX-License-Identifier: MPL-2.0

package evalhost

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/compiler/parse"
	typeio "github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	luacode "github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/modules/json"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
)

// modulesProvider returns a provider serving the given modules keyed by name.
func modulesProvider(modules ...*luaapi.ModuleDef) ModuleProvider {
	byName := make(map[string]*luaapi.ModuleDef, len(modules))
	for _, m := range modules {
		byName[m.Name] = m
	}
	return func() map[string]*luaapi.ModuleDef { return byName }
}

// safeModulesProvider returns a provider for modules safe for eval testing
func safeModulesProvider() ModuleProvider {
	return modulesProvider(json.Module, timemod.Module)
}

func TestCompiler_Compile_Basic(t *testing.T) {
	compiler := NewCompiler(safeModulesProvider())

	program, err := compiler.Compile(CompileCmd{
		Source: `
			local function handle(x)
				return x * 2
			end
			return { handle = handle }
		`,
		Method:  "handle",
		Modules: []string{"json"},
	})

	require.NoError(t, err)
	assert.NotNil(t, program)
	assert.Equal(t, "handle", program.Method())
	assert.Equal(t, []string{"json"}, program.Modules())
	assert.NotNil(t, program.Proto())
}

func TestCompiler_Compile_ModuleTypesAreIncluded(t *testing.T) {
	manifest := typeio.NewManifest("mock")
	manifest.DefineType("Point", typ.NewRecord().Field("x", typ.Number).Build())
	mockMod := &luaapi.ModuleDef{
		Name:  "mock",
		Class: []string{luaapi.ClassDeterministic},
		Types: func() *typeio.Manifest { return manifest },
	}
	compiler := NewCompiler(modulesProvider(mockMod))

	program, err := compiler.Compile(CompileCmd{
		Source:  `local p = Point({x = 1}); return p`,
		Method:  "handle",
		Modules: []string{"mock"},
	})

	require.NoError(t, err)
	require.NotNil(t, program)
	require.NotNil(t, program.Proto())
	require.NotEmpty(t, program.Proto().TypeInfo)

	decoded, err := typeio.DecodeManifest(program.Proto().TypeInfo)
	require.NoError(t, err)
	_, ok := decoded.Types["Point"]
	assert.True(t, ok, "expected Point in type info")
}

func TestCompiler_Compile_SourceDeclaredTypesAreIncluded(t *testing.T) {
	compiler := NewCompiler(safeModulesProvider())

	program, err := compiler.Compile(CompileCmd{
		Source: `
			type Point = {x: number, y: number}

			local p, perr = Point:is({x = 1, y = 2})
			if perr then error(tostring(perr)) end

			local bad, berr = Point:is({x = 1, y = "bad"})
			if bad ~= nil or berr == nil then error("expected invalid point error") end

			return { sum = p.x + p.y }
		`,
		Modules: []string{"json"},
	})

	require.NoError(t, err)
	require.NotNil(t, program)
	require.NotNil(t, program.Proto())
	require.NotEmpty(t, program.Proto().TypeInfo)

	decoded, err := typeio.DecodeManifest(program.Proto().TypeInfo)
	require.NoError(t, err)
	_, ok := decoded.Types["Point"]
	require.True(t, ok, "expected source-declared Point in type info")

	l := lua.NewState()
	defer l.Close()
	fn := l.LoadProto(program.Proto())
	l.Push(fn)
	require.NoError(t, l.PCall(0, 1, nil))
	result := l.CheckTable(-1)
	assert.Equal(t, lua.LInteger(3), result.RawGetString("sum"))
}

func TestCompiler_Compile_SyntaxError(t *testing.T) {
	compiler := NewCompiler(safeModulesProvider())

	program, err := compiler.Compile(CompileCmd{
		Source:  `this is not valid lua syntax!!!`,
		Method:  "handle",
		Modules: []string{"json"},
	})

	assert.Error(t, err)
	assert.Nil(t, program)
	assert.Contains(t, err.Error(), "parse error")
}

func TestCompiler_Compile_ForbiddenClass(t *testing.T) {
	// Create a mock module with process class
	mockMod := &luaapi.ModuleDef{
		Name:  "badmodule",
		Class: []string{luaapi.ClassProcess},
	}

	modules := []*luaapi.ModuleDef{
		json.Module,
		mockMod,
	}
	compiler := NewCompiler(modulesProvider(modules...))

	// Requesting a module with forbidden class should fail
	program, err := compiler.Compile(CompileCmd{
		Source:  `return {}`,
		Method:  "handle",
		Modules: []string{"badmodule"},
	})

	assert.Error(t, err)
	assert.Nil(t, program)
	assert.Contains(t, err.Error(), "forbidden class")
}

func TestCompiler_Compile_UnavailableModule(t *testing.T) {
	compiler := NewCompiler(safeModulesProvider())

	program, err := compiler.Compile(CompileCmd{
		Source:  `return {}`,
		Method:  "handle",
		Modules: []string{"nonexistent"},
	})

	assert.Error(t, err)
	assert.Nil(t, program)
	assert.Contains(t, err.Error(), "not available")
}

func TestCompiler_Compile_DefaultModules(t *testing.T) {
	compiler := NewCompiler(safeModulesProvider())

	program, err := compiler.Compile(CompileCmd{
		Source: `return {}`,
		Method: "handle",
	})

	require.NoError(t, err)
	assert.NotNil(t, program)
	// Default modules are derived from class filtering
	assert.NotEmpty(t, program.Modules())
}

func TestCompiler_GetModuleBinder(t *testing.T) {
	compiler := NewCompiler(safeModulesProvider())

	binder := compiler.GetModuleBinder([]string{"json"})
	assert.NotNil(t, binder)
}

func TestCompiler_ClassBasedFiltering(t *testing.T) {
	// Create modules with different classes
	safeModule := &luaapi.ModuleDef{
		Name:  "safe",
		Class: []string{luaapi.ClassDeterministic, luaapi.ClassEncoding},
	}
	processModule := &luaapi.ModuleDef{
		Name:  "unsafe_process",
		Class: []string{luaapi.ClassProcess},
	}
	storageModule := &luaapi.ModuleDef{
		Name:  "unsafe_storage",
		Class: []string{luaapi.ClassStorage},
	}
	networkModule := &luaapi.ModuleDef{
		Name:  "unsafe_network",
		Class: []string{luaapi.ClassNetwork},
	}

	modules := []*luaapi.ModuleDef{
		safeModule,
		processModule,
		storageModule,
		networkModule,
	}
	compiler := NewCompiler(modulesProvider(modules...))

	// Safe module should compile
	_, err := compiler.Compile(CompileCmd{Source: "return {}", Modules: []string{"safe"}})
	assert.NoError(t, err)

	// Modules with forbidden classes should fail to compile
	_, err = compiler.Compile(CompileCmd{Source: "return {}", Modules: []string{"unsafe_process"}})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "forbidden class")

	_, err = compiler.Compile(CompileCmd{Source: "return {}", Modules: []string{"unsafe_storage"}})
	assert.Error(t, err)

	_, err = compiler.Compile(CompileCmd{Source: "return {}", Modules: []string{"unsafe_network"}})
	assert.Error(t, err)
}

func TestCompiler_CustomForbiddenClasses(t *testing.T) {
	ioModule := &luaapi.ModuleDef{
		Name:  "io_module",
		Class: []string{luaapi.ClassIO},
	}

	provider := modulesProvider(ioModule)

	// With default settings, IO is allowed
	compilerDefault := NewCompiler(provider)
	_, err := compilerDefault.Compile(CompileCmd{Source: "return {}", Modules: []string{"io_module"}})
	assert.NoError(t, err)

	// With custom forbidden classes including IO
	compilerStrict := NewCompiler(provider,
		WithForbiddenClasses(luaapi.ClassIO, luaapi.ClassProcess),
	)
	_, err = compilerStrict.Compile(CompileCmd{Source: "return {}", Modules: []string{"io_module"}})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "forbidden class")
}

func TestCompiler_Compile_MultipleModules(t *testing.T) {
	compiler := NewCompiler(safeModulesProvider())

	program, err := compiler.Compile(CompileCmd{
		Source: `
			local json = require("json")
			local time = require("time")
			return { handle = function() return json.encode({t = time.now()}) end }
		`,
		Method:  "handle",
		Modules: []string{"json", "time"},
	})

	require.NoError(t, err)
	assert.NotNil(t, program)
	assert.Equal(t, []string{"json", "time"}, program.Modules())
}

func TestCompiler_Compile_EmptySource(t *testing.T) {
	compiler := NewCompiler(safeModulesProvider())

	program, err := compiler.Compile(CompileCmd{
		Source:  ``,
		Method:  "handle",
		Modules: []string{"json"},
	})

	require.NoError(t, err)
	assert.NotNil(t, program)
}

func TestCompiler_Compile_ReturnTable(t *testing.T) {
	compiler := NewCompiler(safeModulesProvider())

	program, err := compiler.Compile(CompileCmd{
		Source: `
			local M = {}
			function M.add(a, b) return a + b end
			function M.sub(a, b) return a - b end
			return M
		`,
		Method:  "add",
		Modules: []string{"json"},
	})

	require.NoError(t, err)
	assert.NotNil(t, program)
	assert.Equal(t, "add", program.Method())
}

func TestCompiler_ForbiddenClasses(t *testing.T) {
	// Verify the default forbidden classes by attempting to compile with them
	processModule := &luaapi.ModuleDef{Name: "proc", Class: []string{luaapi.ClassProcess}}
	storageModule := &luaapi.ModuleDef{Name: "stor", Class: []string{luaapi.ClassStorage}}
	networkModule := &luaapi.ModuleDef{Name: "net", Class: []string{luaapi.ClassNetwork}}

	compiler := NewCompiler(modulesProvider(processModule, storageModule, networkModule))

	// All should be blocked by default
	_, err := compiler.Compile(CompileCmd{Source: "return {}", Modules: []string{"proc"}})
	assert.Error(t, err)

	_, err = compiler.Compile(CompileCmd{Source: "return {}", Modules: []string{"stor"}})
	assert.Error(t, err)

	_, err = compiler.Compile(CompileCmd{Source: "return {}", Modules: []string{"net"}})
	assert.Error(t, err)
}

// =============================================================================
// Security Tests - Verify process module is NOT accessible in eval context
// =============================================================================

func TestCompiler_Security_ProcessModuleBlocked(t *testing.T) {
	// Even if process module is in available modules, ClassProcess blocks it
	processModule := &luaapi.ModuleDef{
		Name:  "process",
		Class: []string{luaapi.ClassProcess},
	}

	modules := []*luaapi.ModuleDef{
		json.Module,
		processModule,
	}
	compiler := NewCompiler(modulesProvider(modules...))

	// Attempting to compile with process module should fail
	_, err := compiler.Compile(CompileCmd{
		Source:  `local p = require("process"); return p`,
		Method:  "handle",
		Modules: []string{"process"},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "forbidden class")
}

func TestCompiler_Security_ModuleBinderExcludesProcess(t *testing.T) {
	// Create compiler with ONLY safe modules (no process)
	compiler := NewCompiler(safeModulesProvider())

	// Get binder for json module only
	binder := compiler.GetModuleBinder([]string{"json"})

	// Create Lua state and apply binder
	l := lua.NewState()
	defer l.Close()

	binder(l)

	// Verify json is available
	jsonMod := l.GetGlobal("json")
	assert.NotEqual(t, lua.LNil, jsonMod)

	// Verify process is NOT available (never bound)
	processMod := l.GetGlobal("process")
	assert.Equal(t, lua.LNil, processMod)
}

func TestCompiler_Security_RuntimeRequireProcessFails(t *testing.T) {
	compiler := NewCompiler(safeModulesProvider())

	// Compile code that tries to require process at runtime
	// This should compile (we don't analyze code), but at runtime
	// the process module won't be available
	program, err := compiler.Compile(CompileCmd{
		Source: `
			local function handle()
				local ok, proc = pcall(require, "process")
				if ok then
					error("process module should not be available")
				end
				return "process_blocked"
			end
			return { handle = handle }
		`,
		Method:  "handle",
		Modules: []string{"json"},
	})
	require.NoError(t, err)

	// Run the compiled program
	l := lua.NewState()
	defer l.Close()

	// Apply module binder
	binder := compiler.GetModuleBinder(program.Modules())
	binder(l)

	// Load and run the proto
	fn := l.NewFunctionFromProto(program.Proto())
	l.Push(fn)
	err = l.PCall(0, 1, nil)
	require.NoError(t, err)

	// Get handle function and call it
	result := l.Get(-1)
	require.Equal(t, lua.LTTable, result.Type())

	tbl := result.(*lua.LTable)
	handleFn := tbl.RawGetString("handle")
	require.Equal(t, lua.LTFunction, handleFn.Type())

	l.Push(handleFn)
	err = l.PCall(0, 1, nil)
	require.NoError(t, err)

	// Should return "process_blocked" - meaning require("process") failed
	ret := l.Get(-1)
	assert.Equal(t, lua.LString("process_blocked"), ret)
}

func TestCompiler_Security_EnvModuleBlockedByClassProcess(t *testing.T) {
	// Env module has ClassProcess - should be blocked in eval
	envModule := &luaapi.ModuleDef{
		Name:  "env",
		Class: []string{luaapi.ClassProcess, luaapi.ClassNondeterministic},
	}

	modules := []*luaapi.ModuleDef{
		json.Module,
		envModule,
	}
	compiler := NewCompiler(modulesProvider(modules...))

	// Requesting env module should fail due to ClassProcess
	_, err := compiler.Compile(CompileCmd{
		Source:  `return require("env")`,
		Method:  "handle",
		Modules: []string{"env"},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "forbidden class")
}

// TestSourceDeclaredTypes_MatchesFullCheck pins the scan that decides whether
// a source is type checked: for every shape of type definition, the types it
// yields equal the types a full check of the source exports.
func TestSourceDeclaredTypes_MatchesFullCheck(t *testing.T) {
	available := safeModulesProvider()()
	modules := []string{"json"}
	cases := map[string]struct {
		source   string
		declared bool
	}{
		"no types": {source: `
			local json = require("json")
			local function enc(v) return json.encode(v) end
			return { run = function(x) return enc({ x = x }) end }`},
		"root": {declared: true, source: `
			type Point = {x: number}
			return { run = function() return Point:is({x = 1}) end }`},
		"do block": {declared: true, source: `
			do type Point = {x: number} end
			return {}`},
		"if else": {declared: true, source: `
			if true then local a = 1 elseif false then local b = 2 else type Point = {x: number} end
			return {}`},
		"while": {declared: true, source: `
			while false do type Point = {x: number} end
			return {}`},
		"repeat": {declared: true, source: `
			repeat type Point = {x: number} until true
			return {}`},
		"numeric for": {declared: true, source: `
			for i = 1, 1 do type Point = {x: number} end
			return {}`},
		"generic for": {declared: true, source: `
			for _, v in ipairs({}) do type Point = {x: number} end
			return {}`},
		"interface": {source: `
			interface Shape function area(self: any): number end
			return {}`},
		"local function body": {source: `
			local function f() type Point = {x: number}; return Point:is({x = 1}) end
			return { run = f }`},
		"method body": {source: `
			local M = {}
			function M.run() type Point = {x: number}; return Point:is({x = 1}) end
			return M`},
		"function expression body": {source: `
			return { run = function() type Point = {x: number}; return Point:is({x = 1}) end }`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			chunk, err := parse.Parse(strings.NewReader(tc.source), "eval")
			require.NoError(t, err)
			require.Equal(t, tc.declared, declaresTypes(chunk))

			typeCfg := luacode.DefaultTypeCheckConfig()
			typeCfg.Enabled = true
			checker := luacode.NewTypeChecker(typeCfg, modulesForNames(modules, available))
			full, _ := checker.CheckParsed(chunk, "eval", nil)
			require.NotNil(t, full)

			got := sourceDeclaredTypes(chunk, modules, available)
			require.Equal(t, len(full.Types), len(got))
			for name, want := range full.Types {
				require.Contains(t, got, name)
				require.True(t, typ.TypeEquals(want, got[name]), "type %s", name)
			}
			require.Equal(t, tc.declared, len(got) > 0)
		})
	}
}
