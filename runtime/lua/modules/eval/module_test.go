package eval

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/channel"
	"github.com/wippyai/runtime/runtime/lua/engine/coroutine"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func setupTestEnvironment(t *testing.T) (*engine.CoroutineVM, *engine.Runner, context.Context) {
	logger := zaptest.NewLogger(t)

	bus := &mockEventBus{}
	jsonMod := &mockModule{name: "json"}
	timeMod := &mockModule{name: "time"}
	channelMod := channel.NewChannelModule()

	cm, err := code.NewCodeManager(zap.NewNop(), bus, code.Config{
		Modules:        []luaapi.Module{jsonMod, timeMod, channelMod},
		ProtoCacheSize: 100,
		MainCacheSize:  50,
	})
	require.NoError(t, err)

	libNode := code.Node{
		ID:     registry.ID{NS: "app", Name: "mylib"},
		Kind:   luaapi.KindLibrary,
		Source: `return {hello = function() return "world" end}`,
	}
	err = cm.AddNode(context.Background(), libNode, nil)
	require.NoError(t, err)

	module := NewEvalModule()

	vm, err := engine.NewCVM(logger)
	require.NoError(t, err)

	L := vm.State()
	L.PreloadModule(module.Name(), module.Loader)

	runner := engine.NewRunner(vm,
		engine.WithLayer(coroutine.NewCoroutineLayer()),
		engine.WithLayer(channel.NewChannelLayer()))

	ctx := ctxapi.NewRootContext()
	ac := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, ac)
	ac.With(luaapi.CodeManagerKey, cm)
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	return vm, runner, ctx
}

func TestEvalModule_Name(t *testing.T) {
	mod := NewEvalModule()
	assert.Equal(t, "eval", mod.Name())
}

func TestEvalModule_CompileAndRun(t *testing.T) {
	vm, runner, ctx := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_compile_and_run()
			local eval = require("eval")

			local program, err = eval.compile("function add(a, b) return a + b end", "add")
			if err then error(err) end
			assert(program ~= nil, "Program should not be nil")
			assert(type(program) == "userdata", "Program should be userdata")

			local result1, err1 = program:run("add", 10, 20)
			if err1 then error(err1) end
			assert(result1 == 30, "10 + 20 should equal 30, got: " .. tostring(result1))

			local result2, err2 = program:run("add", 5, 15)
			if err2 then error(err2) end
			assert(result2 == 20, "5 + 15 should equal 20, got: " .. tostring(result2))

			return {
				first = result1,
				second = result2,
				success = true
			}
		end
	`, "test", "test_compile_and_run")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_compile_and_run")
	require.NoError(t, err)

	assert.NotNil(t, result)
}

func TestEvalModule_RunOneShot(t *testing.T) {
	vm, runner, ctx := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_run_oneshot()
			local eval = require("eval")

			local result, err = eval.run({
				source = "function main() return 42 end",
				method = "main"
			})
			if err then error(err) end
			assert(result == 42, "Should return 42, got: " .. tostring(result))

			return {result = result, success = true}
		end
	`, "test", "test_run_oneshot")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_run_oneshot")
	require.NoError(t, err)

	assert.NotNil(t, result)
}

func TestEvalModule_WithModules(t *testing.T) {
	vm, runner, ctx := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_with_modules()
			local eval = require("eval")

			local program, err = eval.compile(
				"function test_json() local json = require('json'); return json.test end",
				"test_json",
				{modules = {"json"}}
			)
			if err then error(err) end

			local result, err = program:run("test_json")
			if err then error(err) end
			assert(result == "module_loaded", "Should load json module")

			return {result = result, success = true}
		end
	`, "test", "test_with_modules")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_with_modules")
	require.NoError(t, err)

	assert.NotNil(t, result)
}

func TestEvalModule_WithImports(t *testing.T) {
	vm, runner, ctx := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_with_imports()
			local eval = require("eval")

			local program, err = eval.compile(
				"function test_lib() local lib = require('helper'); return lib.hello() end",
				"test_lib",
				{imports = {helper = "app:mylib"}}
			)
			if err then error(err) end

			local result, err = program:run("test_lib")
			if err then error(err) end
			assert(result == "world", "Should call library function")

			return {result = result, success = true}
		end
	`, "test", "test_with_imports")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_with_imports")
	require.NoError(t, err)

	assert.NotNil(t, result)
}

func TestEvalModule_SingleMethod(t *testing.T) {
	vm, runner, ctx := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_single_method()
			local eval = require("eval")

			local program, err = eval.compile("function main() return 'single_method' end", "main")
			if err then error(err) end

			local result, err = program:run("main")
			if err then error(err) end
			assert(result == "single_method", "Should execute the specified method")

			return {result = result, success = true}
		end
	`, "test", "test_single_method")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_single_method")
	require.NoError(t, err)

	assert.NotNil(t, result)
}

func TestEvalModule_MissingMethod(t *testing.T) {
	vm, runner, ctx := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_missing_method()
			local eval = require("eval")

			local program, err = eval.compile("function main() end", "")

			assert(program == nil, "Program should be nil")
			assert(err ~= nil, "Error should not be nil")
			assert(string.find(err, "method is required"), "Error should mention method is required")

			return {success = true}
		end
	`, "test", "test_missing_method")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_missing_method")
	require.NoError(t, err)

	assert.NotNil(t, result)
}

func TestEvalModule_InvalidSource(t *testing.T) {
	vm, runner, ctx := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_invalid_source()
			local eval = require("eval")

			local program, err = eval.compile("function main( invalid syntax", "main")

			assert(program == nil, "Program should be nil")
			assert(err ~= nil, "Error should not be nil")
			assert(string.find(err, "parse error") or string.find(err, "compile error"), "Error should mention parse or compile error")

			return {success = true}
		end
	`, "test", "test_invalid_source")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_invalid_source")
	require.NoError(t, err)

	assert.NotNil(t, result)
}

func TestEvalModule_SetTimeout(t *testing.T) {
	vm, runner, ctx := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_set_timeout()
			local eval = require("eval")

			local program, err = eval.compile("function main() return 'ok' end", "main")
			if err then error(err) end

			program:set_timeout("5s")

			local result, err = program:run("main")
			if err then error(err) end
			assert(result == "ok", "Should execute with timeout")

			return {success = true}
		end
	`, "test", "test_set_timeout")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_set_timeout")
	require.NoError(t, err)

	assert.NotNil(t, result)
}

func TestEvalModule_WithConfig(t *testing.T) {
	vm, runner, ctx := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_with_config()
			local eval = require("eval")

			local program, err = eval.compile(
				"function process() local json = require('json'); local lib = require('helper'); return lib.hello() .. ':' .. json.test end",
				"process",
				{
					modules = {"json"},
					imports = {helper = "app:mylib"}
				}
			)
			if err then error(err) end

			local result, err = program:run("process")
			if err then error(err) end
			assert(result == "world:module_loaded", "Should use both modules and imports")

			return {success = true}
		end
	`, "test", "test_with_config")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_with_config")
	require.NoError(t, err)

	assert.NotNil(t, result)
}

func TestEvalModule_TimeoutEnforcement(t *testing.T) {
	vm, runner, ctx := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_timeout_enforcement()
			local eval = require("eval")

			local program, err = eval.compile([[
				function sleep_long()
					local count = 0
					while count < 1000000 do
						coroutine.yield()
						count = count + 1
					end
					return "completed"
				end
			]], "sleep_long")
			if err then error(err) end

			program:set_timeout("10ms")

			local result, err = program:run("sleep_long")

			assert(result == nil, "Should not get result on timeout")
			assert(err ~= nil, "Should get error on timeout")
			assert(string.find(err, "deadline exceeded") or string.find(err, "context deadline"),
				   "Error should indicate timeout: " .. tostring(err))

			return {success = true}
		end
	`, "test", "test_timeout_enforcement")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_timeout_enforcement")
	require.NoError(t, err)

	assert.NotNil(t, result)
}

func TestEvalModule_InfiniteLoopWithYield(t *testing.T) {
	vm, runner, ctx := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_infinite_loop_with_yield()
			local eval = require("eval")

			local program, err = eval.compile([[
				function loop_forever()
					while true do
						coroutine.yield()
					end
				end
			]], "loop_forever")
			if err then error(err) end

			program:set_timeout("100ms")

			local result, err = program:run("loop_forever")

			assert(result == nil, "Should not get result on timeout")
			assert(err ~= nil, "Should get error on timeout")
			assert(string.find(err, "deadline exceeded") or string.find(err, "context deadline"),
				   "Error should indicate timeout: " .. tostring(err))

			return {success = true}
		end
	`, "test", "test_infinite_loop_with_yield")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_infinite_loop_with_yield")
	require.NoError(t, err)

	assert.NotNil(t, result)
}

func TestEvalModule_TableArguments(t *testing.T) {
	vm, runner, ctx := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_table_arguments()
			local eval = require("eval")

			local program, err = eval.compile([[
				function process_table(data)
					return data.name .. ":" .. tostring(data.value * 2)
				end
			]], "process_table")
			if err then error(err) end

			local input = {name = "test", value = 21}
			local result, err = program:run("process_table", input)
			if err then error(err) end

			assert(result == "test:42", "Should process table fields, got: " .. tostring(result))

			return {success = true}
		end
	`, "test", "test_table_arguments")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_table_arguments")
	require.NoError(t, err)

	assert.NotNil(t, result)
}

func TestEvalModule_TableReturn(t *testing.T) {
	vm, runner, ctx := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_table_return()
			local eval = require("eval")

			local program, err = eval.compile([[
				function make_table()
					return {result = "ok", count = 5, nested = {value = 42}}
				end
			]], "make_table")
			if err then error(err) end

			local result, err = program:run("make_table")
			if err then error(err) end

			assert(type(result) == "table", "Should return table")
			assert(result.result == "ok", "Should have result field")
			assert(result.count == 5, "Should have count field")
			assert(type(result.nested) == "table", "Should have nested table")
			assert(result.nested.value == 42, "Should have nested value")

			return {success = true}
		end
	`, "test", "test_table_return")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_table_return")
	require.NoError(t, err)

	assert.NotNil(t, result)
}

func TestEvalModule_RuntimeError(t *testing.T) {
	vm, runner, ctx := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_runtime_error()
			local eval = require("eval")

			local program, err = eval.compile([[
				function fail()
					error("custom error message")
				end
			]], "fail")
			if err then error(err) end

			local result, err = program:run("fail")

			assert(result == nil, "Should not get result on error")
			assert(err ~= nil, "Should get error")
			assert(string.find(err, "custom error message"),
				   "Error should contain custom message: " .. tostring(err))

			return {success = true}
		end
	`, "test", "test_runtime_error")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_runtime_error")
	require.NoError(t, err)

	assert.NotNil(t, result)
}

func TestEvalModule_MultipleArguments(t *testing.T) {
	vm, runner, ctx := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_multiple_arguments()
			local eval = require("eval")

			local program, err = eval.compile([[
				function combine(a, b, c, d)
					return tostring(a) .. ":" .. b .. ":" .. tostring(c) .. ":" .. tostring(d)
				end
			]], "combine")
			if err then error(err) end

			local result1, err1 = program:run("combine", 10, "hello", true, {x = 5})
			if err1 then error(err1) end
			assert(string.find(result1, "10:hello:true:table"), "First run should combine args")

			local result2, err2 = program:run("combine", 20, "world", false, {y = 10})
			if err2 then error(err2) end
			assert(string.find(result2, "20:world:false:table"), "Second run should combine different args")

			return {success = true}
		end
	`, "test", "test_multiple_arguments")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_multiple_arguments")
	require.NoError(t, err)

	assert.NotNil(t, result)
}

func TestEvalModule_DeadlockDetection(t *testing.T) {
	vm, runner, ctx := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_deadlock_detection()
			local eval = require("eval")

			local program, err = eval.compile([[
				function deadlock_test()
					local channel = require("channel")

					local ch = channel.new(0)

					coroutine.spawn(function()
						ch:receive()
					end)

					return "done"
				end
			]], "deadlock_test", {modules = {"channel"}})
			if err then error(err) end

			local result, err = program:run("deadlock_test")

			assert(result == nil, "Should not get result on deadlock")
			assert(err ~= nil, "Should get error on deadlock")
			assert(string.find(err, "deadlock") or string.find(err, "Deadlock") or string.find(err, "orphaned coroutines"),
				   "Error should indicate deadlock: " .. tostring(err))

			return {success = true}
		end
	`, "test", "test_deadlock_detection")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_deadlock_detection")
	require.NoError(t, err)

	assert.NotNil(t, result)
}

func TestEvalModule_CPUBoundTimeout(t *testing.T) {
	vm, runner, ctx := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_cpu_bound_timeout()
			local eval = require("eval")

			local program, err = eval.compile([[
				function cpu_loop()
					local x = 0
					while true do
						x = x + 1
					end
					return x
				end
			]], "cpu_loop")
			if err then error(err) end

			program:set_timeout("100ms")

			local result, err = program:run("cpu_loop")

			assert(result == nil, "Should not get result on timeout")
			assert(err ~= nil, "Should get error on timeout")
			assert(string.find(err, "deadline exceeded") or string.find(err, "context deadline") or string.find(err, "canceled"),
				   "Error should indicate timeout/cancellation: " .. tostring(err))

			return {success = true}
		end
	`, "test", "test_cpu_bound_timeout")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_cpu_bound_timeout")
	require.NoError(t, err)

	assert.NotNil(t, result)
}
