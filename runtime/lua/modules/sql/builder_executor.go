package sql

import (
	"database/sql"
	"fmt"

	"github.com/Masterminds/squirrel"
	servicesql "github.com/wippyai/runtime/api/service/sql"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

const queryExecutorTypeName = "sql.QueryExecutor"

type queryExecutorWrapper struct {
	db    *sql.DB
	query string
	args  []any
}

var queryExecutorMethods = map[string]lua.LGoFunc{
	"exec":   executorExec,
	"query":  executorQuery,
	"to_sql": executorToSQL,
}

var queryExecutorMetamethods = map[string]lua.LGoFunc{
	"__tostring": executorToString,
}

func init() {
	value.RegisterTypeMethods(nil, queryExecutorTypeName, queryExecutorMetamethods, queryExecutorMethods)
}

func newQueryExecutorFromSelect(l *lua.LState, dbWrapper *DB, builder squirrel.SelectBuilder) int {
	if dbWrapper.dbType == servicesql.KindPostgres {
		builder = builder.PlaceholderFormat(squirrel.Dollar)
	}
	return newQueryExecutorFromSqlizer(l, dbWrapper.db, builder)
}

func newQueryExecutorFromInsert(l *lua.LState, dbWrapper *DB, builder squirrel.InsertBuilder) int {
	if dbWrapper.dbType == servicesql.KindPostgres {
		builder = builder.PlaceholderFormat(squirrel.Dollar)
	}
	return newQueryExecutorFromSqlizer(l, dbWrapper.db, builder)
}

func newQueryExecutorFromUpdate(l *lua.LState, dbWrapper *DB, builder squirrel.UpdateBuilder) int {
	if dbWrapper.dbType == servicesql.KindPostgres {
		builder = builder.PlaceholderFormat(squirrel.Dollar)
	}
	return newQueryExecutorFromSqlizer(l, dbWrapper.db, builder)
}

func newQueryExecutorFromDelete(l *lua.LState, dbWrapper *DB, builder squirrel.DeleteBuilder) int {
	if dbWrapper.dbType == servicesql.KindPostgres {
		builder = builder.PlaceholderFormat(squirrel.Dollar)
	}
	return newQueryExecutorFromSqlizer(l, dbWrapper.db, builder)
}

func newQueryExecutorFromSqlizer(l *lua.LState, db *sql.DB, builder squirrel.Sqlizer) int {
	query, args, err := builder.ToSql()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, err.Error()).WithKind(lua.KindInvalid).WithRetryable(false))
		return 2
	}

	wrapper := &queryExecutorWrapper{
		db:    db,
		query: query,
		args:  args,
	}

	value.PushTypedUserData(l, wrapper, queryExecutorTypeName)
	return 1
}

func checkQueryExecutor(l *lua.LState) *queryExecutorWrapper {
	ud := l.CheckUserData(1)
	if wrapper, ok := ud.Value.(*queryExecutorWrapper); ok {
		return wrapper
	}
	l.ArgError(1, "expected QueryExecutor object")
	return nil
}

func executorToString(l *lua.LState) int {
	wrapper := checkQueryExecutor(l)
	if wrapper == nil {
		return 0
	}

	l.Push(lua.LString(fmt.Sprintf("QueryExecutor: %s [Args: %v]", wrapper.query, wrapper.args)))
	return 1
}

func executorToSQL(l *lua.LState) int {
	wrapper := checkQueryExecutor(l)
	if wrapper == nil {
		return 0
	}

	l.Push(lua.LString(wrapper.query))
	l.Push(goArgsToLuaTable(l, wrapper.args))
	return 2
}

func executorExec(l *lua.LState) int {
	wrapper := checkQueryExecutor(l)
	if wrapper == nil {
		return 0
	}

	yield := AcquireExecuteYield()
	yield.DB = wrapper.db
	yield.Query = wrapper.query
	yield.Params = wrapper.args
	l.Push(yield)
	return -1
}

func executorQuery(l *lua.LState) int {
	wrapper := checkQueryExecutor(l)
	if wrapper == nil {
		return 0
	}

	yield := AcquireQueryYield()
	yield.DB = wrapper.db
	yield.Query = wrapper.query
	yield.Params = wrapper.args
	l.Push(yield)
	return -1
}
