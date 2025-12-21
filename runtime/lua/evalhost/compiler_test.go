package evalhost

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/modules/json"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
)

// safeModules returns modules safe for eval testing
func safeModules() []*luaapi.ModuleDef {
	return []*luaapi.ModuleDef{
		json.Module,
		timemod.Module,
	}
}

func TestCompiler_Compile_Basic(t *testing.T) {
	compiler := NewCompiler(safeModules())

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

func TestCompiler_Compile_SyntaxError(t *testing.T) {
	compiler := NewCompiler(safeModules())

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
	compiler := NewCompiler(modules)

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
	compiler := NewCompiler(safeModules())

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
	compiler := NewCompiler(safeModules())

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
	compiler := NewCompiler(safeModules())

	binder := compiler.GetModuleBinder([]string{"json"})
	assert.NotNil(t, binder)
}

func TestCompiler_ModuleInfo(t *testing.T) {
	compiler := NewCompiler(safeModules())

	info, ok := compiler.ModuleInfo("json")
	assert.True(t, ok)
	assert.Equal(t, "json", info.Name)

	_, ok = compiler.ModuleInfo("nonexistent")
	assert.False(t, ok)
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
	compiler := NewCompiler(modules)

	// Safe module should be allowed
	assert.True(t, compiler.IsModuleAllowed("safe"))

	// Modules with forbidden classes should not be allowed
	assert.False(t, compiler.IsModuleAllowed("unsafe_process"))
	assert.False(t, compiler.IsModuleAllowed("unsafe_storage"))
	assert.False(t, compiler.IsModuleAllowed("unsafe_network"))
}

func TestCompiler_CustomForbiddenClasses(t *testing.T) {
	ioModule := &luaapi.ModuleDef{
		Name:  "io_module",
		Class: []string{luaapi.ClassIO},
	}

	modules := []*luaapi.ModuleDef{ioModule}

	// With default settings, IO is allowed
	compilerDefault := NewCompiler(modules)
	assert.True(t, compilerDefault.IsModuleAllowed("io_module"))

	// With custom forbidden classes including IO
	compilerStrict := NewCompiler(modules,
		WithForbiddenClasses(luaapi.ClassIO, luaapi.ClassProcess),
	)
	assert.False(t, compilerStrict.IsModuleAllowed("io_module"))
}

func TestCompiler_Compile_MultipleModules(t *testing.T) {
	compiler := NewCompiler(safeModules())

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
	compiler := NewCompiler(safeModules())

	program, err := compiler.Compile(CompileCmd{
		Source:  ``,
		Method:  "handle",
		Modules: []string{"json"},
	})

	require.NoError(t, err)
	assert.NotNil(t, program)
}

func TestCompiler_Compile_ReturnTable(t *testing.T) {
	compiler := NewCompiler(safeModules())

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
	// Verify the default forbidden classes
	compiler := NewCompiler(safeModules())

	forbidden := compiler.GetForbiddenClasses()
	assert.Contains(t, forbidden, luaapi.ClassProcess)
	assert.Contains(t, forbidden, luaapi.ClassStorage)
	assert.Contains(t, forbidden, luaapi.ClassNetwork)
}
