package stages

import (
	"io"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	glua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/bytecode"
)

func TestBytecode_CompileFunction(t *testing.T) {
	ctx, _ := setupTestContext()
	ClearBytecodeResource()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "hello"),
			Kind: luaapi.Function,
			Data: payload.New(map[string]any{
				"source": `
local function handler(ctx)
    return "hello"
end
return { handler = handler }
`,
				"method":  "handler",
				"modules": []string{"json"},
				"pool": map[string]any{
					"size": 4,
				},
			}),
			Meta: map[string]any{
				"priority": "high",
			},
		},
	}

	stage := Bytecode()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	// Verify entry was transformed
	entry := entries[0]
	assert.Equal(t, luaapi.FunctionBytecode, entry.Kind)

	data := entry.Data.Data().(map[string]any)
	assert.Equal(t, BytecodeFSID, data["fs"])
	assert.Equal(t, "app/hello.luac", data["path"])
	assert.NotEmpty(t, data["hash"])
	assert.Equal(t, "handler", data["method"])
	assert.Equal(t, []string{"json"}, data["modules"])

	pool := data["pool"].(map[string]any)
	assert.Equal(t, 4, pool["size"])

	// Verify bytecode resource was created
	res := GetBytecodeResource()
	require.NotNil(t, res)
	assert.Equal(t, BytecodeFSID, res.ID.String())

	// Verify bytecode file exists
	f, err := res.FS.Open("app/hello.luac")
	require.NoError(t, err)
	defer f.Close()

	stat, err := f.Stat()
	require.NoError(t, err)
	assert.True(t, stat.Size() > 0)
}

func TestBytecode_CompileLibrary(t *testing.T) {
	ctx, _ := setupTestContext()
	ClearBytecodeResource()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("lib", "utils"),
			Kind: luaapi.Library,
			Data: payload.New(map[string]any{
				"source": `
local utils = {}
function utils.add(a, b)
    return a + b
end
return utils
`,
			}),
		},
	}

	stage := Bytecode()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	entry := entries[0]
	assert.Equal(t, luaapi.LibraryBytecode, entry.Kind)

	data := entry.Data.Data().(map[string]any)
	assert.Equal(t, BytecodeFSID, data["fs"])
	assert.Equal(t, "lib/utils.luac", data["path"])
	assert.NotEmpty(t, data["hash"])
}

func TestBytecode_CompileProcess(t *testing.T) {
	ctx, _ := setupTestContext()
	ClearBytecodeResource()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "worker"),
			Kind: luaapi.Process,
			Data: payload.New(map[string]any{
				"source": `
local function run(ctx)
    while true do
        coroutine.yield()
    end
end
return { run = run }
`,
				"method": "run",
			}),
		},
	}

	stage := Bytecode()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	entry := entries[0]
	assert.Equal(t, luaapi.ProcessBytecode, entry.Kind)

	data := entry.Data.Data().(map[string]any)
	assert.Equal(t, "run", data["method"])
}

func TestBytecode_MultipleEntries(t *testing.T) {
	ctx, _ := setupTestContext()
	ClearBytecodeResource()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "func1"),
			Kind: luaapi.Function,
			Data: payload.New(map[string]any{
				"source": `return { handler = function() return 1 end }`,
				"method": "handler",
			}),
		},
		{
			ID:   registry.NewID("app", "func2"),
			Kind: luaapi.Function,
			Data: payload.New(map[string]any{
				"source": `return { handler = function() return 2 end }`,
				"method": "handler",
			}),
		},
		{
			ID:   registry.NewID("lib", "shared"),
			Kind: luaapi.Library,
			Data: payload.New(map[string]any{
				"source": `return {}`,
			}),
		},
	}

	stage := Bytecode()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	assert.Equal(t, luaapi.FunctionBytecode, entries[0].Kind)
	assert.Equal(t, luaapi.FunctionBytecode, entries[1].Kind)
	assert.Equal(t, luaapi.LibraryBytecode, entries[2].Kind)

	res := GetBytecodeResource()
	require.NotNil(t, res)

	// Verify all files exist
	files := []string{"app/func1.luac", "app/func2.luac", "lib/shared.luac"}
	for _, path := range files {
		_, err := res.FS.Open(path)
		assert.NoError(t, err, "file %s should exist", path)
	}
}

func TestBytecode_PatternFilter(t *testing.T) {
	ctx, _ := setupTestContext()
	ClearBytecodeResource()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "include"),
			Kind: luaapi.Function,
			Data: payload.New(map[string]any{
				"source": `return { handler = function() end }`,
				"method": "handler",
			}),
		},
		{
			ID:   registry.NewID("app", "exclude"),
			Kind: luaapi.Function,
			Data: payload.New(map[string]any{
				"source": `return { handler = function() end }`,
				"method": "handler",
			}),
		},
	}

	stage := Bytecode("app:include")
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	// Only first entry should be compiled
	assert.Equal(t, luaapi.FunctionBytecode, entries[0].Kind)
	assert.Equal(t, luaapi.Function, entries[1].Kind) // unchanged
}

func TestBytecode_NamespaceWildcard(t *testing.T) {
	ctx, _ := setupTestContext()
	ClearBytecodeResource()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "func1"),
			Kind: luaapi.Function,
			Data: payload.New(map[string]any{
				"source": `return { handler = function() end }`,
				"method": "handler",
			}),
		},
		{
			ID:   registry.NewID("app", "func2"),
			Kind: luaapi.Function,
			Data: payload.New(map[string]any{
				"source": `return { handler = function() end }`,
				"method": "handler",
			}),
		},
		{
			ID:   registry.NewID("lib", "shared"),
			Kind: luaapi.Library,
			Data: payload.New(map[string]any{
				"source": `return {}`,
			}),
		},
	}

	stage := Bytecode("app:**")
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	// app namespace entries should be compiled
	assert.Equal(t, luaapi.FunctionBytecode, entries[0].Kind)
	assert.Equal(t, luaapi.FunctionBytecode, entries[1].Kind)
	// lib namespace should be unchanged
	assert.Equal(t, luaapi.Library, entries[2].Kind)
}

func TestBytecode_SkipsUnsupportedKinds(t *testing.T) {
	ctx, _ := setupTestContext()
	ClearBytecodeResource()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "func"),
			Kind: luaapi.Function,
			Data: payload.New(map[string]any{
				"source": `return { handler = function() end }`,
				"method": "handler",
			}),
		},
		{
			ID:   registry.NewID("app", "config"),
			Kind: "config.yaml", // unsupported kind
			Data: payload.New(map[string]any{
				"key": "value",
			}),
		},
	}

	stage := Bytecode()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	assert.Equal(t, luaapi.FunctionBytecode, entries[0].Kind)
	assert.Equal(t, "config.yaml", entries[1].Kind) // unchanged
}

func TestBytecode_InvalidSource(t *testing.T) {
	ctx, _ := setupTestContext()
	ClearBytecodeResource()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "broken"),
			Kind: luaapi.Function,
			Data: payload.New(map[string]any{
				"source": `this is not valid lua syntax!!!`,
				"method": "handler",
			}),
		},
	}

	stage := Bytecode()
	err := stage.Execute(ctx, &entries)
	assert.Error(t, err)
}

func TestBytecode_NoSource(t *testing.T) {
	ctx, _ := setupTestContext()
	ClearBytecodeResource()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "empty"),
			Kind: luaapi.Function,
			Data: payload.New(map[string]any{
				"method": "handler",
			}),
		},
	}

	stage := Bytecode()
	err := stage.Execute(ctx, &entries)
	assert.Error(t, err)
}

func TestBytecode_EmptyEntries(t *testing.T) {
	ctx, _ := setupTestContext()
	ClearBytecodeResource()

	var entries []registry.Entry

	stage := Bytecode()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	res := GetBytecodeResource()
	assert.Nil(t, res) // no resource created
}

func TestBytecode_PreservesImports(t *testing.T) {
	ctx, _ := setupTestContext()
	ClearBytecodeResource()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "func"),
			Kind: luaapi.Function,
			Data: payload.New(map[string]any{
				"source": `return { handler = function() end }`,
				"method": "handler",
				"imports": map[string]any{
					"utils": "lib:utils",
				},
			}),
		},
	}

	stage := Bytecode()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	data := entries[0].Data.Data().(map[string]any)
	imports := data["imports"].(map[string]any)
	assert.Equal(t, "lib:utils", imports["utils"])
}

func TestBytecode_HashFormat(t *testing.T) {
	ctx, _ := setupTestContext()
	ClearBytecodeResource()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "func"),
			Kind: luaapi.Function,
			Data: payload.New(map[string]any{
				"source": `return { handler = function() end }`,
				"method": "handler",
			}),
		},
	}

	stage := Bytecode()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	data := entries[0].Data.Data().(map[string]any)
	hash := data["hash"].(string)

	// Hash should be in format "sha256:hexstring"
	assert.True(t, len(hash) > 7, "hash should be longer than prefix")
	assert.Equal(t, "sha256:", hash[:7])
	assert.Equal(t, 64+7, len(hash)) // sha256 hex = 64 chars + "sha256:" prefix
}

func TestBytecode_NoNamespace(t *testing.T) {
	ctx, _ := setupTestContext()
	ClearBytecodeResource()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("", "global"),
			Kind: luaapi.Function,
			Data: payload.New(map[string]any{
				"source": `return { handler = function() end }`,
				"method": "handler",
			}),
		},
	}

	stage := Bytecode()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	data := entries[0].Data.Data().(map[string]any)
	assert.Equal(t, "global.luac", data["path"]) // no namespace prefix
}

func TestBytecode_StageName(t *testing.T) {
	stage := Bytecode()
	assert.Equal(t, "bytecode", stage.Name())
}

func TestBytecodeFS(t *testing.T) {
	ctx, _ := setupTestContext()
	ClearBytecodeResource()

	// Before compilation, should return nil
	assert.Nil(t, BytecodeFS())

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "func"),
			Kind: luaapi.Function,
			Data: payload.New(map[string]any{
				"source": `return { handler = function() end }`,
				"method": "handler",
			}),
		},
	}

	stage := Bytecode()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	// After compilation, should return fs.FS
	fsys := BytecodeFS()
	require.NotNil(t, fsys)

	// Verify it's usable
	_, ok := fsys.(fs.FS)
	assert.True(t, ok)
}

func TestBytecode_EndToEnd_CompileAndExecute(t *testing.T) {
	ctx, _ := setupTestContext()
	ClearBytecodeResource()

	// Lua script that returns a value when executed
	luaSource := `
local function add(a, b)
    return a + b
end

local function greet(name)
    return "Hello, " .. name .. "!"
end

return {
    add = add,
    greet = greet,
    value = 42
}
`

	entries := []registry.Entry{
		{
			ID:   registry.NewID("test", "math"),
			Kind: luaapi.Library,
			Data: payload.New(map[string]any{
				"source": luaSource,
			}),
		},
	}

	// Compile to bytecode
	stage := Bytecode()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	// Get the bytecode filesystem
	fsys := BytecodeFS()
	require.NotNil(t, fsys)

	// Read the compiled bytecode
	bcFile, err := fsys.Open("test/math.luac")
	require.NoError(t, err)
	defer bcFile.Close()

	bcData, err := io.ReadAll(bcFile)
	require.NoError(t, err)
	require.NotEmpty(t, bcData)

	// Load bytecode into a Lua state and execute
	proto, err := bytecode.Undump(bcData)
	require.NoError(t, err)

	L := glua.NewState()
	defer L.Close()

	// Load the proto as a function
	lfunc := L.NewFunctionFromProto(proto)
	L.Push(lfunc)

	// Execute the chunk to get the returned table
	err = L.PCall(0, 1, nil)
	require.NoError(t, err)

	// Get the returned table
	result := L.Get(-1)
	require.Equal(t, glua.LTTable, result.Type())
	tbl := result.(*glua.LTable)

	// Verify the value field (can be LNumber or LInteger)
	val := tbl.RawGetString("value")
	require.True(t, val.Type() == glua.LTNumber || val.Type() == glua.LTInteger)
	assert.Equal(t, float64(42), float64(glua.LVAsNumber(val)))

	// Call the add function
	addFn := tbl.RawGetString("add")
	require.Equal(t, glua.LTFunction, addFn.Type())

	L.Push(addFn)
	L.Push(glua.LNumber(10))
	L.Push(glua.LNumber(32))
	err = L.PCall(2, 1, nil)
	require.NoError(t, err)

	addResult := L.Get(-1)
	assert.Equal(t, float64(42), float64(glua.LVAsNumber(addResult)))
	L.Pop(1)

	// Call the greet function
	greetFn := tbl.RawGetString("greet")
	require.Equal(t, glua.LTFunction, greetFn.Type())

	L.Push(greetFn)
	L.Push(glua.LString("World"))
	err = L.PCall(1, 1, nil)
	require.NoError(t, err)

	greetResult := L.Get(-1)
	assert.Equal(t, glua.LString("Hello, World!"), greetResult)
}

func TestBytecode_EndToEnd_FunctionWithHandler(t *testing.T) {
	ctx, _ := setupTestContext()
	ClearBytecodeResource()

	// Function with handler pattern used in the runtime
	luaSource := `
local function handler(ctx)
    local input = ctx.input or 0
    return { result = input * 2, status = "ok" }
end

return { handler = handler }
`

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "double"),
			Kind: luaapi.Function,
			Data: payload.New(map[string]any{
				"source": luaSource,
				"method": "handler",
			}),
		},
	}

	// Compile to bytecode
	stage := Bytecode()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	// Verify entry transformation
	require.Equal(t, luaapi.FunctionBytecode, entries[0].Kind)

	// Get bytecode and execute
	fsys := BytecodeFS()
	require.NotNil(t, fsys)

	bcFile, err := fsys.Open("app/double.luac")
	require.NoError(t, err)
	defer bcFile.Close()

	bcData, err := io.ReadAll(bcFile)
	require.NoError(t, err)

	proto, err := bytecode.Undump(bcData)
	require.NoError(t, err)

	L := glua.NewState()
	defer L.Close()

	// Execute bytecode
	lfunc := L.NewFunctionFromProto(proto)
	L.Push(lfunc)
	err = L.PCall(0, 1, nil)
	require.NoError(t, err)

	// Get the module table
	module := L.Get(-1).(*glua.LTable)

	// Get and call the handler
	handler := module.RawGetString("handler")
	require.Equal(t, glua.LTFunction, handler.Type())

	// Create a mock context table
	ctxTable := L.NewTable()
	ctxTable.RawSetString("input", glua.LNumber(21))

	L.Push(handler)
	L.Push(ctxTable)
	err = L.PCall(1, 1, nil)
	require.NoError(t, err)

	// Verify result
	result := L.Get(-1).(*glua.LTable)
	assert.Equal(t, glua.LNumber(42), result.RawGetString("result"))
	assert.Equal(t, glua.LString("ok"), result.RawGetString("status"))
}
