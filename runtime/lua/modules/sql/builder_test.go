// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"strings"
	"testing"

	"github.com/Masterminds/squirrel"
	lua "github.com/wippyai/go-lua"
)

func TestBuilderSelectBasic(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.Push(lua.LString("id"))
	l.Push(lua.LString("name"))
	builderSelect(l)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	wrapper, ok := ud.Value.(*selectBuilderWrapper)
	if !ok {
		t.Fatalf("expected selectBuilderWrapper, got %T", ud.Value)
	}

	query, _, err := wrapper.builder.From("users").ToSql()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(query, "SELECT id, name FROM users") {
		t.Errorf("unexpected query: %s", query)
	}
}

func TestBuilderInsertBasic(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.Push(lua.LString("users"))
	builderInsert(l)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	wrapper, ok := ud.Value.(*insertBuilderWrapper)
	if !ok {
		t.Fatalf("expected insertBuilderWrapper, got %T", ud.Value)
	}

	query, args, err := wrapper.builder.Columns("name", "age").Values("John", 30).ToSql()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(query, "INSERT INTO users") {
		t.Errorf("unexpected query: %s", query)
	}

	if len(args) != 2 {
		t.Errorf("expected 2 args, got %d", len(args))
	}
}

func TestBuilderUpdateBasic(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.Push(lua.LString("users"))
	builderUpdate(l)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	wrapper, ok := ud.Value.(*updateBuilderWrapper)
	if !ok {
		t.Fatalf("expected updateBuilderWrapper, got %T", ud.Value)
	}

	query, args, err := wrapper.builder.Set("name", "Jane").Where(squirrel.Eq{"id": 1}).ToSql()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(query, "UPDATE users SET name = ?") {
		t.Errorf("unexpected query: %s", query)
	}

	if len(args) != 2 {
		t.Errorf("expected 2 args, got %d", len(args))
	}
}

func TestBuilderDeleteBasic(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.Push(lua.LString("users"))
	builderDelete(l)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	wrapper, ok := ud.Value.(*deleteBuilderWrapper)
	if !ok {
		t.Fatalf("expected deleteBuilderWrapper, got %T", ud.Value)
	}

	query, args, err := wrapper.builder.Where(squirrel.Eq{"id": 1}).ToSql()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(query, "DELETE FROM users") {
		t.Errorf("unexpected query: %s", query)
	}

	if len(args) != 1 {
		t.Errorf("expected 1 arg, got %d", len(args))
	}
}

func TestBuilderExpr(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.Push(lua.LString("COALESCE(?, ?)"))
	l.Push(lua.LNil)
	l.Push(lua.LString("default"))
	builderExpr(l)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	sqlizer, ok := ud.Value.(squirrel.Sqlizer)
	if !ok {
		t.Fatalf("expected Sqlizer, got %T", ud.Value)
	}

	query, args, err := sqlizer.ToSql()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if query != "COALESCE(?, ?)" {
		t.Errorf("unexpected query: %s", query)
	}

	if len(args) != 2 {
		t.Errorf("expected 2 args, got %d", len(args))
	}

	if args[0] != nil {
		t.Errorf("expected first arg to be nil, got %v", args[0])
	}

	if args[1] != "default" {
		t.Errorf("expected second arg to be 'default', got %v", args[1])
	}
}

func TestBuilderEq(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 2)
	tbl.RawSetString("id", lua.LNumber(1))
	tbl.RawSetString("name", lua.LString("John"))
	l.Push(tbl)
	builderEq(l)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	sqlizer, ok := ud.Value.(squirrel.Sqlizer)
	if !ok {
		t.Fatalf("expected Sqlizer, got %T", ud.Value)
	}

	query, args, err := sqlizer.ToSql()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(args) != 2 {
		t.Errorf("expected 2 args, got %d", len(args))
	}

	if !strings.Contains(query, "=") {
		t.Errorf("expected query to contain '=', got: %s", query)
	}
}

func TestBuilderNotEq(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 1)
	tbl.RawSetString("id", lua.LNumber(1))
	l.Push(tbl)
	builderNotEq(l)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	sqlizer, ok := ud.Value.(squirrel.Sqlizer)
	if !ok {
		t.Fatalf("expected Sqlizer, got %T", ud.Value)
	}

	query, _, err := sqlizer.ToSql()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(query, "<>") {
		t.Errorf("expected query to contain '<>', got: %s", query)
	}
}

func TestBuilderLt(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 1)
	tbl.RawSetString("id", lua.LNumber(100))
	l.Push(tbl)
	builderLt(l)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	sqlizer, ok := ud.Value.(squirrel.Sqlizer)
	if !ok {
		t.Fatalf("expected Sqlizer, got %T", ud.Value)
	}

	query, _, err := sqlizer.ToSql()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(query, "<") || strings.Contains(query, "<=") {
		t.Errorf("expected query to contain '<' but not '<=', got: %s", query)
	}
}

func TestBuilderGt(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 1)
	tbl.RawSetString("id", lua.LNumber(100))
	l.Push(tbl)
	builderGt(l)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	sqlizer, ok := ud.Value.(squirrel.Sqlizer)
	if !ok {
		t.Fatalf("expected Sqlizer, got %T", ud.Value)
	}

	query, _, err := sqlizer.ToSql()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(query, ">") || strings.Contains(query, ">=") {
		t.Errorf("expected query to contain '>' but not '>=', got: %s", query)
	}
}

func TestBuilderLike(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 1)
	tbl.RawSetString("name", lua.LString("%John%"))
	l.Push(tbl)
	builderLike(l)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	sqlizer, ok := ud.Value.(squirrel.Sqlizer)
	if !ok {
		t.Fatalf("expected Sqlizer, got %T", ud.Value)
	}

	query, _, err := sqlizer.ToSql()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(query, "LIKE") {
		t.Errorf("expected query to contain 'LIKE', got: %s", query)
	}
}

func TestBuilderPlaceholderFormats(t *testing.T) {
	mod, _ := Module.Build()
	builder := mod.RawGetString("builder").(*lua.LTable)

	tests := []struct {
		expected squirrel.PlaceholderFormat
		name     string
	}{
		{squirrel.Question, "question"},
		{squirrel.Dollar, "dollar"},
		{squirrel.AtP, "at"},
		{squirrel.Colon, "colon"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := builder.RawGetString(tt.name)
			ud, ok := val.(*lua.LUserData)
			if !ok {
				t.Fatalf("expected userdata, got %T", val)
			}

			format, ok := ud.Value.(squirrel.PlaceholderFormat)
			if !ok {
				t.Fatalf("expected PlaceholderFormat, got %T", ud.Value)
			}

			if format != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, format)
			}
		})
	}
}

func TestGoValueToLua(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tests := []struct {
		input    any
		expected lua.LValue
	}{
		{nil, lua.LNil},
		{true, lua.LTrue},
		{false, lua.LFalse},
		{42, lua.LInteger(42)},
		{int64(100), lua.LInteger(100)},
		{3.14, lua.LNumber(3.14)},
		{"hello", lua.LString("hello")},
		{[]byte("bytes"), lua.LString("bytes")},
	}

	for _, tt := range tests {
		result := goValueToLua(l, tt.input)
		if result != tt.expected {
			t.Errorf("goValueToLua(%v) = %v, expected %v", tt.input, result, tt.expected)
		}
	}
}

func TestGoArgsToLuaTable(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	args := []any{"hello", int64(42), 3.14, true}
	result := goArgsToLuaTable(l, args)

	if result.Len() != 4 {
		t.Errorf("expected table length 4, got %d", result.Len())
	}

	if result.RawGetInt(1) != lua.LString("hello") {
		t.Errorf("expected first element to be 'hello', got %v", result.RawGetInt(1))
	}

	if result.RawGetInt(2) != lua.LInteger(42) {
		t.Errorf("expected second element to be 42, got %v", result.RawGetInt(2))
	}

	if result.RawGetInt(3) != lua.LNumber(3.14) {
		t.Errorf("expected third element to be 3.14, got %v", result.RawGetInt(3))
	}

	if result.RawGetInt(4) != lua.LTrue {
		t.Errorf("expected fourth element to be true, got %v", result.RawGetInt(4))
	}
}

func TestBuilderMethodsHaveRunWith(t *testing.T) {
	tests := []struct {
		methods map[string]lua.LGoFunc
		name    string
	}{
		{selectBuilderMethods, "SelectBuilder"},
		{insertBuilderMethods, "InsertBuilder"},
		{updateBuilderMethods, "UpdateBuilder"},
		{deleteBuilderMethods, "DeleteBuilder"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := tt.methods["run_with"]; !ok {
				t.Errorf("%s missing run_with method", tt.name)
			}
		})
	}
}

func TestQueryExecutorMethods(t *testing.T) {
	expectedMethods := []string{"exec", "query", "to_sql"}

	for _, method := range expectedMethods {
		if _, ok := queryExecutorMethods[method]; !ok {
			t.Errorf("QueryExecutor missing %s method", method)
		}
	}
}

func TestQueryExecutorToSQL(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	wrapper := &queryExecutorWrapper{
		db:    nil,
		query: "SELECT * FROM users WHERE id = ?",
		args:  []any{int64(42)},
	}

	ud := l.NewUserData()
	ud.Value = wrapper
	l.Push(ud)

	executorToSQL(l)

	query := l.Get(-2)
	args := l.Get(-1)

	if query.(lua.LString) != "SELECT * FROM users WHERE id = ?" {
		t.Errorf("unexpected query: %v", query)
	}

	argsTable, ok := args.(*lua.LTable)
	if !ok {
		t.Fatalf("expected args table, got %T", args)
	}

	if argsTable.Len() != 1 {
		t.Errorf("expected 1 arg, got %d", argsTable.Len())
	}

	if argsTable.RawGetInt(1) != lua.LInteger(42) {
		t.Errorf("expected arg to be 42, got %v", argsTable.RawGetInt(1))
	}
}

func TestPostgresPlaceholderAutoDetection(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	builder := squirrel.Select("id", "name").From("users").Where(squirrel.Eq{"id": 1})

	dbWrapper := &DB{
		db:     nil,
		dbType: "db.sql.postgres",
	}

	newQueryExecutorFromSelect(l, dbWrapper, builder)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	executor, ok := ud.Value.(*queryExecutorWrapper)
	if !ok {
		t.Fatalf("expected queryExecutorWrapper, got %T", ud.Value)
	}

	if !strings.Contains(executor.query, "$1") {
		t.Errorf("expected postgres dollar placeholders, got: %s", executor.query)
	}
}

func TestMySQLPlaceholderDefault(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	builder := squirrel.Select("id", "name").From("users").Where(squirrel.Eq{"id": 1})

	dbWrapper := &DB{
		db:     nil,
		dbType: "db.sql.mysql",
	}

	newQueryExecutorFromSelect(l, dbWrapper, builder)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	executor, ok := ud.Value.(*queryExecutorWrapper)
	if !ok {
		t.Fatalf("expected queryExecutorWrapper, got %T", ud.Value)
	}

	if !strings.Contains(executor.query, "?") {
		t.Errorf("expected question mark placeholders, got: %s", executor.query)
	}
}

func TestBuilderLtOrEq(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 1)
	tbl.RawSetString("id", lua.LNumber(100))
	l.Push(tbl)
	builderLtOrEq(l)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	sqlizer, ok := ud.Value.(squirrel.Sqlizer)
	if !ok {
		t.Fatalf("expected Sqlizer, got %T", ud.Value)
	}

	query, _, err := sqlizer.ToSql()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(query, "<=") {
		t.Errorf("expected query to contain '<=', got: %s", query)
	}
}

func TestBuilderGtOrEq(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 1)
	tbl.RawSetString("id", lua.LNumber(100))
	l.Push(tbl)
	builderGtOrEq(l)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	sqlizer, ok := ud.Value.(squirrel.Sqlizer)
	if !ok {
		t.Fatalf("expected Sqlizer, got %T", ud.Value)
	}

	query, _, err := sqlizer.ToSql()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(query, ">=") {
		t.Errorf("expected query to contain '>=', got: %s", query)
	}
}

func TestBuilderNotLike(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 1)
	tbl.RawSetString("name", lua.LString("%test%"))
	l.Push(tbl)
	builderNotLike(l)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	sqlizer, ok := ud.Value.(squirrel.Sqlizer)
	if !ok {
		t.Fatalf("expected Sqlizer, got %T", ud.Value)
	}

	query, _, err := sqlizer.ToSql()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(query, "NOT LIKE") {
		t.Errorf("expected query to contain 'NOT LIKE', got: %s", query)
	}
}

func TestBuilderAnd(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	// Create array of conditions
	arr := l.CreateTable(2, 0)

	// First condition: {id = 1}
	cond1 := l.CreateTable(0, 1)
	cond1.RawSetString("id", lua.LNumber(1))
	arr.RawSetInt(1, cond1)

	// Second condition: {active = true}
	cond2 := l.CreateTable(0, 1)
	cond2.RawSetString("active", lua.LTrue)
	arr.RawSetInt(2, cond2)

	l.Push(arr)
	builderAnd(l)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	sqlizer, ok := ud.Value.(squirrel.Sqlizer)
	if !ok {
		t.Fatalf("expected Sqlizer, got %T", ud.Value)
	}

	query, _, err := sqlizer.ToSql()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(query, "AND") {
		t.Errorf("expected query to contain 'AND', got: %s", query)
	}
}

func TestBuilderOr(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	// Create array of conditions
	arr := l.CreateTable(2, 0)

	// First condition: {id = 1}
	cond1 := l.CreateTable(0, 1)
	cond1.RawSetString("id", lua.LNumber(1))
	arr.RawSetInt(1, cond1)

	// Second condition: {id = 2}
	cond2 := l.CreateTable(0, 1)
	cond2.RawSetString("id", lua.LNumber(2))
	arr.RawSetInt(2, cond2)

	l.Push(arr)
	builderOr(l)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	sqlizer, ok := ud.Value.(squirrel.Sqlizer)
	if !ok {
		t.Fatalf("expected Sqlizer, got %T", ud.Value)
	}

	query, _, err := sqlizer.ToSql()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(query, "OR") {
		t.Errorf("expected query to contain 'OR', got: %s", query)
	}
}

func TestSelectBuilderMethods(t *testing.T) {
	expectedMethods := []string{
		"from", "join", "left_join", "right_join", "inner_join",
		"where", "order_by", "group_by", "having", "limit", "offset",
		"columns", "distinct", "suffix", "placeholder_format", "to_sql", "run_with",
	}

	for _, method := range expectedMethods {
		if _, ok := selectBuilderMethods[method]; !ok {
			t.Errorf("SelectBuilder missing %s method", method)
		}
	}
}

func TestInsertBuilderMethods(t *testing.T) {
	expectedMethods := []string{
		"into", "columns", "values", "set_map", "select",
		"prefix", "suffix", "options", "placeholder_format", "to_sql", "run_with",
	}

	for _, method := range expectedMethods {
		if _, ok := insertBuilderMethods[method]; !ok {
			t.Errorf("InsertBuilder missing %s method", method)
		}
	}
}

func TestUpdateBuilderMethods(t *testing.T) {
	expectedMethods := []string{
		"table", "set", "set_map", "where", "order_by", "limit", "offset",
		"from", "from_select", "suffix", "placeholder_format", "to_sql", "run_with",
	}

	for _, method := range expectedMethods {
		if _, ok := updateBuilderMethods[method]; !ok {
			t.Errorf("UpdateBuilder missing %s method", method)
		}
	}
}

func TestDeleteBuilderMethods(t *testing.T) {
	expectedMethods := []string{
		"from", "where", "order_by", "limit", "offset",
		"suffix", "placeholder_format", "to_sql", "run_with",
	}

	for _, method := range expectedMethods {
		if _, ok := deleteBuilderMethods[method]; !ok {
			t.Errorf("DeleteBuilder missing %s method", method)
		}
	}
}

func TestSQLiteAutoDetection(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	builder := squirrel.Select("id").From("users").Where(squirrel.Eq{"id": 1})

	dbWrapper := &DB{
		db:     nil,
		dbType: "db.sql.sqlite",
	}

	newQueryExecutorFromSelect(l, dbWrapper, builder)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	executor, ok := ud.Value.(*queryExecutorWrapper)
	if !ok {
		t.Fatalf("expected queryExecutorWrapper, got %T", ud.Value)
	}

	// SQLite uses question marks
	if !strings.Contains(executor.query, "?") {
		t.Errorf("expected question mark placeholders for SQLite, got: %s", executor.query)
	}
}

func TestInsertExecutorAutoDetection(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	builder := squirrel.Insert("users").Columns("name").Values("test")

	dbWrapper := &DB{
		db:     nil,
		dbType: "db.sql.postgres",
	}

	newQueryExecutorFromInsert(l, dbWrapper, builder)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	executor, ok := ud.Value.(*queryExecutorWrapper)
	if !ok {
		t.Fatalf("expected queryExecutorWrapper, got %T", ud.Value)
	}

	if !strings.Contains(executor.query, "$1") {
		t.Errorf("expected postgres dollar placeholders, got: %s", executor.query)
	}
}

func TestUpdateExecutorAutoDetection(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	builder := squirrel.Update("users").Set("name", "test").Where(squirrel.Eq{"id": 1})

	dbWrapper := &DB{
		db:     nil,
		dbType: "db.sql.postgres",
	}

	newQueryExecutorFromUpdate(l, dbWrapper, builder)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	executor, ok := ud.Value.(*queryExecutorWrapper)
	if !ok {
		t.Fatalf("expected queryExecutorWrapper, got %T", ud.Value)
	}

	if !strings.Contains(executor.query, "$1") {
		t.Errorf("expected postgres dollar placeholders, got: %s", executor.query)
	}
}

func TestDeleteExecutorAutoDetection(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	builder := squirrel.Delete("users").Where(squirrel.Eq{"id": 1})

	dbWrapper := &DB{
		db:     nil,
		dbType: "db.sql.postgres",
	}

	newQueryExecutorFromDelete(l, dbWrapper, builder)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	executor, ok := ud.Value.(*queryExecutorWrapper)
	if !ok {
		t.Fatalf("expected queryExecutorWrapper, got %T", ud.Value)
	}

	if !strings.Contains(executor.query, "$1") {
		t.Errorf("expected postgres dollar placeholders, got: %s", executor.query)
	}
}

func TestSelectExecutorWithTransaction(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	builder := squirrel.Select("id", "name").From("users").Where(squirrel.Eq{"id": 1})

	db := &DB{
		db:     nil,
		dbType: "db.sql.postgres",
	}
	txWrapper := &Transaction{
		tx:     nil,
		db:     db,
		active: true,
	}

	newQueryExecutorFromSelectTx(l, txWrapper, builder)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	executor, ok := ud.Value.(*queryExecutorWrapper)
	if !ok {
		t.Fatalf("expected queryExecutorWrapper, got %T", ud.Value)
	}

	if executor.db != nil {
		t.Error("expected db to be nil for transaction executor")
	}

	if !strings.Contains(executor.query, "$1") {
		t.Errorf("expected postgres dollar placeholders, got: %s", executor.query)
	}
}

func TestInsertExecutorWithTransaction(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	builder := squirrel.Insert("users").Columns("name", "email").Values("test", "test@example.com")

	db := &DB{
		db:     nil,
		dbType: "db.sql.mysql",
	}
	txWrapper := &Transaction{
		tx:     nil,
		db:     db,
		active: true,
	}

	newQueryExecutorFromInsertTx(l, txWrapper, builder)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	executor, ok := ud.Value.(*queryExecutorWrapper)
	if !ok {
		t.Fatalf("expected queryExecutorWrapper, got %T", ud.Value)
	}

	if executor.db != nil {
		t.Error("expected db to be nil for transaction executor")
	}

	if !strings.Contains(executor.query, "?") {
		t.Errorf("expected question mark placeholders, got: %s", executor.query)
	}
}

func TestUpdateExecutorWithTransaction(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	builder := squirrel.Update("users").Set("name", "updated").Where(squirrel.Eq{"id": 1})

	db := &DB{
		db:     nil,
		dbType: "db.sql.postgres",
	}
	txWrapper := &Transaction{
		tx:     nil,
		db:     db,
		active: true,
	}

	newQueryExecutorFromUpdateTx(l, txWrapper, builder)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	executor, ok := ud.Value.(*queryExecutorWrapper)
	if !ok {
		t.Fatalf("expected queryExecutorWrapper, got %T", ud.Value)
	}

	if executor.db != nil {
		t.Error("expected db to be nil for transaction executor")
	}

	if !strings.Contains(executor.query, "$") {
		t.Errorf("expected postgres dollar placeholders, got: %s", executor.query)
	}
}

func TestDeleteExecutorWithTransaction(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	builder := squirrel.Delete("users").Where(squirrel.Eq{"id": 1})

	db := &DB{
		db:     nil,
		dbType: "db.sql.postgres",
	}
	txWrapper := &Transaction{
		tx:     nil,
		db:     db,
		active: true,
	}

	newQueryExecutorFromDeleteTx(l, txWrapper, builder)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	executor, ok := ud.Value.(*queryExecutorWrapper)
	if !ok {
		t.Fatalf("expected queryExecutorWrapper, got %T", ud.Value)
	}

	if executor.db != nil {
		t.Error("expected db to be nil for transaction executor")
	}

	if !strings.Contains(executor.query, "$1") {
		t.Errorf("expected postgres dollar placeholders, got: %s", executor.query)
	}
}

func TestRunWithAcceptsBothDBAndTransaction(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	selectWrapper := &selectBuilderWrapper{
		builder: squirrel.Select("*").From("users"),
	}
	insertWrapper := &insertBuilderWrapper{
		builder: squirrel.Insert("users").Columns("name").Values("test"),
	}
	updateWrapper := &updateBuilderWrapper{
		builder: squirrel.Update("users").Set("name", "updated"),
	}
	deleteWrapper := &deleteBuilderWrapper{
		builder: squirrel.Delete("users"),
	}

	db := &DB{
		db:     nil,
		dbType: "db.sql.sqlite3",
	}
	tx := &Transaction{
		tx:     nil,
		db:     db,
		active: true,
	}

	tests := []struct {
		setupFunc   func()
		runWithFunc func(*lua.LState) int
		name        string
	}{
		{
			name: "select with DB",
			setupFunc: func() {
				ud := l.NewUserData()
				ud.Value = selectWrapper
				l.Push(ud)
				dbUD := l.NewUserData()
				dbUD.Value = db
				l.Push(dbUD)
			},
			runWithFunc: selectRunWith,
		},
		{
			name: "select with TX",
			setupFunc: func() {
				ud := l.NewUserData()
				ud.Value = selectWrapper
				l.Push(ud)
				txUD := l.NewUserData()
				txUD.Value = tx
				l.Push(txUD)
			},
			runWithFunc: selectRunWith,
		},
		{
			name: "insert with DB",
			setupFunc: func() {
				ud := l.NewUserData()
				ud.Value = insertWrapper
				l.Push(ud)
				dbUD := l.NewUserData()
				dbUD.Value = db
				l.Push(dbUD)
			},
			runWithFunc: insertRunWith,
		},
		{
			name: "insert with TX",
			setupFunc: func() {
				ud := l.NewUserData()
				ud.Value = insertWrapper
				l.Push(ud)
				txUD := l.NewUserData()
				txUD.Value = tx
				l.Push(txUD)
			},
			runWithFunc: insertRunWith,
		},
		{
			name: "update with DB",
			setupFunc: func() {
				ud := l.NewUserData()
				ud.Value = updateWrapper
				l.Push(ud)
				dbUD := l.NewUserData()
				dbUD.Value = db
				l.Push(dbUD)
			},
			runWithFunc: updateRunWith,
		},
		{
			name: "update with TX",
			setupFunc: func() {
				ud := l.NewUserData()
				ud.Value = updateWrapper
				l.Push(ud)
				txUD := l.NewUserData()
				txUD.Value = tx
				l.Push(txUD)
			},
			runWithFunc: updateRunWith,
		},
		{
			name: "delete with DB",
			setupFunc: func() {
				ud := l.NewUserData()
				ud.Value = deleteWrapper
				l.Push(ud)
				dbUD := l.NewUserData()
				dbUD.Value = db
				l.Push(dbUD)
			},
			runWithFunc: deleteRunWith,
		},
		{
			name: "delete with TX",
			setupFunc: func() {
				ud := l.NewUserData()
				ud.Value = deleteWrapper
				l.Push(ud)
				txUD := l.NewUserData()
				txUD.Value = tx
				l.Push(txUD)
			},
			runWithFunc: deleteRunWith,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l.SetTop(0)
			tt.setupFunc()

			count := tt.runWithFunc(l)
			if count != 1 {
				t.Errorf("expected 1 return value, got %d", count)
			}

			result := l.Get(-1)
			if result == lua.LNil {
				t.Error("expected non-nil result")
			}

			ud, ok := result.(*lua.LUserData)
			if !ok {
				t.Fatalf("expected userdata, got %T", result)
			}

			_, ok = ud.Value.(*queryExecutorWrapper)
			if !ok {
				t.Fatalf("expected queryExecutorWrapper, got %T", ud.Value)
			}
		})
	}
}
