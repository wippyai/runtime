package builder

import (
	"context"
	ctxapi "github.com/wippyai/runtime/api/context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/coroutine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap/zaptest"
)

// setupLuaWithInsertModule sets up a Lua state with the InsertBuilder registered
func setupLuaWithInsertModule(t *testing.T) (*engine.CoroutineVM, *engine.Runner, context.Context) {
	logger := zaptest.NewLogger(t)

	// Create a VM with coroutine support
	vm, err := engine.NewCVM(logger)
	require.NoError(t, err)

	// Get the Lua state
	L := vm.State()

	// Create a table and register Insert builder
	modTable := L.CreateTable(0, 20)
	registerInsertBuilderType(L)
	registerSqlizerMetatable(L) // For Expr etc.

	// Register the official placeholder formats from module.go
	registerPlaceholderFormats(L, modTable)

	// Add the INSERT builder
	modTable.RawSetString("insert", L.NewFunction(builderInsert))

	// Create and set the SQL NULL value
	nullUserData := L.NewUserData()
	nullUserData.Value = "SQL_NULL"
	L.SetGlobal("SQL_NULL", nullUserData)

	// Add the table to global state for testing
	L.SetGlobal("builder", modTable)

	// Create a runner with the coroutine layer
	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))

	// Create a context for execution
	ctx := ctxapi.NewRootContext()

	// Initialize a unit of work with the context
	_, luaCtx := runner.InitUnitOfWork(ctx)

	// Set the context in the Lua state
	L.SetContext(luaCtx)

	return vm, runner, luaCtx
}

// Helper function to register placeholder formats for testing
//
//nolint:unused // to be used in tests
func registerTestPlaceholderFormats(l *lua.LState, modTable *lua.LTable) {
	// Create userdata for each placeholder format
	questionFormat := l.NewUserData()
	questionFormat.Value = questionPlaceholderFormat{}
	modTable.RawSetString("question", questionFormat)

	dollarFormat := l.NewUserData()
	dollarFormat.Value = dollarPlaceholderFormat{}
	modTable.RawSetString("dollar", dollarFormat)
}

// Simple implementations for placeholder formats for testing purposes

//nolint:unused // ok for now
type questionPlaceholderFormat struct{}

//nolint:unused // ok for now
type dollarPlaceholderFormat struct{}

//nolint:unused // ok for now
func (questionPlaceholderFormat) ReplacePlaceholders(sql string) (string, error) {
	return sql, nil
}

//nolint:unused // ok for now
func (dollarPlaceholderFormat) ReplacePlaceholders(sql string) (string, error) {
	return sql, nil // Just a stub for testing
}

// TestInsertBasic tests basic InsertBuilder functionality
func TestInsertBasic(t *testing.T) {
	vm, runner, ctx := setupLuaWithInsertModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_insert_basic()
			-- Create a basic INSERT builder
			local insert = builder.insert("users")
			
			-- Add a placeholder value to make Squirrel happy
			-- An empty INSERT isn't valid SQL in many databases
			insert = insert:columns("id"):values(1)
			
			-- Convert to SQL
			local sql, args = insert:to_sql()
			
			return {
				sql = sql,
				args_count = #args
			}
		end
	`, "test", "test_insert_basic")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_insert_basic")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output
	sql := resultTable.RawGetString("sql").String()
	assert.Contains(t, sql, "INSERT INTO users")
	assert.Equal(t, 1, int(resultTable.RawGetString("args_count").(lua.LNumber)))
}

// TestInsertColumns tests setting columns and values
func TestInsertColumns(t *testing.T) {
	vm, runner, ctx := setupLuaWithInsertModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_insert_columns()
			-- Create INSERT builder with columns and values
			local insert = builder.insert("users")
				:columns("id", "name", "email")
				:values(1, "John", "john@example.com")
			
			-- Convert to SQL
			local sql, args = insert:to_sql()
			
			return {
				sql = sql,
				args = args,
				args_count = #args
			}
		end
	`, "test", "test_insert_columns")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_insert_columns")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output
	sql := resultTable.RawGetString("sql").String()
	assert.Contains(t, sql, "INSERT INTO users")
	assert.Contains(t, sql, "(id,name,email)")
	assert.Contains(t, sql, "VALUES (?,?,?)")

	// Verify args
	argsCount := int(resultTable.RawGetString("args_count").(lua.LNumber))
	assert.Equal(t, 3, argsCount)

	args := resultTable.RawGetString("args").(*lua.LTable)
	assert.Equal(t, float64(1), float64(args.RawGetInt(1).(lua.LNumber)))
	assert.Equal(t, "John", string(args.RawGetInt(2).(lua.LString)))
	assert.Equal(t, "john@example.com", string(args.RawGetInt(3).(lua.LString)))
}

// TestInsertSetMap tests using the set_map method
func TestInsertSetMap(t *testing.T) {
	vm, runner, ctx := setupLuaWithInsertModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_insert_set_map()
			-- Create INSERT builder with set_map
			local insert = builder.insert("users")
				:set_map({
					id = 1,
					name = "John",
					email = "john@example.com"
				})
			
			-- Convert to SQL
			local sql, args = insert:to_sql()
			
			-- The columns in set_map might be in any order, so we'll check them separately
			local has_id = string.find(sql, "id") ~= nil
			local has_name = string.find(sql, "name") ~= nil
			local has_email = string.find(sql, "email") ~= nil
			
			return {
				sql = sql,
				args = args,
				args_count = #args,
				has_all_columns = has_id and has_name and has_email
			}
		end
	`, "test", "test_insert_set_map")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_insert_set_map")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output
	sql := resultTable.RawGetString("sql").String()
	assert.Contains(t, sql, "INSERT INTO users")
	assert.Contains(t, sql, "VALUES")

	// Verify all columns are present
	hasAllColumns := resultTable.RawGetString("has_all_columns").(lua.LBool)
	assert.True(t, bool(hasAllColumns))

	// Verify args
	argsCount := int(resultTable.RawGetString("args_count").(lua.LNumber))
	assert.Equal(t, 3, argsCount)
}

// TestInsertMultipleRows tests inserting multiple rows
func TestInsertMultipleRows(t *testing.T) {
	vm, runner, ctx := setupLuaWithInsertModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_insert_multiple_rows()
			-- Create INSERT builder with multiple rows
			local insert = builder.insert("users")
				:columns("id", "name", "email")
				:values(1, "John", "john@example.com")
				:values(2, "Jane", "jane@example.com")
			
			-- Convert to SQL
			local sql, args = insert:to_sql()
			
			return {
				sql = sql,
				args = args,
				args_count = #args
			}
		end
	`, "test", "test_insert_multiple_rows")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_insert_multiple_rows")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output
	sql := resultTable.RawGetString("sql").String()
	assert.Contains(t, sql, "INSERT INTO users")
	assert.Contains(t, sql, "VALUES (?,?,?),(?,?,?)")

	// Verify args
	argsCount := int(resultTable.RawGetString("args_count").(lua.LNumber))
	assert.Equal(t, 6, argsCount) // 2 rows × 3 columns
}

// TestInsertOptions tests using options like IGNORE
func TestInsertOptions(t *testing.T) {
	vm, runner, ctx := setupLuaWithInsertModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_insert_options()
			-- Create INSERT builder with IGNORE option
			local insert = builder.insert("users")
				:options("IGNORE")
				:columns("id", "name")
				:values(1, "John")
			
			-- Convert to SQL
			local sql, args = insert:to_sql()
			
			return {
				sql = sql,
				has_ignore = string.find(sql, "IGNORE") ~= nil
			}
		end
	`, "test", "test_insert_options")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_insert_options")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output
	sql := resultTable.RawGetString("sql").String()
	assert.Contains(t, sql, "INSERT IGNORE INTO users")

	// Verify IGNORE option is present
	hasIgnore := resultTable.RawGetString("has_ignore").(lua.LBool)
	assert.True(t, bool(hasIgnore))
}

// TestInsertSuffix tests adding a suffix like ON DUPLICATE KEY UPDATE
func TestInsertSuffix(t *testing.T) {
	vm, runner, ctx := setupLuaWithInsertModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_insert_suffix()
			-- Create INSERT builder with suffix
			local insert = builder.insert("users")
				:columns("id", "name")
				:values(1, "John")
				:suffix("ON DUPLICATE KEY UPDATE name = VALUES(name)")
			
			-- Convert to SQL
			local sql, args = insert:to_sql()
			
			return {
				sql = sql,
				has_suffix = string.find(sql, "ON DUPLICATE KEY UPDATE") ~= nil
			}
		end
	`, "test", "test_insert_suffix")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_insert_suffix")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output
	sql := resultTable.RawGetString("sql").String()
	assert.Contains(t, sql, "INSERT INTO users")

	// Verify suffix is present
	hasSuffix := resultTable.RawGetString("has_suffix").(lua.LBool)
	assert.True(t, bool(hasSuffix))
}

// TestInsertPlaceholderFormat tests different placeholder formats
func TestInsertPlaceholderFormat(t *testing.T) {
	vm, runner, ctx := setupLuaWithInsertModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_insert_placeholder_format()
			-- Create INSERT builder with dollar placeholder format
			local insert = builder.insert("users")
				:columns("id", "name")
				:values(1, "John")
				:placeholder_format(builder.dollar)
			
			-- Convert to SQL
			local sql, args = insert:to_sql()
			
			-- Create another builder with question placeholder format
			local insert2 = builder.insert("users")
				:columns("id", "name")
				:values(1, "John")
				:placeholder_format(builder.question)
			
			-- Convert to SQL
			local sql2, args2 = insert2:to_sql()
			
			return {
				sql_dollar = sql,
				sql_question = sql2
			}
		end
	`, "test", "test_insert_placeholder_format")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_insert_placeholder_format")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	sqlDollar := resultTable.RawGetString("sql_dollar").String()
	sqlQuestion := resultTable.RawGetString("sql_question").String()

	assert.Contains(t, sqlDollar, "INSERT INTO users")
	assert.Contains(t, sqlQuestion, "INSERT INTO users")
}

// TestInsertToString tests the __tostring metamethod
func TestInsertToString(t *testing.T) {
	vm, runner, ctx := setupLuaWithInsertModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_insert_to_string()
			-- Create INSERT builder
			local insert = builder.insert("users")
				:columns("id", "name")
				:values(1, "John")
			
			-- Convert to string using tostring
			local str = tostring(insert)
			
			return str
		end
	`, "test", "test_insert_to_string")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_insert_to_string")
	require.NoError(t, err)
	resultStr := result.(lua.LString)

	// Verify string representation
	assert.Contains(t, string(resultStr), "InsertBuilder")
	assert.Contains(t, string(resultStr), "INSERT INTO users")
}

// TestInsertErrors tests error handling
func TestInsertErrors(t *testing.T) {
	vm, runner, ctx := setupLuaWithInsertModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_insert_errors()
			-- Create an INSERT without table
			local insert1 = builder.insert()
			
			-- Try to get SQL (should return nil, error)
			local sql1, err1 = insert1:to_sql()
			
			-- Create an INSERT without values
			local insert2 = builder.insert("users")
				:columns("id", "name")
				-- No values set
			
			-- Try to get SQL (should return nil, error)
			local sql2, err2 = insert2:to_sql()
			
			return {
				sql1 = sql1,
				err1 = err1,
				sql2 = sql2,
				err2 = err2
			}
		end
	`, "test", "test_insert_errors")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_insert_errors")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify error handling
	sql1 := resultTable.RawGetString("sql1")
	err1 := resultTable.RawGetString("err1")
	sql2 := resultTable.RawGetString("sql2")
	err2 := resultTable.RawGetString("err2")

	// Check that SQL results are nil
	assert.Equal(t, lua.LNil, sql1)
	assert.Equal(t, lua.LNil, sql2)

	// Check error messages
	assert.Contains(t, err1.String(), "insert statements must specify a table")
	assert.Contains(t, err2.String(), "insert statements must have at least one set of values")
}
