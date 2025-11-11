package builder

import (
	"context"
	ctxapi "github.com/ponyruntime/pony/api/context"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap/zaptest"
)

// setupLuaWithDeleteModule sets up a Lua state with the DeleteBuilder registered
func setupLuaWithDeleteModule(t *testing.T) (*engine.CoroutineVM, *engine.Runner, context.Context) {
	logger := zaptest.NewLogger(t)

	// Create a VM with coroutine support
	vm, err := engine.NewCVM(logger)
	require.NoError(t, err)

	// Get the Lua state
	L := vm.State()

	// Create a table and register Delete builder
	modTable := L.CreateTable(0, 20)
	registerDeleteBuilderType(L)
	registerSqlizerMetatable(L) // For Expr etc.

	// Register the placeholder formats
	registerPlaceholderFormats(L, modTable)

	// Register the expression builders
	registerExpressionBuilders(L, modTable)

	// Add the DELETE builder
	modTable.RawSetString("delete", L.NewFunction(builderDelete))

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

// TestDeleteBasic tests basic DeleteBuilder functionality
func TestDeleteBasic(t *testing.T) {
	vm, runner, ctx := setupLuaWithDeleteModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_delete_basic()
			-- Create a basic DELETE builder
			local delete = builder.delete("users")
			
			-- Convert to SQL
			local sql, args = delete:to_sql()
			
			return {
				sql = sql,
				args_count = #args
			}
		end
	`, "test", "test_delete_basic")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_delete_basic")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output
	sql := resultTable.RawGetString("sql").String()
	assert.Contains(t, sql, "DELETE FROM users")
	assert.Equal(t, 0, int(resultTable.RawGetString("args_count").(lua.LNumber)))
}

// TestDeleteFrom tests the from method
func TestDeleteFrom(t *testing.T) {
	vm, runner, ctx := setupLuaWithDeleteModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_delete_from()
			-- Create builder and set table with from()
			local delete = builder.delete()
			delete = delete:from("users")
			
			-- Convert to SQL
			local sql, args = delete:to_sql()
			
			return {
				sql = sql,
				args_count = #args
			}
		end
	`, "test", "test_delete_from")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_delete_from")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output
	sql := resultTable.RawGetString("sql").String()
	assert.Contains(t, sql, "DELETE FROM users")
	assert.Equal(t, 0, int(resultTable.RawGetString("args_count").(lua.LNumber)))
}

// TestDeleteWhere tests the where method with different clause types
func TestDeleteWhere(t *testing.T) {
	vm, runner, ctx := setupLuaWithDeleteModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_delete_where()
			-- DELETE with string condition
			local delete1 = builder.delete("users")
				:where("id = ?", 1)
			
			local sql1, args1 = delete1:to_sql()
			
			-- DELETE with table condition
			local delete2 = builder.delete("users")
				:where({active = true, role = "admin"})
			
			local sql2, args2 = delete2:to_sql()
			
			-- DELETE with Sqlizer condition
			local delete3 = builder.delete("users")
				:where(builder.eq({id = 1}))
			
			local sql3, args3 = delete3:to_sql()
			
			-- DELETE with multiple where calls
			local delete4 = builder.delete("users")
				:where("created_at < ?", "2023-01-01")
				:where(builder.eq({active = true}))
			
			local sql4, args4 = delete4:to_sql()
			
			return {
				sql1 = sql1, args1 = args1,
				sql2 = sql2, args2 = args2,
				sql3 = sql3, args3 = args3,
				sql4 = sql4, args4 = args4
			}
		end
	`, "test", "test_delete_where")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_delete_where")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output with string condition
	sql1 := resultTable.RawGetString("sql1").String()
	args1 := resultTable.RawGetString("args1").(*lua.LTable)
	assert.Contains(t, sql1, "DELETE FROM users WHERE id = ?")
	assert.Equal(t, 1, args1.Len())
	assert.Equal(t, float64(1), float64(args1.RawGetInt(1).(lua.LNumber)))

	// Verify SQL output with table condition
	sql2 := resultTable.RawGetString("sql2").String()
	args2 := resultTable.RawGetString("args2").(*lua.LTable)
	assert.Contains(t, sql2, "DELETE FROM users WHERE")
	assert.Contains(t, sql2, "active = ?")
	assert.Contains(t, sql2, "role = ?")
	assert.Equal(t, 2, args2.Len())

	// Verify SQL output with Sqlizer condition
	sql3 := resultTable.RawGetString("sql3").String()
	args3 := resultTable.RawGetString("args3").(*lua.LTable)
	assert.Contains(t, sql3, "DELETE FROM users WHERE id = ?")
	assert.Equal(t, 1, args3.Len())
	assert.Equal(t, float64(1), float64(args3.RawGetInt(1).(lua.LNumber)))

	// Verify SQL output with multiple where calls
	sql4 := resultTable.RawGetString("sql4").String()
	args4 := resultTable.RawGetString("args4").(*lua.LTable)
	assert.Contains(t, sql4, "DELETE FROM users WHERE created_at < ? AND active = ?")
	assert.Equal(t, 2, args4.Len())
}

// TestDeleteOrderLimit tests order_by, limit, and offset methods
func TestDeleteOrderLimit(t *testing.T) {
	vm, runner, ctx := setupLuaWithDeleteModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_delete_order_limit()
			-- DELETE with ORDER BY
			local delete1 = builder.delete("users")
				:order_by("id DESC")
			
			local sql1, args1 = delete1:to_sql()
			
			-- DELETE with multiple ORDER BY
			local delete2 = builder.delete("users")
				:order_by("created_at DESC", "id ASC")
			
			local sql2, args2 = delete2:to_sql()
			
			-- DELETE with LIMIT
			local delete3 = builder.delete("users")
				:limit(10)
			
			local sql3, args3 = delete3:to_sql()
			
			-- DELETE with OFFSET
			local delete4 = builder.delete("users")
				:offset(5)
			
			local sql4, args4 = delete4:to_sql()
			
			-- DELETE with ORDER BY, LIMIT, and OFFSET
			local delete5 = builder.delete("users")
				:where({active = false})
				:order_by("last_login ASC")
				:limit(100)
				:offset(200)
			
			local sql5, args5 = delete5:to_sql()
			
			return {
				sql1 = sql1, 
				sql2 = sql2, 
				sql3 = sql3, 
				sql4 = sql4, 
				sql5 = sql5, args5 = args5
			}
		end
	`, "test", "test_delete_order_limit")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_delete_order_limit")
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
	assert.Contains(t, sql5, "DELETE FROM users WHERE active = ? ORDER BY last_login ASC LIMIT 100 OFFSET 200")
	assert.Equal(t, 1, args5.Len())
}

// TestDeleteSuffix tests the suffix method
func TestDeleteSuffix(t *testing.T) {
	vm, runner, ctx := setupLuaWithDeleteModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_delete_suffix()
			-- DELETE with suffix
			local delete = builder.delete("users")
				:where({id = 1})
				:suffix("RETURNING id, name")
			
			local sql, args = delete:to_sql()
			
			-- DELETE with parameterized suffix
			local delete2 = builder.delete("logs")
				:where("created_at < ?", "2023-01-01")
				:suffix("RETURNING ?", "id")
			
			local sql2, args2 = delete2:to_sql()
			
			return {
				sql = sql,
				args = args,
				sql2 = sql2,
				args2 = args2
			}
		end
	`, "test", "test_delete_suffix")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_delete_suffix")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output with suffix
	sql := resultTable.RawGetString("sql").String()
	assert.Contains(t, sql, "DELETE FROM users WHERE id = ? RETURNING id, name")

	// Verify SQL output with parameterized suffix
	sql2 := resultTable.RawGetString("sql2").String()
	args2 := resultTable.RawGetString("args2").(*lua.LTable)
	assert.Contains(t, sql2, "DELETE FROM logs WHERE created_at < ? RETURNING ?")
	assert.Equal(t, 2, args2.Len())
	assert.Equal(t, "2023-01-01", args2.RawGetInt(1).String())
	assert.Equal(t, "id", args2.RawGetInt(2).String())
}

// TestDeletePlaceholderFormat tests different placeholder formats
func TestDeletePlaceholderFormat(t *testing.T) {
	vm, runner, ctx := setupLuaWithDeleteModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_delete_placeholder_format()
			-- DELETE with dollar placeholder format
			local delete1 = builder.delete("users")
				:where("id = ?", 1)
				:placeholder_format(builder.dollar)
			
			local sql1, args1 = delete1:to_sql()
			
			-- DELETE with question placeholder format
			local delete2 = builder.delete("users")
				:where("id = ?", 1)
				:placeholder_format(builder.question)
			
			local sql2, args2 = delete2:to_sql()
			
			return {
				sql_dollar = sql1,
				sql_question = sql2
			}
		end
	`, "test", "test_delete_placeholder_format")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_delete_placeholder_format")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	sqlDollar := resultTable.RawGetString("sql_dollar").String()
	sqlQuestion := resultTable.RawGetString("sql_question").String()

	assert.Contains(t, sqlDollar, "DELETE FROM users WHERE")
	assert.Contains(t, sqlQuestion, "DELETE FROM users WHERE")
}

// TestDeleteToString tests the __tostring metamethod
func TestDeleteToString(t *testing.T) {
	vm, runner, ctx := setupLuaWithDeleteModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_delete_to_string()
			-- Create DELETE builder
			local delete = builder.delete("users")
				:where("id = ?", 1)
			
			-- Convert to string using tostring
			local str = tostring(delete)
			
			return str
		end
	`, "test", "test_delete_to_string")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_delete_to_string")
	require.NoError(t, err)
	resultStr := result.(lua.LString)

	// Verify string representation
	assert.Contains(t, string(resultStr), "DeleteBuilder")
	assert.Contains(t, string(resultStr), "DELETE FROM users")
}

// TestDeleteErrors tests error handling
func TestDeleteErrors(t *testing.T) {
	vm, runner, ctx := setupLuaWithDeleteModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_delete_errors()
			-- Create a DELETE without table
			local delete1 = builder.delete()
			
			-- Try to get SQL (should return nil, error)
			local sql1, err1 = delete1:to_sql()
			
			-- Using bad placeholder format with a non-userdata value
			local delete2 = builder.delete("users")
			local success, err2 = pcall(function()
				delete2:placeholder_format("invalid")
			end)
			
			return {
				sql1 = sql1,
				err1 = err1,
				success = success,
				err2 = err2
			}
		end
	`, "test", "test_delete_errors")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_delete_errors")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify error handling for missing table
	sql1 := resultTable.RawGetString("sql1")
	err1 := resultTable.RawGetString("err1")
	assert.Equal(t, lua.LNil, sql1)
	assert.Contains(t, err1.String(), "delete statements must specify a From table")

	// Verify error handling for invalid placeholder
	success := lua.LVAsBool(resultTable.RawGetString("success"))
	err2 := resultTable.RawGetString("err2").String()
	assert.False(t, success)
	assert.Contains(t, err2, "bad argument #2 to placeholder_format")
}
