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

func setupLuaWithExprModule(t *testing.T) (*engine.CoroutineVM, *engine.Runner, context.Context) {
	logger := zaptest.NewLogger(t)

	// Create a VM with coroutine support
	vm, err := engine.NewCVM(logger)
	require.NoError(t, err)

	// Get the Lua state
	L := vm.State()

	// Create a table and register expression builders
	modTable := L.CreateTable(0, 20)
	registerExpressionBuilders(L, modTable)

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

// TestRawExpr tests the raw expression builder
func TestRawExpr(t *testing.T) {
	vm, runner, ctx := setupLuaWithExprModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_raw_expr()
			-- Basic expression with a single parameter
			local expr1 = builder.expr("col = ?", 42)
			local sql1, args1 = expr1:to_sql()
			
			-- Expression with multiple parameters
			local expr2 = builder.expr("col BETWEEN ? AND ?", 10, 20)
			local sql2, args2 = expr2:to_sql()
			
			-- Expression with nil parameter
			local expr3 = builder.expr("col IS ?", nil)
			local sql3, args3 = expr3:to_sql()
			
			-- Expression with no parameters
			local expr4 = builder.expr("1=1")
			local sql4, args4 = expr4:to_sql()
			
			return {
				sql1 = sql1, args1 = args1,
				sql2 = sql2, args2 = args2,
				sql3 = sql3, args3 = args3,
				sql4 = sql4, args4 = args4
			}
		end
	`, "test", "test_raw_expr")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_raw_expr")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output for expressions
	assert.Equal(t, "col = ?", resultTable.RawGetString("sql1").String())
	assert.Equal(t, "col BETWEEN ? AND ?", resultTable.RawGetString("sql2").String())
	assert.Equal(t, "col IS ?", resultTable.RawGetString("sql3").String())
	assert.Equal(t, "1=1", resultTable.RawGetString("sql4").String())

	// Verify parameters
	args1 := resultTable.RawGetString("args1").(*lua.LTable)
	assert.Equal(t, float64(42), float64(args1.RawGetInt(1).(lua.LNumber)))

	args2 := resultTable.RawGetString("args2").(*lua.LTable)
	assert.Equal(t, float64(10), float64(args2.RawGetInt(1).(lua.LNumber)))
	assert.Equal(t, float64(20), float64(args2.RawGetInt(2).(lua.LNumber)))

	args3 := resultTable.RawGetString("args3").(*lua.LTable)
	assert.Equal(t, lua.LNil, args3.RawGetInt(1))

	args4 := resultTable.RawGetString("args4").(*lua.LTable)
	assert.Equal(t, 0, args4.Len())
}

// TestEqExpr tests the equality expression builder
func TestEqExpr(t *testing.T) {
	vm, runner, ctx := setupLuaWithExprModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_eq()
			-- Single equality condition
			local eq1 = builder.eq({id = 1})
			local sql1, args1 = eq1:to_sql()
			
			-- Multiple equality conditions
			local eq2 = builder.eq({id = 2, name = "test", active = true})
			local sql2, args2 = eq2:to_sql()
			
			-- Equality with NULL value (IS NULL)
			local eq3 = builder.eq({id = SQL_NULL})
			local sql3, args3 = eq3:to_sql()
			
			-- Empty equality map (always true)
			local eq4 = builder.eq({})
			local sql4, args4 = eq4:to_sql()
			
			return {
				sql1 = sql1, args1 = args1,
				sql2 = sql2, args2 = args2,
				sql3 = sql3, args3 = args3,
				sql4 = sql4, args4 = args4
			}
		end
	`, "test", "test_eq")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_eq")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output
	assert.Equal(t, "id = ?", resultTable.RawGetString("sql1").String())

	sql2 := resultTable.RawGetString("sql2").String()
	assert.Contains(t, sql2, "id = ?")
	assert.Contains(t, sql2, "name = ?")
	assert.Contains(t, sql2, "active = ?")

	assert.Equal(t, "id IS NULL", resultTable.RawGetString("sql3").String())
	assert.Equal(t, "(1=1)", resultTable.RawGetString("sql4").String())

	// Verify parameters
	args1 := resultTable.RawGetString("args1").(*lua.LTable)
	assert.Equal(t, float64(1), float64(args1.RawGetInt(1).(lua.LNumber)))

	args2 := resultTable.RawGetString("args2").(*lua.LTable)
	assert.Equal(t, 3, args2.Len())
}

// TestNotEqExpr tests the inequality expression builder
func TestNotEqExpr(t *testing.T) {
	vm, runner, ctx := setupLuaWithExprModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_not_eq()
			-- Single inequality condition
			local neq1 = builder.not_eq({id = 1})
			local sql1, args1 = neq1:to_sql()
			
			-- Multiple inequality conditions
			local neq2 = builder.not_eq({id = 2, name = "test"})
			local sql2, args2 = neq2:to_sql()
			
			-- Inequality with NULL value (IS NOT NULL)
			local neq3 = builder.not_eq({id = SQL_NULL})
			local sql3, args3 = neq3:to_sql()
			
			-- Empty inequality map
			local neq4 = builder.not_eq({})
			local sql4, args4 = neq4:to_sql()
			
			return {
				sql1 = sql1, args1 = args1,
				sql2 = sql2, args2 = args2,
				sql3 = sql3, args3 = args3,
				sql4 = sql4, args4 = args4
			}
		end
	`, "test", "test_not_eq")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_not_eq")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output
	assert.Equal(t, "id <> ?", resultTable.RawGetString("sql1").String())

	sql2 := resultTable.RawGetString("sql2").String()
	assert.Contains(t, sql2, "id <> ?")
	assert.Contains(t, sql2, "name <> ?")

	assert.Equal(t, "id IS NOT NULL", resultTable.RawGetString("sql3").String())
	assert.Equal(t, "(1=1)", resultTable.RawGetString("sql4").String())

	// Verify parameters
	args1 := resultTable.RawGetString("args1").(*lua.LTable)
	assert.Equal(t, float64(1), float64(args1.RawGetInt(1).(lua.LNumber)))

	args2 := resultTable.RawGetString("args2").(*lua.LTable)
	assert.Equal(t, 2, args2.Len())
}

// TestComparisonExpr tests the comparison expression builders (lt, lte, gt, gte)
func TestComparisonExpr(t *testing.T) {
	vm, runner, ctx := setupLuaWithExprModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_comparison()
			-- Less than
			local lt = builder.lt({id = 10, age = 30})
			local lt_sql, lt_args = lt:to_sql()
			
			-- Less than or equal
			local lte = builder.lte({id = 20})
			local lte_sql, lte_args = lte:to_sql()
			
			-- Greater than
			local gt = builder.gt({id = 30})
			local gt_sql, gt_args = gt:to_sql()
			
			-- Greater than or equal
			local gte = builder.gte({id = 40})
			local gte_sql, gte_args = gte:to_sql()
			
			return {
				lt_sql = lt_sql, lt_args = lt_args,
				lte_sql = lte_sql, lte_args = lte_args,
				gt_sql = gt_sql, gt_args = gt_args,
				gte_sql = gte_sql, gte_args = gte_args
			}
		end
	`, "test", "test_comparison")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_comparison")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output
	ltSQL := resultTable.RawGetString("lt_sql").String()
	assert.Contains(t, ltSQL, "id < ?")
	assert.Contains(t, ltSQL, "age < ?")

	assert.Equal(t, "id <= ?", resultTable.RawGetString("lte_sql").String())
	assert.Equal(t, "id > ?", resultTable.RawGetString("gt_sql").String())
	assert.Equal(t, "id >= ?", resultTable.RawGetString("gte_sql").String())

	// Verify parameters
	ltArgs := resultTable.RawGetString("lt_args").(*lua.LTable)
	assert.Equal(t, 2, ltArgs.Len())

	lteArgs := resultTable.RawGetString("lte_args").(*lua.LTable)
	assert.Equal(t, float64(20), float64(lteArgs.RawGetInt(1).(lua.LNumber)))

	gtArgs := resultTable.RawGetString("gt_args").(*lua.LTable)
	assert.Equal(t, float64(30), float64(gtArgs.RawGetInt(1).(lua.LNumber)))

	gteArgs := resultTable.RawGetString("gte_args").(*lua.LTable)
	assert.Equal(t, float64(40), float64(gteArgs.RawGetInt(1).(lua.LNumber)))
}

// TestLikeExpr tests the LIKE and NOT LIKE expression builders
func TestLikeExpr(t *testing.T) {
	vm, runner, ctx := setupLuaWithExprModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_like()
			-- LIKE with one pattern
			local like1 = builder.like({name = "test%"})
			local like1_sql, like1_args = like1:to_sql()
			
			-- LIKE with multiple patterns
			local like2 = builder.like({name = "a%", email = "%example.com"})
			local like2_sql, like2_args = like2:to_sql()
			
			-- NOT LIKE
			local not_like1 = builder.not_like({name = "test%"})
			local not_like1_sql, not_like1_args = not_like1:to_sql()
			
			-- NOT LIKE with multiple patterns
			local not_like2 = builder.not_like({name = "a%", email = "%example.com"})
			local not_like2_sql, not_like2_args = not_like2:to_sql()
			
			return {
				like1_sql = like1_sql, like1_args = like1_args,
				like2_sql = like2_sql, like2_args = like2_args,
				not_like1_sql = not_like1_sql, not_like1_args = not_like1_args,
				not_like2_sql = not_like2_sql, not_like2_args = not_like2_args
			}
		end
	`, "test", "test_like")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_like")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output
	assert.Equal(t, "name LIKE ?", resultTable.RawGetString("like1_sql").String())

	like2SQL := resultTable.RawGetString("like2_sql").String()
	assert.Contains(t, like2SQL, "name LIKE ?")
	assert.Contains(t, like2SQL, "email LIKE ?")

	assert.Equal(t, "name NOT LIKE ?", resultTable.RawGetString("not_like1_sql").String())

	notLike2SQL := resultTable.RawGetString("not_like2_sql").String()
	assert.Contains(t, notLike2SQL, "name NOT LIKE ?")
	assert.Contains(t, notLike2SQL, "email NOT LIKE ?")

	// Verify parameters
	like1Args := resultTable.RawGetString("like1_args").(*lua.LTable)
	assert.Equal(t, "test%", like1Args.RawGetInt(1).String())

	like2Args := resultTable.RawGetString("like2_args").(*lua.LTable)
	assert.Equal(t, 2, like2Args.Len())

	notLike1Args := resultTable.RawGetString("not_like1_args").(*lua.LTable)
	assert.Equal(t, "test%", notLike1Args.RawGetInt(1).String())

	notLike2Args := resultTable.RawGetString("not_like2_args").(*lua.LTable)
	assert.Equal(t, 2, notLike2Args.Len())
}

// TestLogicalExpr tests the AND and OR logical expression builders
func TestLogicalExpr(t *testing.T) {
	vm, runner, ctx := setupLuaWithExprModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_logical()
			-- AND with multiple conditions
			local and1 = builder.and_({
				builder.eq({id = 1}),
				builder.eq({active = true})
			})
			local and1_sql, and1_args = and1:to_sql()
			
			-- Empty AND (evaluates to true)
			local and2 = builder.and_({})
			local and2_sql, and2_args = and2:to_sql()
			
			-- OR with multiple conditions
			local or1 = builder.or_({
				builder.eq({id = 1}),
				builder.eq({id = 2})
			})
			local or1_sql, or1_args = or1:to_sql()
			
			-- Empty OR (evaluates to false)
			local or2 = builder.or_({})
			local or2_sql, or2_args = or2:to_sql()
			
			-- Nested conditions
			local nested = builder.and_({
				builder.or_({
					builder.eq({status = "active"}),
					builder.eq({status = "pending"})
				}),
				builder.lt({created_at = "2023-01-01"})
			})
			local nested_sql, nested_args = nested:to_sql()
			
			return {
				and1_sql = and1_sql, and1_args = and1_args,
				and2_sql = and2_sql, and2_args = and2_args,
				or1_sql = or1_sql, or1_args = or1_args,
				or2_sql = or2_sql, or2_args = or2_args,
				nested_sql = nested_sql, nested_args = nested_args
			}
		end
	`, "test", "test_logical")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_logical")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify SQL output
	and1SQL := resultTable.RawGetString("and1_sql").String()
	assert.Contains(t, and1SQL, "(id = ? AND active = ?)")

	assert.Equal(t, "(1=1)", resultTable.RawGetString("and2_sql").String())

	or1SQL := resultTable.RawGetString("or1_sql").String()
	assert.Contains(t, or1SQL, "(id = ? OR id = ?)")

	assert.Equal(t, "(1=0)", resultTable.RawGetString("or2_sql").String())

	nestedSQL := resultTable.RawGetString("nested_sql").String()
	assert.Contains(t, nestedSQL, "((status = ? OR status = ?) AND created_at < ?)")

	// Verify parameters
	and1Args := resultTable.RawGetString("and1_args").(*lua.LTable)
	assert.Equal(t, 2, and1Args.Len())

	and2Args := resultTable.RawGetString("and2_args").(*lua.LTable)
	assert.Equal(t, 0, and2Args.Len())

	or1Args := resultTable.RawGetString("or1_args").(*lua.LTable)
	assert.Equal(t, 2, or1Args.Len())

	or2Args := resultTable.RawGetString("or2_args").(*lua.LTable)
	assert.Equal(t, 0, or2Args.Len())

	nestedArgs := resultTable.RawGetString("nested_args").(*lua.LTable)
	assert.Equal(t, 3, nestedArgs.Len())
}

// TestSqlizerToString tests the __tostring metamethod for Sqlizer objects
func TestSqlizerToString(t *testing.T) {
	vm, runner, ctx := setupLuaWithExprModule(t)
	defer vm.Close()

	err := vm.Import(`
		function test_sqlizer_tostring()
			-- Create expression objects
			local expr1 = builder.expr("id = ?", 1)
			local expr2 = builder.eq({name = "test"})
			local expr3 = builder.and_({
				builder.eq({id = 1}),
				builder.eq({active = true})
			})
			
			-- Convert to strings
			local str1 = tostring(expr1)
			local str2 = tostring(expr2)
			local str3 = tostring(expr3)
			
			return {
				str1 = str1,
				str2 = str2,
				str3 = str3
			}
		end
	`, "test", "test_sqlizer_tostring")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_sqlizer_tostring")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	// Verify string representations
	str1 := resultTable.RawGetString("str1").String()
	assert.Contains(t, str1, "SQL:")
	assert.Contains(t, str1, "id = ?")
	assert.Contains(t, str1, "Args: [1]")

	str2 := resultTable.RawGetString("str2").String()
	assert.Contains(t, str2, "SQL:")
	assert.Contains(t, str2, "name = ?")
	assert.Contains(t, str2, "Args: [test]")

	str3 := resultTable.RawGetString("str3").String()
	assert.Contains(t, str3, "SQL:")
	assert.Contains(t, str3, "AND")
	assert.Contains(t, str3, "Args:")
}
