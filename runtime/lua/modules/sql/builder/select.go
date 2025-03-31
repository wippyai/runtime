package builder

import (
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	luaconv "github.com/ponyruntime/pony/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
)

// selectBuilderWrapper wraps a Squirrel SelectBuilder
type selectBuilderWrapper struct {
	builder squirrel.SelectBuilder
}

// builderSelect creates a new select builder
// Usage: sql.builder.select("id", "name", "email")
func builderSelect(l *lua.LState) int {
	// Get columns from arguments
	columns := make([]string, 0, l.GetTop())

	for i := 1; i <= l.GetTop(); i++ {
		if l.Get(i).Type() != lua.LTString {
			l.ArgError(i, "expected string column name")
			return 0
		}
		columns = append(columns, l.CheckString(i))
	}

	// Create wrapper with default placeholder format
	wrapper := &selectBuilderWrapper{
		builder: squirrel.Select(columns...).PlaceholderFormat(squirrel.Question),
	}

	// Create userdata
	ud := wrapSelectBuilder(l, wrapper)
	l.Push(ud)
	return 1
}

// wrapSelectBuilder wraps a SelectBuilder in a Lua userdata
func wrapSelectBuilder(l *lua.LState, wrapper *selectBuilderWrapper) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = wrapper
	ud.Metatable = value.GetTypeMetatable(l, "sql.SelectBuilder")
	return ud
}

// registerSelectBuilderType registers the SelectBuilder metatable
func registerSelectBuilderType(l *lua.LState) {
	// Define methods
	methods := map[string]lua.LGFunction{
		"from":               selectFrom,
		"join":               selectJoin,
		"left_join":          selectLeftJoin,
		"right_join":         selectRightJoin,
		"inner_join":         selectInnerJoin,
		"where":              selectWhere,
		"order_by":           selectOrderBy,
		"group_by":           selectGroupBy,
		"having":             selectHaving,
		"limit":              selectLimit,
		"offset":             selectOffset,
		"columns":            selectColumns,
		"distinct":           selectDistinct,
		"suffix":             selectSuffix,
		"placeholder_format": selectPlaceholderFormat,
		"to_sql":             selectToSql,
		"run_with":           selectRunWith,
	}

	// Define metamethods
	metamethods := map[string]lua.LGFunction{
		"__tostring": selectToString,
	}

	// Register the metatable
	value.RegisterTypeMethods(l, "sql.SelectBuilder", metamethods, methods)
}

// selectToString is the __tostring metamethod
func selectToString(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	query, args, err := wrapper.builder.ToSql()
	if err != nil {
		l.Push(lua.LString(fmt.Sprintf("SelectBuilder Error: %v", err)))
		return 1
	}

	l.Push(lua.LString(fmt.Sprintf("SelectBuilder: %s [Args: %v]", query, args)))
	return 1
}

// checkSelectBuilder ensures the argument is a SelectBuilder and returns it
func checkSelectBuilder(l *lua.LState) *selectBuilderWrapper {
	ud := l.CheckUserData(1)
	if wrapper, ok := ud.Value.(*selectBuilderWrapper); ok {
		return wrapper
	}
	l.ArgError(1, "expected SelectBuilder object")
	return nil
}

// Query building methods (all return a new builder instance)

// selectFrom sets the FROM clause
// Usage: builder = builder:from("users")
func selectFrom(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	table := l.CheckString(2)

	// Create new wrapper with updated builder (immutability)
	newWrapper := &selectBuilderWrapper{
		builder: wrapper.builder.From(table),
	}

	// Return new wrapped builder
	ud := wrapSelectBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// selectWhere adds a WHERE condition
// Usage: builder = builder:where("id > ?", 100) or builder:where({id = 1})
func selectWhere(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Updated builder to store result
	var newBuilder squirrel.SelectBuilder

	switch l.Get(2).Type() {
	case lua.LTString:
		// String condition with args: where("id > ?", 100)
		condition := l.CheckString(2)
		args := make([]interface{}, 0, l.GetTop()-2)
		for i := 3; i <= l.GetTop(); i++ {
			args = append(args, luaconv.ToGoAny(l.Get(i)))
		}
		newBuilder = wrapper.builder.Where(condition, args...)

	case lua.LTTable:
		// Table condition: where({active = true})
		table := l.CheckTable(2)
		eqMap := luaTableToMap(l, table)
		newBuilder = wrapper.builder.Where(squirrel.Eq(eqMap))

	case lua.LTUserData:
		// Sqlizer condition: where(sql.builder.eq({...}))
		ud := l.CheckUserData(2)
		if sqlizer, ok := ud.Value.(squirrel.Sqlizer); ok {
			newBuilder = wrapper.builder.Where(sqlizer)
		} else {
			l.ArgError(2, "expected string, table, or Sqlizer")
			return 0
		}

	default:
		l.ArgError(2, "expected string, table, or Sqlizer")
		return 0
	}

	// Create new wrapper
	newWrapper := &selectBuilderWrapper{builder: newBuilder}

	// Return new wrapped builder
	ud := wrapSelectBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// selectJoin adds a JOIN clause
// Usage: builder = builder:join("emails USING (email_id)")
func selectJoin(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	join := l.CheckString(2)

	// Handle optional args
	args := make([]interface{}, 0, l.GetTop()-2)
	for i := 3; i <= l.GetTop(); i++ {
		args = append(args, luaconv.ToGoAny(l.Get(i)))
	}

	// Create new wrapper
	newWrapper := &selectBuilderWrapper{
		builder: wrapper.builder.Join(join, args...),
	}

	// Return new wrapped builder
	ud := wrapSelectBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// selectLeftJoin adds a LEFT JOIN clause
// Usage: builder = builder:left_join("emails USING (email_id)")
func selectLeftJoin(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	join := l.CheckString(2)

	// Handle optional args
	args := make([]interface{}, 0, l.GetTop()-2)
	for i := 3; i <= l.GetTop(); i++ {
		args = append(args, luaconv.ToGoAny(l.Get(i)))
	}

	// Create new wrapper
	newWrapper := &selectBuilderWrapper{
		builder: wrapper.builder.LeftJoin(join, args...),
	}

	// Return new wrapped builder
	ud := wrapSelectBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// selectRightJoin adds a RIGHT JOIN clause
// Usage: builder = builder:right_join("emails USING (email_id)")
func selectRightJoin(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	join := l.CheckString(2)

	// Handle optional args
	args := make([]interface{}, 0, l.GetTop()-2)
	for i := 3; i <= l.GetTop(); i++ {
		args = append(args, luaconv.ToGoAny(l.Get(i)))
	}

	// Create new wrapper
	newWrapper := &selectBuilderWrapper{
		builder: wrapper.builder.RightJoin(join, args...),
	}

	// Return new wrapped builder
	ud := wrapSelectBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// selectInnerJoin adds an INNER JOIN clause
// Usage: builder = builder:inner_join("emails USING (email_id)")
func selectInnerJoin(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	join := l.CheckString(2)

	// Handle optional args
	args := make([]interface{}, 0, l.GetTop()-2)
	for i := 3; i <= l.GetTop(); i++ {
		args = append(args, luaconv.ToGoAny(l.Get(i)))
	}

	// Create new wrapper
	newWrapper := &selectBuilderWrapper{
		builder: wrapper.builder.InnerJoin(join, args...),
	}

	// Return new wrapped builder
	ud := wrapSelectBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// selectOrderBy adds an ORDER BY clause
// Usage: builder = builder:order_by("name ASC", "id DESC")
func selectOrderBy(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Collect all order by clauses
	orderBys := make([]string, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		orderBys = append(orderBys, l.CheckString(i))
	}

	// Create new wrapper
	newWrapper := &selectBuilderWrapper{
		builder: wrapper.builder.OrderBy(orderBys...),
	}

	// Return new wrapped builder
	ud := wrapSelectBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// selectGroupBy adds a GROUP BY clause
// Usage: builder = builder:group_by("department", "location")
func selectGroupBy(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Collect all group by clauses
	groupBys := make([]string, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		groupBys = append(groupBys, l.CheckString(i))
	}

	// Create new wrapper
	newWrapper := &selectBuilderWrapper{
		builder: wrapper.builder.GroupBy(groupBys...),
	}

	// Return new wrapped builder
	ud := wrapSelectBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// selectHaving adds a HAVING clause
// Usage: builder = builder:having("COUNT(*) > ?", 5)
func selectHaving(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Updated builder to store result
	var newBuilder squirrel.SelectBuilder

	switch l.Get(2).Type() {
	case lua.LTString:
		// String condition with args
		condition := l.CheckString(2)
		args := make([]interface{}, 0, l.GetTop()-2)
		for i := 3; i <= l.GetTop(); i++ {
			args = append(args, luaconv.ToGoAny(l.Get(i)))
		}
		newBuilder = wrapper.builder.Having(condition, args...)

	case lua.LTTable:
		// Table condition
		table := l.CheckTable(2)
		eqMap := luaTableToMap(l, table)
		newBuilder = wrapper.builder.Having(squirrel.Eq(eqMap))

	case lua.LTUserData:
		// Sqlizer condition
		ud := l.CheckUserData(2)
		if sqlizer, ok := ud.Value.(squirrel.Sqlizer); ok {
			newBuilder = wrapper.builder.Having(sqlizer)
		} else {
			l.ArgError(2, "expected string, table, or Sqlizer")
			return 0
		}

	default:
		l.ArgError(2, "expected string, table, or Sqlizer")
		return 0
	}

	// Create new wrapper
	newWrapper := &selectBuilderWrapper{builder: newBuilder}

	// Return new wrapped builder
	ud := wrapSelectBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// selectLimit adds a LIMIT clause
// Usage: builder = builder:limit(10)
func selectLimit(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	limit := l.CheckNumber(2)

	// Create new wrapper
	newWrapper := &selectBuilderWrapper{
		builder: wrapper.builder.Limit(uint64(limit)),
	}

	// Return new wrapped builder
	ud := wrapSelectBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// selectOffset adds an OFFSET clause
// Usage: builder = builder:offset(20)
func selectOffset(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	offset := l.CheckNumber(2)

	// Create new wrapper
	newWrapper := &selectBuilderWrapper{
		builder: wrapper.builder.Offset(uint64(offset)),
	}

	// Return new wrapped builder
	ud := wrapSelectBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// selectColumns adds additional columns
// Usage: builder = builder:columns("count(*) as total")
func selectColumns(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Get columns
	columns := make([]string, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		columns = append(columns, l.CheckString(i))
	}

	// Create new wrapper
	newWrapper := &selectBuilderWrapper{
		builder: wrapper.builder.Columns(columns...),
	}

	// Return new wrapped builder
	ud := wrapSelectBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// selectDistinct adds DISTINCT to the query
// Usage: builder = builder:distinct()
func selectDistinct(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Create new wrapper
	newWrapper := &selectBuilderWrapper{
		builder: wrapper.builder.Distinct(),
	}

	// Return new wrapped builder
	ud := wrapSelectBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// selectSuffix adds a suffix to the query
// Usage: builder = builder:suffix("FOR UPDATE")
func selectSuffix(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	suffix := l.CheckString(2)

	// Handle optional args
	args := make([]interface{}, 0, l.GetTop()-2)
	for i := 3; i <= l.GetTop(); i++ {
		args = append(args, luaconv.ToGoAny(l.Get(i)))
	}

	// Create new wrapper
	newWrapper := &selectBuilderWrapper{
		builder: wrapper.builder.Suffix(suffix, args...),
	}

	// Return new wrapped builder
	ud := wrapSelectBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// selectPlaceholderFormat sets the placeholder format
// Usage: builder = builder:placeholder_format(sql.builder.dollar)
func selectPlaceholderFormat(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	ud := l.CheckUserData(2)
	format, ok := ud.Value.(squirrel.PlaceholderFormat)
	if !ok {
		l.ArgError(2, "expected placeholder format")
		return 0
	}

	// Create new wrapper
	newWrapper := &selectBuilderWrapper{
		builder: wrapper.builder.PlaceholderFormat(format),
	}

	// Return new wrapped builder
	ud = wrapSelectBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// selectToSql generates the SQL and args (for debugging)
// Usage: sql, args = builder:to_sql()
func selectToSql(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	query, args, err := wrapper.builder.ToSql()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(query))

	// Convert args to Lua table
	argsTable := l.CreateTable(len(args), 0)
	for i, arg := range args {
		luaValue, err := luaconv.GoToLua(arg)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(fmt.Sprintf("conversion error: %v", err)))
			return 2
		}
		argsTable.RawSetInt(i+1, luaValue)
	}

	l.Push(argsTable)
	return 2
}

// selectRunWith creates an executor with this builder
// Usage: executor = builder:run_with(db)
func selectRunWith(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Check for DB or Transaction
	ud := l.CheckUserData(2)

	// Create query executor
	executor, err := NewQueryExecutor(l, wrapper.builder, ud.Value)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Return the executor
	l.Push(executor)
	return 1
}
