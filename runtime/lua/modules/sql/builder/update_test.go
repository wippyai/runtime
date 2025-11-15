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

// setupLuaWithUpdateModule sets up a Lua state with the UpdateBuilder registered
func setupLuaWithUpdateModule(t *testing.T) (*engine.CoroutineVM, *engine.Runner, context.Context) {
	logger := zaptest.NewLogger(t)

	// Create a VM with coroutine support
	vm, err := engine.NewCVM(logger)
	require.NoError(t, err)

	// Get the Lua state
	L := vm.State()

	// Create a table and register Update builder
	modTable := L.CreateTable(0, 20)
	registerUpdateBuilderType(L)
	registerSqlizerMetatable(L) // For Expr etc.

	// Register the placeholder formats
	registerPlaceholderFormats(L, modTable)

	// Register the expression builders
	registerExpressionBuilders(L, modTable)

	// Register SelectBuilder for from_select tests
	registerSelectBuilderType(L)
	modTable.RawSetString("select", L.NewFunction(builderSelect))

	// Add the UPDATE builder
	modTable.RawSetString("update", L.NewFunction(builderUpdate))

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
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	// Initialize a unit of work with the context
	_, luaCtx := runner.InitUnitOfWork(ctx)

	// Set the context in the Lua state
	L.SetContext(luaCtx)

	return vm, runner, luaCtx
}

// TestUpdateBasic tests basic UpdateBuilder functionality
func TestUpdateBasic(t *testing.T) {
	vm, runner, ctx := setupLuaWithUpdateModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_update_basic()
			-- Create a basic UPDATE builder
			local update = builder.update("users")
				:set("name", "John")
			
			-- Convert to SQL
			local sql, args = update:to_sql()
			
			return {
				sql = sql,
				args = args,
				args_count = #args
			}
		end
	`, "test", "test_update_basic")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_update_basic")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output
	sql := resultTable.RawGetString("sql").String()
	assert.Contains(t, sql, "UPDATE users SET name = ?")

	args := resultTable.RawGetString("args").(*lua.LTable)
	assert.Equal(t, 1, args.Len())
	assert.Equal(t, "John", args.RawGetInt(1).String())
}

// TestUpdateTable tests the table method
func TestUpdateTable(t *testing.T) {
	vm, runner, ctx := setupLuaWithUpdateModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_update_table()
			-- Create builder without table and set it with table()
			local update = builder.update()
			update = update:table("users")
			update = update:set("name", "John")
			
			-- Convert to SQL
			local sql, args = update:to_sql()
			
			return {
				sql = sql,
				args = args
			}
		end
	`, "test", "test_update_table")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_update_table")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output
	sql := resultTable.RawGetString("sql").String()
	assert.Contains(t, sql, "UPDATE users SET name = ?")

	args := resultTable.RawGetString("args").(*lua.LTable)
	assert.Equal(t, 1, args.Len())
	assert.Equal(t, "John", args.RawGetInt(1).String())
}

// TestUpdateSetMap tests the set_map method
func TestUpdateSetMap(t *testing.T) {
	vm, runner, ctx := setupLuaWithUpdateModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_update_set_map()
			-- Create UPDATE with set_map
			local update = builder.update("users")
				:set_map({
					name = "John",
					email = "john@example.com",
					active = true
				})
			
			-- Convert to SQL
			local sql, args = update:to_sql()
			
			-- The columns in set_map might be in any order, so we'll check them separately
			local has_name = string.find(sql, "name = ?") ~= nil
			local has_email = string.find(sql, "email = ?") ~= nil
			local has_active = string.find(sql, "active = ?") ~= nil
			
			return {
				sql = sql,
				args = args,
				args_count = #args,
				has_all_columns = has_name and has_email and has_active
			}
		end
	`, "test", "test_update_set_map")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_update_set_map")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output
	sql := resultTable.RawGetString("sql").String()
	assert.Contains(t, sql, "UPDATE users SET")

	// Verify all columns are present
	hasAllColumns := resultTable.RawGetString("has_all_columns").(lua.LBool)
	assert.True(t, bool(hasAllColumns))

	// Verify args count
	argsCount := int(resultTable.RawGetString("args_count").(lua.LNumber))
	assert.Equal(t, 3, argsCount)
}

// TestUpdateWhere tests the where method with different clause types
func TestUpdateWhere(t *testing.T) {
	vm, runner, ctx := setupLuaWithUpdateModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_update_where()
			-- UPDATE with string condition
			local update1 = builder.update("users")
				:set("name", "John")
				:where("id = ?", 1)
			
			local sql1, args1 = update1:to_sql()
			
			-- UPDATE with table condition
			local update2 = builder.update("users")
				:set("name", "Jane")
				:where({active = true, role = "admin"})
			
			local sql2, args2 = update2:to_sql()
			
			-- UPDATE with Sqlizer condition
			local update3 = builder.update("users")
				:set("name", "Bob")
				:where(builder.eq({id = 1}))
			
			local sql3, args3 = update3:to_sql()
			
			-- UPDATE with multiple where calls
			local update4 = builder.update("users")
				:set("status", "inactive")
				:where("created_at < ?", "2023-01-01")
				:where(builder.eq({active = true}))
			
			local sql4, args4 = update4:to_sql()
			
			return {
				sql1 = sql1, args1 = args1,
				sql2 = sql2, args2 = args2,
				sql3 = sql3, args3 = args3,
				sql4 = sql4, args4 = args4
			}
		end
	`, "test", "test_update_where")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_update_where")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output with string condition
	sql1 := resultTable.RawGetString("sql1").String()
	args1 := resultTable.RawGetString("args1").(*lua.LTable)
	assert.Contains(t, sql1, "UPDATE users SET name = ? WHERE id = ?")
	assert.Equal(t, 2, args1.Len())
	assert.Equal(t, "John", args1.RawGetInt(1).String())
	assert.Equal(t, float64(1), float64(args1.RawGetInt(2).(lua.LNumber)))

	// Verify SQL output with table condition
	sql2 := resultTable.RawGetString("sql2").String()
	args2 := resultTable.RawGetString("args2").(*lua.LTable)
	assert.Contains(t, sql2, "UPDATE users SET name = ? WHERE")
	assert.Contains(t, sql2, "active = ?")
	assert.Contains(t, sql2, "role = ?")
	assert.Equal(t, 3, args2.Len())

	// Verify SQL output with Sqlizer condition
	sql3 := resultTable.RawGetString("sql3").String()
	args3 := resultTable.RawGetString("args3").(*lua.LTable)
	assert.Contains(t, sql3, "UPDATE users SET name = ? WHERE id = ?")
	assert.Equal(t, 2, args3.Len())

	// Verify SQL output with multiple where calls
	sql4 := resultTable.RawGetString("sql4").String()
	args4 := resultTable.RawGetString("args4").(*lua.LTable)
	assert.Contains(t, sql4, "UPDATE users SET status = ? WHERE created_at < ? AND active = ?")
	assert.Equal(t, 3, args4.Len())
}

// TestUpdateOrderLimit tests order_by, limit, and offset methods
func TestUpdateOrderLimit(t *testing.T) {
	vm, runner, ctx := setupLuaWithUpdateModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_update_order_limit()
			-- UPDATE with ORDER BY
			local update1 = builder.update("users")
				:set("name", "John")
				:order_by("id DESC")
			
			local sql1, args1 = update1:to_sql()
			
			-- UPDATE with multiple ORDER BY
			local update2 = builder.update("users")
				:set("name", "John")
				:order_by("created_at DESC", "id ASC")
			
			local sql2, args2 = update2:to_sql()
			
			-- UPDATE with LIMIT
			local update3 = builder.update("users")
				:set("name", "John")
				:limit(10)
			
			local sql3, args3 = update3:to_sql()
			
			-- UPDATE with OFFSET
			local update4 = builder.update("users")
				:set("name", "John")
				:offset(5)
			
			local sql4, args4 = update4:to_sql()
			
			-- UPDATE with ORDER BY, LIMIT, and OFFSET
			local update5 = builder.update("users")
				:set("active", false)
				:where({status = "pending"})
				:order_by("last_login ASC")
				:limit(100)
				:offset(200)
			
			local sql5, args5 = update5:to_sql()
			
			return {
				sql1 = sql1, 
				sql2 = sql2, 
				sql3 = sql3, 
				sql4 = sql4, 
				sql5 = sql5, args5 = args5
			}
		end
	`, "test", "test_update_order_limit")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_update_order_limit")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify ORDER BY clause
	sql1 := resultTable.RawGetString("sql1").String()
	assert.Contains(t, sql1, "ORDER BY id DESC")

	// Verify multiple ORDER BY clauses
	sql2 := resultTable.RawGetString("sql2").String()
	assert.Contains(t, sql2, "ORDER BY created_at DESC, id ASC")

	// Verify LIMIT clause
	sql3 := resultTable.RawGetString("sql3").String()
	assert.Contains(t, sql3, "LIMIT 10")

	// Verify OFFSET clause
	sql4 := resultTable.RawGetString("sql4").String()
	assert.Contains(t, sql4, "OFFSET 5")

	// Verify combined clauses
	sql5 := resultTable.RawGetString("sql5").String()
	args5 := resultTable.RawGetString("args5").(*lua.LTable)
	assert.Contains(t, sql5, "WHERE status = ? ORDER BY last_login ASC LIMIT 100 OFFSET 200")
	assert.Equal(t, 2, args5.Len()) // 1 for active=false, 1 for status=pending
}

// TestUpdateSuffix tests the suffix method
func TestUpdateSuffix(t *testing.T) {
	vm, runner, ctx := setupLuaWithUpdateModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_update_suffix()
			-- UPDATE with suffix
			local update = builder.update("users")
				:set("name", "John")
				:where({id = 1})
				:suffix("RETURNING id, name")
			
			local sql, args = update:to_sql()
			
			-- UPDATE with parameterized suffix
			local update2 = builder.update("logs")
				:set("status", "processed")
				:where("created_at < ?", "2023-01-01")
				:suffix("RETURNING ?", "id")
			
			local sql2, args2 = update2:to_sql()
			
			return {
				sql = sql,
				args = args,
				sql2 = sql2,
				args2 = args2
			}
		end
	`, "test", "test_update_suffix")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_update_suffix")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output with suffix
	sql := resultTable.RawGetString("sql").String()
	assert.Contains(t, sql, "UPDATE users SET name = ? WHERE id = ? RETURNING id, name")

	// Verify SQL output with parameterized suffix
	sql2 := resultTable.RawGetString("sql2").String()
	args2 := resultTable.RawGetString("args2").(*lua.LTable)
	assert.Contains(t, sql2, "UPDATE logs SET status = ? WHERE created_at < ? RETURNING ?")
	assert.Equal(t, 3, args2.Len())
	assert.Equal(t, "processed", args2.RawGetInt(1).String())
	assert.Equal(t, "2023-01-01", args2.RawGetInt(2).String())
	assert.Equal(t, "id", args2.RawGetInt(3).String())
}

// TestUpdateFrom tests the from and from_select methods
func TestUpdateFrom(t *testing.T) {
	vm, runner, ctx := setupLuaWithUpdateModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_update_from()
			-- UPDATE with FROM clause (for Postgres)
			local update1 = builder.update("users")
				:set("active", false)
				:from("inactive_logs")
				:where("users.id = inactive_logs.user_id")
			
			local sql1, args1 = update1:to_sql()
			
			-- UPDATE with FROM SELECT
			local select_query = builder.select("user_id")
				:from("inactive_logs")
				:where("last_seen < ?", "2023-01-01")
			
			local update2 = builder.update("users")
				:set("active", false)
				:from_select(select_query, "inactive_users")
				:where("users.id = inactive_users.user_id")
			
			local sql2, args2 = update2:to_sql()
			
			return {
				sql1 = sql1,
				args1 = args1,
				sql2 = sql2,
				args2 = args2
			}
		end
	`, "test", "test_update_from")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_update_from")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output with FROM clause
	sql1 := resultTable.RawGetString("sql1").String()
	args1 := resultTable.RawGetString("args1").(*lua.LTable)
	assert.Contains(t, sql1, "UPDATE users SET active = ? FROM inactive_logs WHERE users.id = inactive_logs.user_id")
	assert.Equal(t, 1, args1.Len())

	// Verify SQL output with FROM SELECT
	sql2 := resultTable.RawGetString("sql2").String()
	args2 := resultTable.RawGetString("args2").(*lua.LTable)
	assert.Contains(t, sql2, "UPDATE users SET active = ? FROM")
	assert.Contains(t, sql2, "SELECT user_id FROM inactive_logs WHERE last_seen < ?")
	assert.Contains(t, sql2, "AS inactive_users WHERE users.id = inactive_users.user_id")
	assert.Equal(t, 2, args2.Len())
}

// TestUpdatePlaceholderFormat tests different placeholder formats
func TestUpdatePlaceholderFormat(t *testing.T) {
	vm, runner, ctx := setupLuaWithUpdateModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_update_placeholder_format()
			-- UPDATE with dollar placeholder format
			local update1 = builder.update("users")
				:set("name", "John")
				:where("id = ?", 1)
				:placeholder_format(builder.dollar)
			
			local sql1, args1 = update1:to_sql()
			
			-- UPDATE with question placeholder format
			local update2 = builder.update("users")
				:set("name", "John")
				:where("id = ?", 1)
				:placeholder_format(builder.question)
			
			local sql2, args2 = update2:to_sql()
			
			return {
				sql_dollar = sql1,
				sql_question = sql2
			}
		end
	`, "test", "test_update_placeholder_format")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_update_placeholder_format")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	sqlDollar := resultTable.RawGetString("sql_dollar").String()
	sqlQuestion := resultTable.RawGetString("sql_question").String()

	assert.Contains(t, sqlDollar, "UPDATE users SET")
	assert.Contains(t, sqlQuestion, "UPDATE users SET")

	// Dollar format should use $1, $2, etc.
	assert.Contains(t, sqlDollar, "$")

	// Question format should use ?
	assert.Contains(t, sqlQuestion, "?")
}

// TestUpdateToString tests the __tostring metamethod
func TestUpdateToString(t *testing.T) {
	vm, runner, ctx := setupLuaWithUpdateModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_update_to_string()
			-- Create UPDATE builder
			local update = builder.update("users")
				:set("name", "John")
				:where("id = ?", 1)
			
			-- Convert to string using tostring
			local str = tostring(update)
			
			return str
		end
	`, "test", "test_update_to_string")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_update_to_string")
	require.NoError(t, err)
	resultStr := result.(lua.LString)

	// Verify string representation
	assert.Contains(t, string(resultStr), "UpdateBuilder")
	assert.Contains(t, string(resultStr), "UPDATE users")
}

// TestUpdateMultipleSet tests multiple set operations
func TestUpdateMultipleSet(t *testing.T) {
	vm, runner, ctx := setupLuaWithUpdateModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_update_multiple_set()
			-- UPDATE with multiple SET clauses
			local update = builder.update("users")
				:set("name", "John")
				:set("email", "john@example.com")
				:set("updated_at", "2023-01-01")
			
			local sql, args = update:to_sql()
			
			return {
				sql = sql,
				args = args,
				args_count = #args
			}
		end
	`, "test", "test_update_multiple_set")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_update_multiple_set")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output
	sql := resultTable.RawGetString("sql").String()
	assert.Contains(t, sql, "UPDATE users SET")
	assert.Contains(t, sql, "name = ?")
	assert.Contains(t, sql, "email = ?")
	assert.Contains(t, sql, "updated_at = ?")

	// Verify args count
	argsCount := int(resultTable.RawGetString("args_count").(lua.LNumber))
	assert.Equal(t, 3, argsCount)
}

// TestUpdateWithNullValues tests handling NULL values
func TestUpdateWithNullValues(t *testing.T) {
	vm, runner, ctx := setupLuaWithUpdateModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_update_with_null()
			-- UPDATE with NULL values using SQL_NULL marker
			local update = builder.update("users")
				:set("email", SQL_NULL)
				:where("id = ?", 1)
			
			local sql, args = update:to_sql()
			
			-- In Lua we can't directly check if args[1] is nil because Lua tables
			-- can't store nil values. Instead, check the args count and SQL format.
			local has_email_set = string.find(sql, "email = ?") ~= nil
			local has_where_id = string.find(sql, "WHERE id = ?") ~= nil
			
			return {
				sql = sql,
				args_count = #args,
				has_email_set = has_email_set,
				has_where_id = has_where_id
			}
		end
	`, "test", "test_update_with_null")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_update_with_null")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output
	sql := resultTable.RawGetString("sql").String()
	assert.Contains(t, sql, "UPDATE users SET email = ? WHERE id = ?")

	// Verify args count - should be 2 (NULL value and ID)
	argsCount := int(resultTable.RawGetString("args_count").(lua.LNumber))
	assert.Equal(t, 2, argsCount)

	// Check if the SQL structure is correct
	hasEmailSet := bool(resultTable.RawGetString("has_email_set").(lua.LBool))
	hasWhereID := bool(resultTable.RawGetString("has_where_id").(lua.LBool))
	assert.True(t, hasEmailSet)
	assert.True(t, hasWhereID)
}

// TestUpdateErrors tests error handling
func TestUpdateErrors(t *testing.T) {
	vm, runner, ctx := setupLuaWithUpdateModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_update_errors()
			-- Create an UPDATE without table
			local update1 = builder.update()
			
			-- Try to get SQL (should return nil, error)
			local sql1, err1 = update1:set("name", "John"):to_sql()
			
			-- Create an UPDATE without SET clauses
			local update2 = builder.update("users")
			
			-- Try to get SQL (should return nil, error)
			local sql2, err2 = update2:to_sql()
			
			-- Using bad placeholder format with a non-userdata value
			local update3 = builder.update("users"):set("name", "John")
			local success, err3 = pcall(function()
				update3:placeholder_format("invalid")
			end)
			
			return {
				sql1 = sql1,
				err1 = err1,
				sql2 = sql2,
				err2 = err2,
				success = success,
				err3 = err3
			}
		end
	`, "test", "test_update_errors")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_update_errors")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify error handling for missing table
	sql1 := resultTable.RawGetString("sql1")
	err1 := resultTable.RawGetString("err1")
	assert.Equal(t, lua.LNil, sql1)
	assert.Contains(t, err1.String(), "update statements must specify a table")

	// Verify error handling for missing SET clauses
	sql2 := resultTable.RawGetString("sql2")
	err2 := resultTable.RawGetString("err2")
	assert.Equal(t, lua.LNil, sql2)
	assert.Contains(t, err2.String(), "update statements must have at least one Set clause")

	// Verify error handling for invalid placeholder
	success := lua.LVAsBool(resultTable.RawGetString("success"))
	err3 := resultTable.RawGetString("err3").String()
	assert.False(t, success)
	assert.Contains(t, err3, "bad argument #2 to placeholder_format")
}
