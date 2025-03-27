package builder

//
//import (
//	"testing"
//
//	"github.com/stretchr/testify/assert"
//	"github.com/stretchr/testify/require"
//	lua "github.com/yuin/gopher-lua"
//)
//
//// TestExprEq tests the eq (equality) expression builder
//func TestExprEq(t *testing.T) {
//	vm, L, runner := setupLuaWithBuilder(t)
//	defer vm.Close()
//
//	// Import test script
//	script := `
//		function test_expr_eq()
//			-- Create an equality expression
//			local expr = sql.builder.eq({
//				id = 123,
//				name = "test",
//				active = true
//			})
//
//			-- Get the SQL and args
//			local sql_str, args = expr:to_sql()
//
//			return {
//				sql = sql_str,
//				args = args
//			}
//		end
//	`
//
//	err := vm.Import(script, "test", "test_expr_eq")
//	require.NoError(t, err, "Failed to import script")
//
//	// Execute the function
//	result, err := runner.Execute(L.Context(), "test_expr_eq")
//	require.NoError(t, err, "Lua execution failed")
//
//	// Verify results
//	resultTable, ok := result.(*lua.LTable)
//	require.True(t, ok, "Expected table result")
//
//	// Check SQL
//	sqlStr := resultTable.RawGetString("sql").(lua.LString)
//	assert.Contains(t, string(sqlStr), "id = ?", "SQL should contain field equality")
//	assert.Contains(t, string(sqlStr), "name = ?", "SQL should contain field equality")
//	assert.Contains(t, string(sqlStr), "active = ?", "SQL should contain field equality")
//
//	// Check args
//	argsTable := resultTable.RawGetString("args").(*lua.LTable)
//	assert.Equal(t, 3, argsTable.Len(), "Should have 3 args")
//}
//
//// TestExprComparisons tests various comparison expressions
//func TestExprComparisons(t *testing.T) {
//	vm, L, runner := setupLuaWithBuilder(t)
//	defer vm.Close()
//
//	// Import test script
//	script := `
//		function test_expr_comparisons()
//			local results = {}
//
//			-- Test lt (less than)
//			local lt = sql.builder.lt({age = 18})
//			local lt_sql, lt_args = lt:to_sql()
//			results.lt = {sql = lt_sql, args = lt_args}
//
//			-- Test lte (less than or equal)
//			local lte = sql.builder.lte({age = 18})
//			local lte_sql, lte_args = lte:to_sql()
//			results.lte = {sql = lte_sql, args = lte_args}
//
//			-- Test gt (greater than)
//			local gt = sql.builder.gt({age = 65})
//			local gt_sql, gt_args = gt:to_sql()
//			results.gt = {sql = gt_sql, args = gt_args}
//
//			-- Test gte (greater than or equal)
//			local gte = sql.builder.gte({age = 65})
//			local gte_sql, gte_args = gte:to_sql()
//			results.gte = {sql = gte_sql, args = gte_args}
//
//			-- Test not_eq (not equal)
//			local not_eq = sql.builder.not_eq({status = "inactive"})
//			local not_eq_sql, not_eq_args = not_eq:to_sql()
//			results.not_eq = {sql = not_eq_sql, args = not_eq_args}
//
//			return results
//		end
//	`
//
//	err := vm.Import(script, "test", "test_expr_comparisons")
//	require.NoError(t, err, "Failed to import script")
//
//	// Execute the function
//	result, err := runner.Execute(L.Context(), "test_expr_comparisons")
//	require.NoError(t, err, "Lua execution failed")
//
//	// Verify results
//	resultTable, ok := result.(*lua.LTable)
//	require.True(t, ok, "Expected table result")
//
//	// Check lt (less than)
//	ltTable := resultTable.RawGetString("lt").(*lua.LTable)
//	ltSql := ltTable.RawGetString("sql").(lua.LString)
//	assert.Contains(t, string(ltSql), "age < ?", "LT SQL should use < operator")
//
//	// Check lte (less than or equal)
//	lteTable := resultTable.RawGetString("lte").(*lua.LTable)
//	lteSql := lteTable.RawGetString("sql").(lua.LString)
//	assert.Contains(t, string(lteSql), "age <= ?", "LTE SQL should use <= operator")
//
//	// Check gt (greater than)
//	gtTable := resultTable.RawGetString("gt").(*lua.LTable)
//	gtSql := gtTable.RawGetString("sql").(lua.LString)
//	assert.Contains(t, string(gtSql), "age > ?", "GT SQL should use > operator")
//
//	// Check gte (greater than or equal)
//	gteTable := resultTable.RawGetString("gte").(*lua.LTable)
//	gteSql := gteTable.RawGetString("sql").(lua.LString)
//	assert.Contains(t, string(gteSql), "age >= ?", "GTE SQL should use >= operator")
//
//	// Check not_eq (not equal)
//	notEqTable := resultTable.RawGetString("not_eq").(*lua.LTable)
//	notEqSql := notEqTable.RawGetString("sql").(lua.LString)
//	assert.Contains(t, string(notEqSql), "status <> ?", "NOT_EQ SQL should use <> operator")
//}
//
//// TestExprLike tests LIKE expressions
//func TestExprLike(t *testing.T) {
//	vm, L, runner := setupLuaWithBuilder(t)
//	defer vm.Close()
//
//	// Import test script
//	script := `
//		function test_expr_like()
//			local results = {}
//
//			-- Test like
//			local like = sql.builder.like({name = "John%"})
//			local like_sql, like_args = like:to_sql()
//			results.like = {sql = like_sql, args = like_args}
//
//			-- Test not_like
//			local not_like = sql.builder.not_like({email = "%test.com"})
//			local not_like_sql, not_like_args = not_like:to_sql()
//			results.not_like = {sql = not_like_sql, args = not_like_args}
//
//			return results
//		end
//	`
//
//	err := vm.Import(script, "test", "test_expr_like")
//	require.NoError(t, err, "Failed to import script")
//
//	// Execute the function
//	result, err := runner.Execute(L.Context(), "test_expr_like")
//	require.NoError(t, err, "Lua execution failed")
//
//	// Verify results
//	resultTable, ok := result.(*lua.LTable)
//	require.True(t, ok, "Expected table result")
//
//	// Check like
//	likeTable := resultTable.RawGetString("like").(*lua.LTable)
//	likeSql := likeTable.RawGetString("sql").(lua.LString)
//	assert.Contains(t, string(likeSql), "name LIKE ?", "LIKE SQL should use LIKE operator")
//
//	// Check not_like
//	notLikeTable := resultTable.RawGetString("not_like").(*lua.LTable)
//	notLikeSql := notLikeTable.RawGetString("sql").(lua.LString)
//	assert.Contains(t, string(notLikeSql), "email NOT LIKE ?", "NOT_LIKE SQL should use NOT LIKE operator")
//}
//
//// TestExprLogical tests logical AND and OR expressions
//func TestExprLogical(t *testing.T) {
//	vm, L, runner := setupLuaWithBuilder(t)
//	defer vm.Close()
//
//	// Import test script
//	script := `
//		function test_expr_logical()
//			local results = {}
//
//			-- Test AND
//			local and_expr = sql.builder.and({
//				sql.builder.eq({status = "active"}),
//				sql.builder.gt({age = 18})
//			})
//			local and_sql, and_args = and_expr:to_sql()
//			results.and_expr = {sql = and_sql, args = and_args}
//
//			-- Test OR
//			local or_expr = sql.builder.or({
//				sql.builder.eq({type = "admin"}),
//				sql.builder.eq({type = "moderator"})
//			})
//			local or_sql, or_args = or_expr:to_sql()
//			results.or_expr = {sql = or_sql, args = or_args}
//
//			-- Test complex nested expression
//			local complex = sql.builder.and({
//				sql.builder.eq({active = true}),
//				sql.builder.or({
//					sql.builder.eq({role = "admin"}),
//					sql.builder.gt({karma = 5000})
//				})
//			})
//			local complex_sql, complex_args = complex:to_sql()
//			results.complex = {sql = complex_sql, args = complex_args}
//
//			return results
//		end
//	`
//
//	err := vm.Import(script, "test", "test_expr_logical")
//	require.NoError(t, err, "Failed to import script")
//
//	// Execute the function
//	result, err := runner.Execute(L.Context(), "test_expr_logical")
//	require.NoError(t, err, "Lua execution failed")
//
//	// Verify results
//	resultTable, ok := result.(*lua.LTable)
//	require.True(t, ok, "Expected table result")
//
//	// Check AND
//	andTable := resultTable.RawGetString("and_expr").(*lua.LTable)
//	andSql := andTable.RawGetString("sql").(lua.LString)
//	assert.Contains(t, string(andSql), "AND", "AND SQL should use AND operator")
//	assert.Contains(t, string(andSql), "status = ?", "AND SQL should contain first condition")
//	assert.Contains(t, string(andSql), "age > ?", "AND SQL should contain second condition")
//
//	// Check OR
//	orTable := resultTable.RawGetString("or_expr").(*lua.LTable)
//	orSql := orTable.RawGetString("sql").(lua.LString)
//	assert.Contains(t, string(orSql), "OR", "OR SQL should use OR operator")
//	assert.Contains(t, string(orSql), "type = ?", "OR SQL should contain conditions")
//
//	// Check complex expression
//	complexTable := resultTable.RawGetString("complex").(*lua.LTable)
//	complexSql := complexTable.RawGetString("sql").(lua.LString)
//	assert.Contains(t, string(complexSql), "AND", "Complex SQL should use AND operator")
//	assert.Contains(t, string(complexSql), "OR", "Complex SQL should use OR operator")
//	assert.Contains(t, string(complexSql), "(", "Complex SQL should use parentheses for grouping")
//	assert.Contains(t, string(complexSql), ")", "Complex SQL should use parentheses for grouping")
//}
//
//// TestExprRaw tests raw SQL expressions
//func TestExprRaw(t *testing.T) {
//	vm, L, runner := setupLuaWithBuilder(t)
//	defer vm.Close()
//
//	// Import test script
//	script := `
//		function test_expr_raw()
//			-- Test expr for raw SQL
//			local raw = sql.builder.expr("CONCAT(first_name, ' ', last_name)")
//			local raw_sql, raw_args = raw:to_sql()
//
//			-- Test expr with args
//			local with_args = sql.builder.expr("COALESCE(?, ?)", nil, "default")
//			local with_args_sql, with_args_args = with_args:to_sql()
//
//			return {
//				raw = {sql = raw_sql, args = raw_args},
//				with_args = {sql = with_args_sql, args = with_args_args}
//			}
//		end
//	`
//
//	err := vm.Import(script, "test", "test_expr_raw")
//	require.NoError(t, err, "Failed to import script")
//
//	// Execute the function
//	result, err := runner.Execute(L.Context(), "test_expr_raw")
//	require.NoError(t, err, "Lua execution failed")
//
//	// Verify results
//	resultTable, ok := result.(*lua.LTable)
//	require.True(t, ok, "Expected table result")
//
//	// Check raw expression
//	rawTable := resultTable.RawGetString("raw").(*lua.LTable)
//	rawSql := rawTable.RawGetString("sql").(lua.LString)
//	assert.Equal(t, "CONCAT(first_name, ' ', last_name)", string(rawSql), "Raw SQL should be preserved exactly")
//
//	// Check expression with args
//	withArgsTable := resultTable.RawGetString("with_args").(*lua.LTable)
//	withArgsSql := withArgsTable.RawGetString("sql").(lua.LString)
//	withArgsArgs := withArgsTable.RawGetString("args").(*lua.LTable)
//
//	assert.Equal(t, "COALESCE(?, ?)", string(withArgsSql), "Parameterized SQL should be preserved")
//	assert.Equal(t, 2, withArgsArgs.Len(), "Should have 2 args")
//}
//
//// TestExprInSelect tests using expressions in a SELECT statement
//func TestExprInSelect(t *testing.T) {
//	vm, L, runner := setupLuaWithBuilder(t)
//	defer vm.Close()
//
//	// Import test script
//	script := `
//		function test_expr_in_select()
//			-- Create a SELECT statement with various expressions
//			local select = sql.builder.select("id", "name")
//				:from("users")
//				:where(sql.builder.and({
//					sql.builder.eq({active = true}),
//					sql.builder.or({
//						sql.builder.like({name = "A%"}),
//						sql.builder.like({email = "%@example.com"})
//					})
//				}))
//				:order_by("created_at DESC")
//				:limit(10)
//
//			-- Get the SQL and args
//			local sql_str, args = select:to_sql()
//
//			return {
//				sql = sql_str,
//				args_count = #args
//			}
//		end
//	`
//
//	err := vm.Import(script, "test", "test_expr_in_select")
//	require.NoError(t, err, "Failed to import script")
//
//	// Execute the function
//	result, err := runner.Execute(L.Context(), "test_expr_in_select")
//	require.NoError(t, err, "Lua execution failed")
//
//	// Verify results
//	resultTable, ok := result.(*lua.LTable)
//	require.True(t, ok, "Expected table result")
//
//	// Check the SQL contains all the expected parts
//	sqlStr := resultTable.RawGetString("sql").(lua.LString)
//	argsCount := resultTable.RawGetString("args_count").(lua.LNumber)
//
//	assert.Contains(t, string(sqlStr), "SELECT", "SQL should be a SELECT statement")
//	assert.Contains(t, string(sqlStr), "FROM users", "SQL should select from users table")
//	assert.Contains(t, string(sqlStr), "WHERE", "SQL should have a WHERE clause")
//	assert.Contains(t, string(sqlStr), "AND", "SQL should use AND")
//	assert.Contains(t, string(sqlStr), "OR", "SQL should use OR")
//	assert.Contains(t, string(sqlStr), "LIKE", "SQL should use LIKE")
//	assert.Contains(t, string(sqlStr), "ORDER BY", "SQL should have ORDER BY")
//	assert.Contains(t, string(sqlStr), "LIMIT", "SQL should have LIMIT")
//
//	// Check we have multiple args (3: active=true, name LIKE, email LIKE)
//	assert.Equal(t, float64(3), float64(argsCount), "Should have 3 args")
//}
