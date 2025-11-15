package builder

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/coroutine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap/zaptest"
)

// setupLuaWithSelectModule sets up a Lua state with the SelectBuilder registered
func setupLuaWithSelectModule(t *testing.T) (*engine.CoroutineVM, *engine.Runner, context.Context) {
	logger := zaptest.NewLogger(t)

	// Create a VM with coroutine support
	vm, err := engine.NewCVM(logger)
	require.NoError(t, err)

	// Get the Lua state
	L := vm.State()

	// Create a table and register Select builder
	modTable := L.CreateTable(0, 20)
	registerSelectBuilderType(L)
	registerSqlizerMetatable(L) // For Expr etc.

	// Register the placeholder formats
	registerPlaceholderFormats(L, modTable)

	// Register the expression builders
	registerExpressionBuilders(L, modTable)

	// Add the SELECT builder
	modTable.RawSetString("select", L.NewFunction(builderSelect))

	// Create and set the SQL NULL value
	nullUserData := L.NewUserData()
	nullUserData.Value = "SQL_NULL"
	L.SetGlobal("SQL_NULL", nullUserData)

	// Add the table to global state for testing
	L.SetGlobal("builder", modTable)

	// Create a runner with the coroutine layer
	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))

	// Create test context with FrameContext
	ctx := newTestContext()

	return vm, runner, ctx
}

// TestSelectBasic tests basic SelectBuilder functionality
func TestSelectBasic(t *testing.T) {
	vm, runner, ctx := setupLuaWithSelectModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_select_basic()
			-- Create a basic SELECT builder
			local select = builder.select("id", "name", "email")
				:from("users")

			-- Convert to SQL
			local sql, args = select:to_sql()

			-- Empty SELECT builder (should error)
			local empty_select = builder.select()
			local empty_sql, empty_error = empty_select:to_sql()

			return {
				sql = sql,
				args = args,
				args_count = #args,
				empty_sql = empty_sql,
				empty_error = tostring(empty_error)
			}
		end
	`, "test", "test_select_basic")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_select_basic")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output
	sql := resultTable.RawGetString("sql").String()
	assert.Equal(t, "SELECT id, name, email FROM users", sql)

	args := resultTable.RawGetString("args").(*lua.LTable)
	assert.Equal(t, 0, args.Len())

	// Empty SELECT should error
	emptySQL := resultTable.RawGetString("empty_sql")
	emptyError := resultTable.RawGetString("empty_error")
	assert.Equal(t, lua.LNil, emptySQL)
	assert.Contains(t, emptyError.String(), "select statements must have at least one result column")
}

// TestSelectColumns tests adding columns to a SELECT
func TestSelectColumns(t *testing.T) {
	vm, runner, ctx := setupLuaWithSelectModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_select_columns()
			-- Create SELECT with columns in constructor
			local select1 = builder.select("id", "name")
				:from("users")
			
			local sql1, args1 = select1:to_sql()
			
			-- Add additional columns with columns()
			local select2 = builder.select("id")
				:from("users")
				:columns("name", "email")
			
			local sql2, args2 = select2:to_sql()
			
			-- Add DISTINCT
			local select3 = builder.select("id", "name")
				:from("users")
				:distinct()
			
			local sql3, args3 = select3:to_sql()
			
			return {
				sql1 = sql1,
				sql2 = sql2,
				sql3 = sql3
			}
		end
	`, "test", "test_select_columns")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_select_columns")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output for columns in constructor
	sql1 := resultTable.RawGetString("sql1").String()
	assert.Equal(t, "SELECT id, name FROM users", sql1)

	// Verify SQL output for adding columns with columns()
	sql2 := resultTable.RawGetString("sql2").String()
	assert.Equal(t, "SELECT id, name, email FROM users", sql2)

	// Verify SQL output for DISTINCT
	sql3 := resultTable.RawGetString("sql3").String()
	assert.Equal(t, "SELECT DISTINCT id, name FROM users", sql3)
}

// TestSelectWhere tests the where method with different clause types
func TestSelectWhere(t *testing.T) {
	vm, runner, ctx := setupLuaWithSelectModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_select_where()
			-- SELECT with string condition
			local select1 = builder.select("id", "name")
				:from("users")
				:where("id = ?", 1)
			
			local sql1, args1 = select1:to_sql()
			
			-- SELECT with table condition
			local select2 = builder.select("id", "name")
				:from("users")
				:where({active = true, role = "admin"})
			
			local sql2, args2 = select2:to_sql()
			
			-- SELECT with Sqlizer condition
			local select3 = builder.select("id", "name")
				:from("users")
				:where(builder.eq({id = 1}))
			
			local sql3, args3 = select3:to_sql()
			
			-- SELECT with multiple where calls (ANDed together)
			local select4 = builder.select("id", "name")
				:from("users")
				:where("created_at > ?", "2023-01-01")
				:where(builder.eq({active = true}))
			
			local sql4, args4 = select4:to_sql()
			
			-- SELECT with OR condition
			local select5 = builder.select("id", "name")
				:from("users")
				:where(builder.or_({
					builder.eq({role = "admin"}),
					builder.eq({role = "manager"})
				}))
			
			local sql5, args5 = select5:to_sql()
			
			return {
				sql1 = sql1, args1 = args1,
				sql2 = sql2, args2 = args2,
				sql3 = sql3, args3 = args3,
				sql4 = sql4, args4 = args4,
				sql5 = sql5, args5 = args5
			}
		end
	`, "test", "test_select_where")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_select_where")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output with string condition
	sql1 := resultTable.RawGetString("sql1").String()
	args1 := resultTable.RawGetString("args1").(*lua.LTable)
	assert.Equal(t, "SELECT id, name FROM users WHERE id = ?", sql1)
	assert.Equal(t, 1, args1.Len())
	assert.Equal(t, float64(1), float64(args1.RawGetInt(1).(lua.LNumber)))

	// Verify SQL output with table condition
	sql2 := resultTable.RawGetString("sql2").String()
	args2 := resultTable.RawGetString("args2").(*lua.LTable)
	assert.Contains(t, sql2, "SELECT id, name FROM users WHERE")
	assert.Contains(t, sql2, "active = ?")
	assert.Contains(t, sql2, "role = ?")
	assert.Equal(t, 2, args2.Len())

	// Verify SQL output with Sqlizer condition
	sql3 := resultTable.RawGetString("sql3").String()
	args3 := resultTable.RawGetString("args3").(*lua.LTable)
	assert.Equal(t, "SELECT id, name FROM users WHERE id = ?", sql3)
	assert.Equal(t, 1, args3.Len())

	// Verify SQL output with multiple where calls
	sql4 := resultTable.RawGetString("sql4").String()
	args4 := resultTable.RawGetString("args4").(*lua.LTable)
	assert.Equal(t, "SELECT id, name FROM users WHERE created_at > ? AND active = ?", sql4)
	assert.Equal(t, 2, args4.Len())

	// Verify SQL output with OR condition
	sql5 := resultTable.RawGetString("sql5").String()
	args5 := resultTable.RawGetString("args5").(*lua.LTable)
	assert.Contains(t, sql5, "SELECT id, name FROM users WHERE (role = ? OR role = ?)")
	assert.Equal(t, 2, args5.Len())
}

// TestSelectJoins tests the various join methods
func TestSelectJoins(t *testing.T) {
	vm, runner, ctx := setupLuaWithSelectModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_select_joins()
			-- Basic JOIN
			local select1 = builder.select("users.id", "profiles.bio")
				:from("users")
				:join("profiles ON users.id = profiles.user_id")
			
			local sql1, args1 = select1:to_sql()
			
			-- JOIN with placeholder
			local select2 = builder.select("users.id", "profiles.bio")
				:from("users")
				:join("profiles ON users.id = profiles.user_id AND profiles.created_at > ?", "2023-01-01")
			
			local sql2, args2 = select2:to_sql()
			
			-- LEFT JOIN
			local select3 = builder.select("users.id", "profiles.bio")
				:from("users")
				:left_join("profiles ON users.id = profiles.user_id")
			
			local sql3, args3 = select3:to_sql()
			
			-- RIGHT JOIN
			local select4 = builder.select("users.id", "profiles.bio")
				:from("users")
				:right_join("profiles ON users.id = profiles.user_id")
			
			local sql4, args4 = select4:to_sql()
			
			-- INNER JOIN
			local select5 = builder.select("users.id", "profiles.bio")
				:from("users")
				:inner_join("profiles ON users.id = profiles.user_id")
			
			local sql5, args5 = select5:to_sql()
			
			-- Multiple JOINs
			local select6 = builder.select("users.id", "profiles.bio", "orders.amount")
				:from("users")
				:left_join("profiles ON users.id = profiles.user_id")
				:inner_join("orders ON users.id = orders.user_id")
			
			local sql6, args6 = select6:to_sql()
			
			return {
				sql1 = sql1,
				sql2 = sql2, args2 = args2,
				sql3 = sql3,
				sql4 = sql4,
				sql5 = sql5,
				sql6 = sql6
			}
		end
	`, "test", "test_select_joins")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_select_joins")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify basic JOIN
	sql1 := resultTable.RawGetString("sql1").String()
	assert.Equal(t, "SELECT users.id, profiles.bio FROM users JOIN profiles ON users.id = profiles.user_id", sql1)

	// Verify JOIN with placeholder
	sql2 := resultTable.RawGetString("sql2").String()
	args2 := resultTable.RawGetString("args2").(*lua.LTable)
	assert.Equal(t, "SELECT users.id, profiles.bio FROM users JOIN profiles ON users.id = profiles.user_id AND profiles.created_at > ?", sql2)
	assert.Equal(t, 1, args2.Len())

	// Verify LEFT JOIN
	sql3 := resultTable.RawGetString("sql3").String()
	assert.Equal(t, "SELECT users.id, profiles.bio FROM users LEFT JOIN profiles ON users.id = profiles.user_id", sql3)

	// Verify RIGHT JOIN
	sql4 := resultTable.RawGetString("sql4").String()
	assert.Equal(t, "SELECT users.id, profiles.bio FROM users RIGHT JOIN profiles ON users.id = profiles.user_id", sql4)

	// Verify INNER JOIN
	sql5 := resultTable.RawGetString("sql5").String()
	assert.Equal(t, "SELECT users.id, profiles.bio FROM users INNER JOIN profiles ON users.id = profiles.user_id", sql5)

	// Verify multiple JOINs
	sql6 := resultTable.RawGetString("sql6").String()
	assert.Equal(t, "SELECT users.id, profiles.bio, orders.amount FROM users LEFT JOIN profiles ON users.id = profiles.user_id INNER JOIN orders ON users.id = orders.user_id", sql6)
}

// TestSelectGroupBy tests GROUP BY, HAVING, ORDER BY, LIMIT, and OFFSET clauses
func TestSelectGroupBy(t *testing.T) {
	vm, runner, ctx := setupLuaWithSelectModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_select_group_by()
			-- GROUP BY
			local select1 = builder.select("department", "COUNT(*) AS employee_count")
				:from("employees")
				:group_by("department")
			
			local sql1, args1 = select1:to_sql()
			
			-- GROUP BY with HAVING
			local select2 = builder.select("department", "COUNT(*) AS employee_count")
				:from("employees")
				:group_by("department")
				:having("COUNT(*) > ?", 5)
			
			local sql2, args2 = select2:to_sql()
			
			-- HAVING with table and Sqlizer conditions
			local select3 = builder.select("department", "COUNT(*) AS employee_count")
				:from("employees")
				:group_by("department")
				:having({department = "Engineering"})
			
			local sql3, args3 = select3:to_sql()
			
			local select4 = builder.select("department", "COUNT(*) AS employee_count")
				:from("employees")
				:group_by("department")
				:having(builder.gt({["COUNT(*)"]=10}))
			
			local sql4, args4 = select4:to_sql()
			
			-- ORDER BY
			local select5 = builder.select("id", "name")
				:from("users")
				:order_by("name ASC")
			
			local sql5, args5 = select5:to_sql()
			
			-- Multiple ORDER BY
			local select6 = builder.select("id", "name", "created_at")
				:from("users")
				:order_by("created_at DESC", "name ASC")
			
			local sql6, args6 = select6:to_sql()
			
			-- LIMIT and OFFSET
			local select7 = builder.select("id", "name")
				:from("users")
				:order_by("id ASC")
				:limit(10)
				:offset(20)
			
			local sql7, args7 = select7:to_sql()
			
			return {
				sql1 = sql1,
				sql2 = sql2, args2 = args2,
				sql3 = sql3, args3 = args3,
				sql4 = sql4, args4 = args4,
				sql5 = sql5,
				sql6 = sql6,
				sql7 = sql7
			}
		end
	`, "test", "test_select_group_by")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_select_group_by")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify GROUP BY
	sql1 := resultTable.RawGetString("sql1").String()
	assert.Equal(t, "SELECT department, COUNT(*) AS employee_count FROM employees GROUP BY department", sql1)

	// Verify GROUP BY with HAVING
	sql2 := resultTable.RawGetString("sql2").String()
	args2 := resultTable.RawGetString("args2").(*lua.LTable)
	assert.Equal(t, "SELECT department, COUNT(*) AS employee_count FROM employees GROUP BY department HAVING COUNT(*) > ?", sql2)
	assert.Equal(t, 1, args2.Len())

	// Verify HAVING with table condition
	sql3 := resultTable.RawGetString("sql3").String()
	args3 := resultTable.RawGetString("args3").(*lua.LTable)
	assert.Equal(t, "SELECT department, COUNT(*) AS employee_count FROM employees GROUP BY department HAVING department = ?", sql3)
	assert.Equal(t, 1, args3.Len())

	// Verify HAVING with Sqlizer condition
	sql4 := resultTable.RawGetString("sql4").String()
	args4 := resultTable.RawGetString("args4").(*lua.LTable)
	assert.Equal(t, "SELECT department, COUNT(*) AS employee_count FROM employees GROUP BY department HAVING COUNT(*) > ?", sql4)
	assert.Equal(t, 1, args4.Len())

	// Verify ORDER BY
	sql5 := resultTable.RawGetString("sql5").String()
	assert.Equal(t, "SELECT id, name FROM users ORDER BY name ASC", sql5)

	// Verify multiple ORDER BY
	sql6 := resultTable.RawGetString("sql6").String()
	assert.Equal(t, "SELECT id, name, created_at FROM users ORDER BY created_at DESC, name ASC", sql6)

	// Verify LIMIT and OFFSET
	sql7 := resultTable.RawGetString("sql7").String()
	assert.Equal(t, "SELECT id, name FROM users ORDER BY id ASC LIMIT 10 OFFSET 20", sql7)
}

// TestSelectSuffix tests the suffix method
func TestSelectSuffix(t *testing.T) {
	vm, runner, ctx := setupLuaWithSelectModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_select_suffix()
			-- SELECT with suffix
			local select1 = builder.select("id", "name")
				:from("users")
				:suffix("FOR UPDATE")
			
			local sql1, args1 = select1:to_sql()
			
			-- SELECT with parameterized suffix
			local select2 = builder.select("id", "name")
				:from("users")
				:suffix("FOR UPDATE OF ? SKIP LOCKED", "users")
			
			local sql2, args2 = select2:to_sql()
			
			return {
				sql1 = sql1,
				sql2 = sql2, args2 = args2
			}
		end
	`, "test", "test_select_suffix")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_select_suffix")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output with suffix
	sql1 := resultTable.RawGetString("sql1").String()
	assert.Equal(t, "SELECT id, name FROM users FOR UPDATE", sql1)

	// Verify SQL output with parameterized suffix
	sql2 := resultTable.RawGetString("sql2").String()
	args2 := resultTable.RawGetString("args2").(*lua.LTable)
	assert.Equal(t, "SELECT id, name FROM users FOR UPDATE OF ? SKIP LOCKED", sql2)
	assert.Equal(t, 1, args2.Len())
	assert.Equal(t, "users", args2.RawGetInt(1).String())
}

// TestSelectPlaceholderFormat tests different placeholder formats
func TestSelectPlaceholderFormat(t *testing.T) {
	vm, runner, ctx := setupLuaWithSelectModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_select_placeholder_format()
			-- SELECT with dollar placeholder format
			local select1 = builder.select("id", "name")
				:from("users")
				:where("id = ?", 1)
				:placeholder_format(builder.dollar)
			
			local sql1, args1 = select1:to_sql()
			
			-- SELECT with question placeholder format
			local select2 = builder.select("id", "name")
				:from("users")
				:where("id = ?", 1)
				:placeholder_format(builder.question)
			
			local sql2, args2 = select2:to_sql()
			
			return {
				sql_dollar = sql1,
				sql_question = sql2
			}
		end
	`, "test", "test_select_placeholder_format")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_select_placeholder_format")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	sqlDollar := resultTable.RawGetString("sql_dollar").String()
	sqlQuestion := resultTable.RawGetString("sql_question").String()

	assert.Contains(t, sqlDollar, "SELECT id, name FROM users WHERE")
	assert.Contains(t, sqlQuestion, "SELECT id, name FROM users WHERE")

	// Dollar format should use $1, $2, etc.
	assert.Contains(t, sqlDollar, "$")

	// Question format should use ?
	assert.Contains(t, sqlQuestion, "?")
}

// TestSelectToString tests the __tostring metamethod
func TestSelectToString(t *testing.T) {
	vm, runner, ctx := setupLuaWithSelectModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_select_to_string()
			-- Create SELECT builder
			local select = builder.select("id", "name")
				:from("users")
				:where("id = ?", 1)
			
			-- Convert to string using tostring
			local str = tostring(select)
			
			return str
		end
	`, "test", "test_select_to_string")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_select_to_string")
	require.NoError(t, err)
	resultStr := result.(lua.LString)

	// Verify string representation
	assert.Contains(t, string(resultStr), "SelectBuilder")
	assert.Contains(t, string(resultStr), "SELECT id, name FROM users")
}

// TestSelectComplex tests complex SELECT queries
func TestSelectComplex(t *testing.T) {
	vm, runner, ctx := setupLuaWithSelectModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_select_complex()
			-- SELECT with multiple clauses
			local select = builder.select("u.id", "u.name", "p.profile_pic", "COUNT(o.id) AS order_count")
				:from("users u")
				:left_join("profiles p ON u.id = p.user_id")
				:left_join("orders o ON u.id = o.user_id")
				:where(builder.and_({
					builder.gt({["u.created_at"]="2023-01-01"}),
					builder.eq({["u.active"]=true})
				}))
				:group_by("u.id", "u.name", "p.profile_pic")
				:having("COUNT(o.id) > ?", 5)
				:order_by("order_count DESC")
				:limit(10)
				:offset(20)
			
			local sql, args = select:to_sql()
			
			return {
				sql = sql,
				args_count = #args
			}
		end
	`, "test", "test_select_complex")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_select_complex")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output
	sql := resultTable.RawGetString("sql").String()
	argsCount := int(resultTable.RawGetString("args_count").(lua.LNumber))

	assert.Contains(t, sql, "SELECT u.id, u.name, p.profile_pic, COUNT(o.id) AS order_count")
	assert.Contains(t, sql, "FROM users u")
	assert.Contains(t, sql, "LEFT JOIN profiles p ON u.id = p.user_id")
	assert.Contains(t, sql, "LEFT JOIN orders o ON u.id = o.user_id")
	assert.Contains(t, sql, "WHERE (u.created_at > ? AND u.active = ?)") // Note the parentheses
	assert.Contains(t, sql, "GROUP BY u.id, u.name, p.profile_pic")
	assert.Contains(t, sql, "HAVING COUNT(o.id) > ?")
	assert.Contains(t, sql, "ORDER BY order_count DESC")
	assert.Contains(t, sql, "LIMIT 10")
	assert.Contains(t, sql, "OFFSET 20")

	assert.Equal(t, 3, argsCount)
}

// TestSelectWithNullValues tests handling NULL values
func TestSelectWithNullValues(t *testing.T) {
	vm, runner, ctx := setupLuaWithSelectModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_select_with_null()
			-- SELECT with NULL values using SQL_NULL marker
			local select = builder.select("id", "name")
				:from("users")
				:where(builder.eq({["deleted_at"]=SQL_NULL}))
			
			local sql, args = select:to_sql()
			
			-- Check if the SQL structure is correct
			local has_is_null = string.find(sql, "deleted_at IS NULL") ~= nil
			
			return {
				sql = sql,
				args_count = #args,
				has_is_null = has_is_null
			}
		end
	`, "test", "test_select_with_null")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_select_with_null")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output
	sql := resultTable.RawGetString("sql").String()
	assert.Contains(t, sql, "SELECT id, name FROM users WHERE deleted_at IS NULL")

	// Verify args count - should be 0 for IS NULL
	argsCount := int(resultTable.RawGetString("args_count").(lua.LNumber))
	assert.Equal(t, 0, argsCount)

	// Check if the SQL has IS NULL instead of = NULL
	hasIsNull := bool(resultTable.RawGetString("has_is_null").(lua.LBool))
	assert.True(t, hasIsNull)
}

// TestSelectErrors tests error handling
func TestSelectErrors(t *testing.T) {
	vm, runner, ctx := setupLuaWithSelectModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_select_errors()
			-- Empty SELECT (no columns)
			local select1 = builder.select()
			local sql1, err1 = select1:to_sql()

			-- Using bad placeholder format with a non-userdata value
			local select2 = builder.select("id"):from("users")
			local success, err2 = pcall(function()
				select2:placeholder_format("invalid")
			end)

			-- Missing FROM clause (actually valid in SQL but common error source)
			local select3 = builder.select("COUNT(*)")
			local sql3, err3 = select3:to_sql()

			return {
				sql1 = sql1,
				err1 = tostring(err1),
				success = success,
				err2 = err2,
				sql3 = sql3
			}
		end
	`, "test", "test_select_errors")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_select_errors")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify error handling for empty SELECT
	sql1 := resultTable.RawGetString("sql1")
	err1 := resultTable.RawGetString("err1")
	assert.Equal(t, lua.LNil, sql1)
	assert.Contains(t, err1.String(), "select statements must have at least one result column")

	// Verify error handling for invalid placeholder
	success := lua.LVAsBool(resultTable.RawGetString("success"))
	err2 := resultTable.RawGetString("err2").String()
	assert.False(t, success)
	assert.Contains(t, err2, "bad argument #2 to placeholder_format")

	// Missing FROM clause is actually valid in SQL (for things like SELECT NOW())
	sql3 := resultTable.RawGetString("sql3").String()
	assert.Equal(t, "SELECT COUNT(*)", sql3)
}
