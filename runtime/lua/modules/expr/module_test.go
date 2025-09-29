package expr

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/system/payload/lua"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	luavm "github.com/yuin/gopher-lua"
	"go.uber.org/zap/zaptest"
)

// setupTestEnvironment creates a test environment with expr module
func setupTestEnvironment(t *testing.T, opts ...Option) (*engine.CoroutineVM, *luavm.LState, engine.UnitOfWork, *engine.Runner) {
	logger := zaptest.NewLogger(t)

	// Create the expr module with options
	module := NewExprModule(opts...)

	// Create a VM with coroutine support
	vm, err := engine.NewCVM(logger)
	require.NoError(t, err)

	// Get the Lua state
	L := vm.State()

	// Register the expr module
	L.PreloadModule(module.Name(), module.Loader)

	// Create a runner with the coroutine layer
	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))

	// Create a UOW for resource management
	uw, ctx := runner.InitUnitOfWork(context.Background())

	// Set the context in the Lua state
	L.SetContext(ctx)

	return vm, L, uw, runner
}

func TestExprModule_CompileAndRun(t *testing.T) {
	vm, L, uw, runner := setupTestEnvironment(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Import test function for compile and run
	err := vm.Import(`
		function test_compile_and_run()
			local expr = require("expr")
			
			-- Test basic compile
			local program, err = expr.compile("2 + 3 * 4")
			if err then error(err) end
			assert(program ~= nil, "Program should not be nil")
			assert(type(program) == "userdata", "Program should be userdata")
			
			-- Test program:run() method
			local result1, err1 = program:run()
			if err1 then error(err1) end
			assert(result1 == 14, "2 + 3 * 4 should equal 14, got: " .. tostring(result1))
			
			-- Test running same program multiple times
			local result2, err2 = program:run()
			if err2 then error(err2) end
			assert(result2 == 14, "Second run should also equal 14")
			
			-- Test program with environment
			local program_env, err3 = expr.compile("price * quantity")
			if err3 then error(err3) end
			
			local result3, err4 = program_env:run({price = 10, quantity = 3})
			if err4 then error(err4) end
			assert(result3 == 30, "10 * 3 should equal 30, got: " .. tostring(result3))
			
			-- Test same program with different environments
			local result4, err5 = program_env:run({price = 5, quantity = 4})
			if err5 then error(err5) end
			assert(result4 == 20, "5 * 4 should equal 20, got: " .. tostring(result4))
			
			return {
				basic = result1,
				with_env1 = result3,
				with_env2 = result4,
				success = true
			}
		end
	`, "test", "test_compile_and_run")
	require.NoError(t, err, "Failed to import test function")

	// Execute the function
	result, err := runner.Execute(L.Context(), "test_compile_and_run")
	require.NoError(t, err, "Lua execution failed")

	resultMap := lua.ToGoAny(result).(map[string]interface{})

	assert.Equal(t, true, resultMap["success"], "Test should succeed")
	assert.Equal(t, float64(14), resultMap["basic"], "Basic arithmetic should work")
	assert.Equal(t, float64(30), resultMap["with_env1"], "First environment should work")
	assert.Equal(t, float64(20), resultMap["with_env2"], "Second environment should work")
}

func TestExprModule_ProgramBuiltinFunctions(t *testing.T) {
	vm, L, uw, runner := setupTestEnvironment(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Import test function for built-in functions with programs
	err := vm.Import(`
		function test_program_builtins()
			local expr = require("expr")
			
			-- Compile programs with built-in functions
			local all_prog, err1 = expr.compile("all(numbers, {# > 0})")
			if err1 then error(err1) end
			
			local filter_prog, err2 = expr.compile("filter(numbers, {# > 3})")
			if err2 then error(err2) end
			
			local map_prog, err3 = expr.compile("map(numbers, {# * 2})")
			if err3 then error(err3) end
			
			local max_prog, err4 = expr.compile("max(a, b)")
			if err4 then error(err4) end
			
			-- Test with different data sets
			local env1 = {numbers = {1, 2, 3, 4, 5}}
			local env2 = {numbers = {-1, 0, 1}}
			local env3 = {a = 10, b = 5}
			local env4 = {a = 3, b = 8}
			
			-- Run tests
			local all_result1, err5 = all_prog:run(env1)
			if err5 then error(err5) end
			
			local all_result2, err6 = all_prog:run(env2)
			if err6 then error(err6) end
			
			local filter_result, err7 = filter_prog:run(env1)
			if err7 then error(err7) end
			
			local map_result, err8 = map_prog:run(env1)
			if err8 then error(err8) end
			
			local max_result1, err9 = max_prog:run(env3)
			if err9 then error(err9) end
			
			local max_result2, err10 = max_prog:run(env4)
			if err10 then error(err10) end
			
			return {
				all_positive = all_result1,
				all_mixed = all_result2,
				filtered = filter_result,
				mapped = map_result,
				max1 = max_result1,
				max2 = max_result2
			}
		end
	`, "test", "test_program_builtins")
	require.NoError(t, err, "Failed to import test function")

	// Execute the function
	result, err := runner.Execute(L.Context(), "test_program_builtins")
	require.NoError(t, err, "Lua execution failed")

	resultMap := lua.ToGoAny(result).(map[string]interface{})

	assert.Equal(t, true, resultMap["all_positive"], "All positive numbers should be > 0")
	assert.Equal(t, false, resultMap["all_mixed"], "Mixed numbers should not all be > 0")
	assert.Equal(t, []interface{}{float64(4), float64(5)}, resultMap["filtered"], "Filter should return [4, 5]")
	assert.Equal(t, []interface{}{float64(2), float64(4), float64(6), float64(8), float64(10)}, resultMap["mapped"], "Map should double all numbers")
	assert.Equal(t, float64(10), resultMap["max1"], "Max of 10 and 5 should be 10")
	assert.Equal(t, float64(8), resultMap["max2"], "Max of 3 and 8 should be 8")
}

func TestExprModule_ErrorHandling(t *testing.T) {
	vm, L, uw, runner := setupTestEnvironment(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Import test function for error handling
	err := vm.Import(`
		function test_error_handling()
			local expr = require("expr")
			
			-- Test empty expression compile
			local program1, err1 = expr.compile("")
			assert(program1 == nil, "Empty expression should return nil")
			assert(err1 ~= nil, "Empty expression should return error")
			
			-- Test invalid syntax compile
			local program2, err2 = expr.compile("2 +")
			assert(program2 == nil, "Invalid syntax should return nil")
			assert(err2 ~= nil, "Invalid syntax should return error")
			
			-- Test valid compile but runtime error
			local program3, err3 = expr.compile("undefined_var")
			assert(program3 ~= nil, "Valid syntax should compile")
			assert(err3 == nil, "Valid syntax should not error on compile")
			
			-- Test runtime error
			local result3, err4 = program3:run()
			assert(result3 == nil, "Undefined variable should return nil")
			assert(err4 ~= nil, "Undefined variable should return runtime error")
			
			-- Test type mismatch at runtime
			local program4, err5 = expr.compile("x + y")
			assert(program4 ~= nil, "Valid syntax should compile")
			assert(err5 == nil, "Valid syntax should not error on compile")
			
			local result4, err6 = program4:run({x = "string", y = 42})
			assert(result4 == nil, "Type mismatch should return nil")
			assert(err6 ~= nil, "Type mismatch should return error")
			
			-- Test invalid program object (can't test easily, but verify method exists)
			-- This would test CheckProgram validation in real scenarios
			
			return {success = true}
		end
	`, "test", "test_error_handling")
	require.NoError(t, err, "Failed to import test function")

	// Execute the function
	result, err := runner.Execute(L.Context(), "test_error_handling")
	require.NoError(t, err, "Lua execution failed")

	resultMap := lua.ToGoAny(result).(map[string]interface{})
	assert.Equal(t, true, resultMap["success"], "Error handling test should succeed")
}

func TestExprModule_CachingBehavior(t *testing.T) {
	vm, L, uw, runner := setupTestEnvironment(t, WithCapacity(100))
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Import test function that compares eval vs compile caching
	err := vm.Import(`
		function test_caching_behavior()
			local expr = require("expr")
			
			-- Test eval caching (should cache)
			local expr_text = "a + b * c"
			local env = {a = 1, b = 2, c = 3}
			
			local eval_results = {}
			for i = 1, 5 do
				local result, err = expr.eval(expr_text, env)
				if err then error(err) end
				table.insert(eval_results, result)
			end
			
			-- Test compile (should not cache, each compile is independent)
			local compile_results = {}
			for i = 1, 3 do
				local program, err = expr.compile(expr_text)
				if err then error(err) end
				
				local result, err2 = program:run(env)
				if err2 then error(err2) end
				table.insert(compile_results, result)
			end
			
			-- All results should be the same regardless of method
			for i = 2, #eval_results do
				assert(eval_results[i] == eval_results[1], "All eval results should be equal")
			end
			
			for i = 2, #compile_results do
				assert(compile_results[i] == compile_results[1], "All compile results should be equal")
			end
			
			assert(eval_results[1] == compile_results[1], "Eval and compile should give same result")
			
			-- Test that compile allows different programs with same expression
			local program1, err1 = expr.compile("x * 2")
			if err1 then error(err1) end
			
			local program2, err2 = expr.compile("x * 2")
			if err2 then error(err2) end
			
			-- Both should work independently
			local result1, err3 = program1:run({x = 5})
			if err3 then error(err3) end
			
			local result2, err4 = program2:run({x = 10})
			if err4 then error(err4) end
			
			return {
				eval_result = eval_results[1],
				compile_result = compile_results[1],
				independent1 = result1,
				independent2 = result2,
				success = true
			}
		end
	`, "test", "test_caching_behavior")
	require.NoError(t, err, "Failed to import test function")

	// Execute the function
	result, err := runner.Execute(L.Context(), "test_caching_behavior")
	require.NoError(t, err, "Lua execution failed")

	resultMap := lua.ToGoAny(result).(map[string]interface{})
	assert.Equal(t, true, resultMap["success"], "Caching behavior test should succeed")
	assert.Equal(t, float64(7), resultMap["eval_result"], "1 + 2 * 3 should equal 7")
	assert.Equal(t, float64(7), resultMap["compile_result"], "Compile should give same result as eval")
	assert.Equal(t, float64(10), resultMap["independent1"], "5 * 2 should equal 10")
	assert.Equal(t, float64(20), resultMap["independent2"], "10 * 2 should equal 20")
}

func TestExprModule_ComplexProgramUsage(t *testing.T) {
	vm, L, uw, runner := setupTestEnvironment(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Import test function for complex usage patterns
	err := vm.Import(`
		function test_complex_usage()
			local expr = require("expr")
			
			-- Test nested objects with programs
			local user_check, err1 = expr.compile("user.profile.age >= min_age && user.active")
			if err1 then error(err1) end
			
			local users = {
				{profile = {age = 25}, active = true},
				{profile = {age = 17}, active = true},
				{profile = {age = 30}, active = false}
			}
			
			local results = {}
			for i, user in ipairs(users) do
				local result, err = user_check:run({user = user, min_age = 18})
				if err then error(err) end
				table.insert(results, result)
			end
			
			-- Test array processing with programs
			local discount_calc, err2 = expr.compile("map(items, {.price * (1 - discount)})")
			if err2 then error(err2) end
			
			local order1 = {
				items = {{price = 100}, {price = 50}, {price = 75}},
				discount = 0.1
			}
			
			local order2 = {
				items = {{price = 200}, {price = 80}},
				discount = 0.2
			}
			
			local discounted1, err3 = discount_calc:run(order1)
			if err3 then error(err3) end
			
			local discounted2, err4 = discount_calc:run(order2)
			if err4 then error(err4) end
			
			-- Test string operations
			local name_formatter, err5 = expr.compile('upper(firstName) + " " + upper(lastName)')
			if err5 then error(err5) end
			
			local name1, err6 = name_formatter:run({firstName = "john", lastName = "doe"})
			if err6 then error(err6) end
			
			local name2, err7 = name_formatter:run({firstName = "jane", lastName = "smith"})
			if err7 then error(err7) end
			
			return {
				user_results = results,
				discounted1 = discounted1,
				discounted2 = discounted2,
				name1 = name1,
				name2 = name2
			}
		end
	`, "test", "test_complex_usage")
	require.NoError(t, err, "Failed to import test function")

	// Execute the function
	result, err := runner.Execute(L.Context(), "test_complex_usage")
	require.NoError(t, err, "Lua execution failed")

	resultMap := lua.ToGoAny(result).(map[string]interface{})

	// Check user validation results
	userResults := resultMap["user_results"].([]interface{})
	assert.Equal(t, true, userResults[0], "First user should pass (age 25, active)")
	assert.Equal(t, false, userResults[1], "Second user should fail (age 17)")
	assert.Equal(t, false, userResults[2], "Third user should fail (not active)")

	// Check discount calculations
	discounted1 := resultMap["discounted1"].([]interface{})
	assert.Equal(t, float64(90), discounted1[0], "100 * 0.9 = 90")
	assert.Equal(t, float64(45), discounted1[1], "50 * 0.9 = 45")
	assert.Equal(t, float64(67.5), discounted1[2], "75 * 0.9 = 67.5")

	discounted2 := resultMap["discounted2"].([]interface{})
	assert.Equal(t, float64(160), discounted2[0], "200 * 0.8 = 160")
	assert.Equal(t, float64(64), discounted2[1], "80 * 0.8 = 64")

	// Check string formatting
	assert.Equal(t, "JOHN DOE", resultMap["name1"], "Name formatting should work")
	assert.Equal(t, "JANE SMITH", resultMap["name2"], "Name formatting should work")
}

func TestExprModule_EvalStillWorks(t *testing.T) {
	vm, L, uw, runner := setupTestEnvironment(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Import test function to ensure eval still works as before
	err := vm.Import(`
		function test_eval_compatibility()
			local expr = require("expr")
			
			-- Test basic eval (should still work)
			local result1, err1 = expr.eval("2 + 3 * 4")
			if err1 then error(err1) end
			
			-- Test eval with environment
			local result2, err2 = expr.eval("price * quantity", {
				price = 10.5,
				quantity = 3
			})
			if err2 then error(err2) end
			
			-- Test eval with built-ins
			local result3, err3 = expr.eval("all(numbers, {# > 0})", {
				numbers = {1, 2, 3, 4, 5}
			})
			if err3 then error(err3) end
			
			return {
				arithmetic = result1,
				variables = result2,
				builtin = result3
			}
		end
	`, "test", "test_eval_compatibility")
	require.NoError(t, err, "Failed to import test function")

	// Execute the function
	result, err := runner.Execute(L.Context(), "test_eval_compatibility")
	require.NoError(t, err, "Lua execution failed")

	resultMap := lua.ToGoAny(result).(map[string]interface{})

	assert.Equal(t, float64(14), resultMap["arithmetic"], "2 + 3 * 4 should equal 14")
	assert.Equal(t, float64(31.5), resultMap["variables"], "10.5 * 3 should equal 31.5")
	assert.Equal(t, true, resultMap["builtin"], "All numbers should be > 0")
}

func TestExprModule_CustomCapacity(t *testing.T) {
	// Test that custom capacity option works
	module := NewExprModule(WithCapacity(50))
	assert.NotNil(t, module, "Module should be created successfully")
	assert.NotNil(t, module.cache, "Cache should be initialized")

	// Verify module can be closed
	module.Close()
}

func TestExprModule_ModuleName(t *testing.T) {
	module := NewExprModule()
	assert.Equal(t, "expr", module.Name(), "Module name should be 'expr'")
}
