package builder

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap/zaptest"
)

// setupLuaWithBuilder sets up a Lua VM with the builder module loaded
func setupLuaWithBuilder(t *testing.T) (*engine.CoroutineVM, *lua.LState, *engine.Runner) {
	logger := zaptest.NewLogger(t)

	// Create a VM with coroutine support
	vm, err := engine.NewCVM(logger)
	require.NoError(t, err, "Failed to create VM")

	// Get the Lua state
	L := vm.State()

	// Create the SQL module table
	sqlMod := L.CreateTable(0, 5)

	// Register the builder module
	RegisterBuilderModule(L, sqlMod)

	// Set the SQL module in _G
	L.SetGlobal("sql", sqlMod)

	// Create a runner with the coroutine layer
	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))

	// Create a context for the state
	ctx := context.Background()
	L.SetContext(ctx)

	return vm, L, runner
}

// TestCaseBuilderBasic tests basic CASE expression construction
func TestCaseBuilderBasic(t *testing.T) {
	vm, L, runner := setupLuaWithBuilder(t)
	defer vm.Close()

	// Import test script
	script := `
		function test_case_builder()
			-- Create a simple CASE expression
			local case = sql.builder.case("status")
				:when("active", "User is active")
				:when("pending", "User is pending")
				:else_("User is inactive")
			
			-- Get the SQL and args
			local sql_str, args = case:to_sql()
			
			return {
				sql = sql_str,
				args = args,
				str_representation = tostring(case)
			}
		end
	`

	err := vm.Import(script, "test", "test_case_builder")
	require.NoError(t, err, "Failed to import script")

	// Execute the function
	result, err := runner.Execute(L.Context(), "test_case_builder")
	require.NoError(t, err, "Lua execution failed")

	// Verify results
	resultTable, ok := result.(*lua.LTable)
	require.True(t, ok, "Expected table result")

	// Get SQL
	sqlStr := resultTable.RawGetString("sql").(lua.LString)
	assert.Contains(t, string(sqlStr), "CASE", "SQL should contain CASE keyword")
	assert.Contains(t, string(sqlStr), "WHEN", "SQL should contain WHEN keyword")
	assert.Contains(t, string(sqlStr), "ELSE", "SQL should contain ELSE keyword")
	assert.Contains(t, string(sqlStr), "END", "SQL should contain END keyword")

	// Verify args table exists and has 3 arguments
	argsTable := resultTable.RawGetString("args").(*lua.LTable)
	assert.Equal(t, 3, argsTable.Len(), "Should have 3 args")

	// Ensure string representation is also correct
	strRepr := resultTable.RawGetString("str_representation").(lua.LString)
	assert.Contains(t, string(strRepr), "CaseBuilder", "String representation should identify as CaseBuilder")
}

// TestCaseBuilderSimple tests a simple CASE expression without a value
func TestCaseBuilderSimple(t *testing.T) {
	vm, L, runner := setupLuaWithBuilder(t)
	defer vm.Close()

	// Import test script
	script := `
		function test_simple_case()
			-- Create a simple CASE expression without a value
			local case = sql.builder.case()
				:when(sql.builder.eq({age = 18}), "Just became an adult")
				:when(sql.builder.gt({age = 65}), "Senior citizen")
				:else_("Regular adult")
			
			-- Get the SQL and args
			local sql_str, args = case:to_sql()
			
			return {
				sql = sql_str,
				args = args
			}
		end
	`

	err := vm.Import(script, "test", "test_simple_case")
	require.NoError(t, err, "Failed to import script")

	// Execute the function
	result, err := runner.Execute(L.Context(), "test_simple_case")
	require.NoError(t, err, "Lua execution failed")

	// Verify results
	resultTable, ok := result.(*lua.LTable)
	require.True(t, ok, "Expected table result")

	// Get SQL and check the structure
	sqlStr := resultTable.RawGetString("sql").(lua.LString)
	assert.Contains(t, string(sqlStr), "CASE", "SQL should contain CASE keyword")
	assert.Contains(t, string(sqlStr), "WHEN", "SQL should contain WHEN keyword")
	assert.Contains(t, string(sqlStr), "ELSE", "SQL should contain ELSE keyword")
	assert.Contains(t, string(sqlStr), "END", "SQL should contain END keyword")
	assert.Contains(t, string(sqlStr), "age = ?", "SQL should contain condition")
}

// TestCaseWithOtherExpressions tests CASE with other expressions
func TestCaseWithOtherExpressions(t *testing.T) {
	vm, L, runner := setupLuaWithBuilder(t)
	defer vm.Close()

	// Import test script
	script := `
		function test_case_with_expressions()
			-- Create a select using CASE expression
			local select = sql.builder.select(
				"id", 
				"name",
				sql.builder.case("type")
					:when("admin", "Administrator")
					:when("user", "Regular User")
					:else_("Unknown")
					:to_sql()
			)
			:from("users")
			
			-- Get the SQL
			local sql_str, args = select:to_sql()
			
			return {
				sql = sql_str,
				args = args
			}
		end
	`

	err := vm.Import(script, "test", "test_case_with_expressions")
	require.NoError(t, err, "Failed to import script")

	// Execute the function
	result, err := runner.Execute(L.Context(), "test_case_with_expressions")
	require.NoError(t, err, "Lua execution failed")

	// Verify results
	resultTable, ok := result.(*lua.LTable)
	require.True(t, ok, "Expected table result")

	// Check SQL
	sqlStr := resultTable.RawGetString("sql").(lua.LString)
	assert.Contains(t, string(sqlStr), "SELECT id, name, CASE", "SQL should contain SELECT with CASE")
	assert.Contains(t, string(sqlStr), "FROM users", "SQL should include FROM clause")
}

// TestCaseComplex tests more complex CASE expressions
func TestCaseComplex(t *testing.T) {
	vm, L, runner := setupLuaWithBuilder(t)
	defer vm.Close()

	// Import test script
	script := `
		function test_complex_case()
			-- Create a more complex CASE expression with multiple types of conditions
			local case = sql.builder.case()
				:when(sql.builder.eq({is_admin = true}), "Admin")
				:when(sql.builder.and({
					sql.builder.eq({is_moderator = true}),
					sql.builder.gt({reputation = 1000})
				}), "Super Moderator")
				:when(sql.builder.eq({is_moderator = true}), "Moderator")
				:else_("Regular User")
			
			-- Get the SQL and args
			local sql_str, args = case:to_sql()
			
			return {
				sql = sql_str,
				args_count = #args
			}
		end
	`

	err := vm.Import(script, "test", "test_complex_case")
	require.NoError(t, err, "Failed to import script")

	// Execute the function
	result, err := runner.Execute(L.Context(), "test_complex_case")
	require.NoError(t, err, "Lua execution failed")

	// Verify results
	resultTable, ok := result.(*lua.LTable)
	require.True(t, ok, "Expected table result")

	// Check SQL and args count (we expect several placeholders)
	sqlStr := resultTable.RawGetString("sql").(lua.LString)
	argsCount := resultTable.RawGetString("args_count").(lua.LNumber)

	assert.Contains(t, string(sqlStr), "CASE", "SQL should contain CASE keyword")
	assert.Contains(t, string(sqlStr), "AND", "SQL should contain AND for combined conditions")
	assert.GreaterOrEqual(t, int(argsCount), 4, "Should have at least 4 arguments")
}
