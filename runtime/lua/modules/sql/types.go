// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"github.com/wippyai/go-lua/types/contract"
	"github.com/wippyai/go-lua/types/effect"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// DB Stats record
var dbStatsType = typ.NewRecord().
	Field("max_open_connections", typ.Integer).
	Field("open_connections", typ.Integer).
	Field("in_use", typ.Integer).
	Field("idle", typ.Integer).
	Field("wait_count", typ.Integer).
	Field("wait_duration", typ.String).
	Field("max_idle_closed", typ.Integer).
	Field("max_idle_time_closed", typ.Integer).
	Field("max_lifetime_closed", typ.Integer).
	Build()

// Query row: {[string]: any} — each row is a table keyed by column name
var queryRowType = typ.NewMap(typ.String, typ.Any)

// Execute result record
var executeResultType = typ.NewRecord().
	Field("rows_affected", typ.Integer).
	Field("last_insert_id", typ.Integer).
	Build()

// Interface types
var (
	sqlDBType       typ.Type
	transactionType typ.Type
	statementType   typ.Type
)

// runnerType is a union type that accepts either DB or Transaction
var runnerType typ.Type

// Placeholder format type
var placeholderFormatType = typ.Any

// Sqlizer type for SQL expression builders
var sqlizerType = typ.NewInterface("sql.Sqlizer", []typ.Method{
	{Name: "to_sql", Type: typ.Func().
		Param("self", typ.Self).
		Returns(typ.String, typ.NewArray(typ.Any), typ.NewOptional(typ.LuaError)).
		Build()},
})

// Query executor returned by builder.run_with
var queryExecutorType = typ.NewInterface("sql.QueryExecutor", []typ.Method{
	{Name: "query", Type: typ.Func().
		Param("self", typ.Self).
		Returns(typ.NewArray(queryRowType), typ.NewOptional(typ.LuaError)).
		Build()},
	{Name: "exec", Type: typ.Func().
		Param("self", typ.Self).
		Returns(executeResultType, typ.NewOptional(typ.LuaError)).
		Build()},
	{Name: "to_sql", Type: typ.Func().
		Param("self", typ.Self).
		Returns(typ.String, typ.NewArray(typ.Any), typ.NewOptional(typ.LuaError)).
		Build()},
})

// SelectBuilder type
var selectBuilderType typ.Type

// InsertBuilder type
var insertBuilderType typ.Type

// UpdateBuilder type
var updateBuilderType typ.Type

// DeleteBuilder type
var deleteBuilderType typ.Type

func init() {
	// Statement interface
	statementType = typ.NewInterface("sql.Statement", []typ.Method{
		{Name: "query", Type: typ.Func().
			Param("self", typ.Self).
			Variadic(typ.Any).
			Returns(typ.NewArray(queryRowType), typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "execute", Type: typ.Func().
			Param("self", typ.Self).
			Variadic(typ.Any).
			Returns(executeResultType, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "close", Type: typ.Func().
			Param("self", typ.Self).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
	})

	// Transaction interface
	transactionType = typ.NewInterface("sql.Transaction", []typ.Method{
		{Name: "query", Type: typ.Func().
			Param("self", typ.Self).
			Param("sql", typ.String).
			Variadic(typ.Any).
			Returns(typ.NewArray(queryRowType), typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "execute", Type: typ.Func().
			Param("self", typ.Self).
			Param("sql", typ.String).
			Variadic(typ.Any).
			Returns(executeResultType, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "prepare", Type: typ.Func().
			Param("self", typ.Self).
			Param("sql", typ.String).
			Returns(statementType, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "savepoint", Type: typ.Func().
			Param("self", typ.Self).
			Param("name", typ.String).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "rollback_to", Type: typ.Func().
			Param("self", typ.Self).
			Param("name", typ.String).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "release", Type: typ.Func().
			Param("self", typ.Self).
			Param("name", typ.String).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "db_type", Type: typ.Func().
			Param("self", typ.Self).
			Returns(typ.String, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "commit", Type: typ.Func().
			Param("self", typ.Self).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "rollback", Type: typ.Func().
			Param("self", typ.Self).
			OptParam("name", typ.String).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
	})

	// DB interface
	sqlDBType = typ.NewInterface("sql.DB", []typ.Method{
		{Name: "type", Type: typ.Func().
			Param("self", typ.Self).
			Returns(typ.String, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "query", Type: typ.Func().
			Param("self", typ.Self).
			Param("sql", typ.String).
			Variadic(typ.Any).
			Returns(typ.NewArray(queryRowType), typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "execute", Type: typ.Func().
			Param("self", typ.Self).
			Param("sql", typ.String).
			Variadic(typ.Any).
			Returns(executeResultType, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "prepare", Type: typ.Func().
			Param("self", typ.Self).
			Param("sql", typ.String).
			Returns(statementType, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "begin", Type: typ.Func().
			Param("self", typ.Self).
			OptParam("opts", typ.Any).
			Returns(transactionType, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "release", Type: typ.Func().
			Param("self", typ.Self).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "stats", Type: typ.Func().
			Param("self", typ.Self).
			Returns(dbStatsType, typ.NewOptional(typ.LuaError)).
			Build()},
	})

	// Initialize runnerType as union of DB and Transaction
	runnerType = typ.NewUnion(sqlDBType, transactionType)

	// SelectBuilder type methods
	selectBuilderType = typ.NewInterface("sql.SelectBuilder", []typ.Method{
		{Name: "from", Type: typ.Func().
			Param("self", typ.Self).
			Param("table", typ.String).
			Variadic(typ.Any).
			Returns(typ.Self).
			Build()},
		{Name: "join", Type: typ.Func().
			Param("self", typ.Self).
			Param("table", typ.String).
			Variadic(typ.Any).
			Returns(typ.Self).
			Build()},
		{Name: "left_join", Type: typ.Func().
			Param("self", typ.Self).
			Param("table", typ.String).
			Variadic(typ.Any).
			Returns(typ.Self).
			Build()},
		{Name: "right_join", Type: typ.Func().
			Param("self", typ.Self).
			Param("table", typ.String).
			Variadic(typ.Any).
			Returns(typ.Self).
			Build()},
		{Name: "inner_join", Type: typ.Func().
			Param("self", typ.Self).
			Param("table", typ.String).
			Variadic(typ.Any).
			Returns(typ.Self).
			Build()},
		{Name: "where", Type: typ.Func().
			Param("self", typ.Self).
			Param("condition", typ.Any).
			Variadic(typ.Any).
			Returns(typ.Self).
			Build()},
		{Name: "order_by", Type: typ.Func().
			Param("self", typ.Self).
			Variadic(typ.String).
			Returns(typ.Self).
			Build()},
		{Name: "group_by", Type: typ.Func().
			Param("self", typ.Self).
			Variadic(typ.String).
			Returns(typ.Self).
			Build()},
		{Name: "having", Type: typ.Func().
			Param("self", typ.Self).
			Param("condition", typ.Any).
			Variadic(typ.Any).
			Returns(typ.Self).
			Build()},
		{Name: "limit", Type: typ.Func().
			Param("self", typ.Self).
			Param("count", typ.Number).
			Returns(typ.Self).
			Build()},
		{Name: "offset", Type: typ.Func().
			Param("self", typ.Self).
			Param("offset", typ.Number).
			Returns(typ.Self).
			Build()},
		{Name: "columns", Type: typ.Func().
			Param("self", typ.Self).
			Variadic(typ.String).
			Returns(typ.Self).
			Build()},
		{Name: "distinct", Type: typ.Func().
			Param("self", typ.Self).
			Returns(typ.Self).
			Build()},
		{Name: "suffix", Type: typ.Func().
			Param("self", typ.Self).
			Param("sql", typ.String).
			Variadic(typ.Any).
			Returns(typ.Self).
			Build()},
		{Name: "placeholder_format", Type: typ.Func().
			Param("self", typ.Self).
			Param("format", placeholderFormatType).
			Returns(typ.Self).
			Build()},
		{Name: "to_sql", Type: typ.Func().
			Param("self", typ.Self).
			Returns(typ.String, typ.NewArray(typ.Any), typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "run_with", Type: typ.Func().
			Param("self", typ.Self).
			Param("runner", runnerType).
			Returns(queryExecutorType).
			Build()},
	})

	// InsertBuilder type methods
	insertBuilderType = typ.NewInterface("sql.InsertBuilder", []typ.Method{
		{Name: "into", Type: typ.Func().
			Param("self", typ.Self).
			Param("table", typ.String).
			Returns(typ.Self).
			Build()},
		{Name: "columns", Type: typ.Func().
			Param("self", typ.Self).
			Variadic(typ.String).
			Returns(typ.Self).
			Build()},
		{Name: "values", Type: typ.Func().
			Param("self", typ.Self).
			Variadic(typ.Any).
			Returns(typ.Self).
			Build()},
		{Name: "set_map", Type: typ.Func().
			Param("self", typ.Self).
			Param("map", typ.Any).
			Returns(typ.Self).
			Build()},
		{Name: "select", Type: typ.Func().
			Param("self", typ.Self).
			Param("builder", selectBuilderType).
			Returns(typ.Self).
			Build()},
		{Name: "prefix", Type: typ.Func().
			Param("self", typ.Self).
			Param("sql", typ.String).
			Variadic(typ.Any).
			Returns(typ.Self).
			Build()},
		{Name: "suffix", Type: typ.Func().
			Param("self", typ.Self).
			Param("sql", typ.String).
			Variadic(typ.Any).
			Returns(typ.Self).
			Build()},
		{Name: "options", Type: typ.Func().
			Param("self", typ.Self).
			Variadic(typ.String).
			Returns(typ.Self).
			Build()},
		{Name: "placeholder_format", Type: typ.Func().
			Param("self", typ.Self).
			Param("format", placeholderFormatType).
			Returns(typ.Self).
			Build()},
		{Name: "to_sql", Type: typ.Func().
			Param("self", typ.Self).
			Returns(typ.String, typ.NewArray(typ.Any), typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "run_with", Type: typ.Func().
			Param("self", typ.Self).
			Param("runner", runnerType).
			Returns(queryExecutorType).
			Build()},
	})

	// UpdateBuilder type methods
	updateBuilderType = typ.NewInterface("sql.UpdateBuilder", []typ.Method{
		{Name: "table", Type: typ.Func().
			Param("self", typ.Self).
			Param("table", typ.String).
			Returns(typ.Self).
			Build()},
		{Name: "set", Type: typ.Func().
			Param("self", typ.Self).
			Param("column", typ.String).
			Param("value", typ.Any).
			Returns(typ.Self).
			Build()},
		{Name: "set_map", Type: typ.Func().
			Param("self", typ.Self).
			Param("map", typ.Any).
			Returns(typ.Self).
			Build()},
		{Name: "where", Type: typ.Func().
			Param("self", typ.Self).
			Param("condition", typ.Any).
			Variadic(typ.Any).
			Returns(typ.Self).
			Build()},
		{Name: "order_by", Type: typ.Func().
			Param("self", typ.Self).
			Variadic(typ.String).
			Returns(typ.Self).
			Build()},
		{Name: "limit", Type: typ.Func().
			Param("self", typ.Self).
			Param("count", typ.Number).
			Returns(typ.Self).
			Build()},
		{Name: "offset", Type: typ.Func().
			Param("self", typ.Self).
			Param("offset", typ.Number).
			Returns(typ.Self).
			Build()},
		{Name: "from", Type: typ.Func().
			Param("self", typ.Self).
			Param("table", typ.String).
			Returns(typ.Self).
			Build()},
		{Name: "from_select", Type: typ.Func().
			Param("self", typ.Self).
			Param("builder", selectBuilderType).
			Param("alias", typ.String).
			Returns(typ.Self).
			Build()},
		{Name: "suffix", Type: typ.Func().
			Param("self", typ.Self).
			Param("sql", typ.String).
			Variadic(typ.Any).
			Returns(typ.Self).
			Build()},
		{Name: "placeholder_format", Type: typ.Func().
			Param("self", typ.Self).
			Param("format", placeholderFormatType).
			Returns(typ.Self).
			Build()},
		{Name: "to_sql", Type: typ.Func().
			Param("self", typ.Self).
			Returns(typ.String, typ.NewArray(typ.Any), typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "run_with", Type: typ.Func().
			Param("self", typ.Self).
			Param("runner", runnerType).
			Returns(queryExecutorType).
			Build()},
	})

	// DeleteBuilder type methods
	deleteBuilderType = typ.NewInterface("sql.DeleteBuilder", []typ.Method{
		{Name: "from", Type: typ.Func().
			Param("self", typ.Self).
			Param("table", typ.String).
			Returns(typ.Self).
			Build()},
		{Name: "where", Type: typ.Func().
			Param("self", typ.Self).
			Param("condition", typ.Any).
			Variadic(typ.Any).
			Returns(typ.Self).
			Build()},
		{Name: "order_by", Type: typ.Func().
			Param("self", typ.Self).
			Variadic(typ.String).
			Returns(typ.Self).
			Build()},
		{Name: "limit", Type: typ.Func().
			Param("self", typ.Self).
			Param("count", typ.Number).
			Returns(typ.Self).
			Build()},
		{Name: "offset", Type: typ.Func().
			Param("self", typ.Self).
			Param("offset", typ.Number).
			Returns(typ.Self).
			Build()},
		{Name: "suffix", Type: typ.Func().
			Param("self", typ.Self).
			Param("sql", typ.String).
			Variadic(typ.Any).
			Returns(typ.Self).
			Build()},
		{Name: "placeholder_format", Type: typ.Func().
			Param("self", typ.Self).
			Param("format", placeholderFormatType).
			Returns(typ.Self).
			Build()},
		{Name: "to_sql", Type: typ.Func().
			Param("self", typ.Self).
			Returns(typ.String, typ.NewArray(typ.Any), typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "run_with", Type: typ.Func().
			Param("self", typ.Self).
			Param("runner", runnerType).
			Returns(queryExecutorType).
			Build()},
	})

	// Builder submodule type (must be after builder types are initialized)
	builderType = typ.NewInterface("sql.builder", []typ.Method{
		{Name: "select", Type: typ.Func().
			Variadic(typ.String).
			Returns(selectBuilderType).
			Build()},
		{Name: "insert", Type: typ.Func().
			OptParam("table", typ.String).
			Returns(insertBuilderType).
			Build()},
		{Name: "update", Type: typ.Func().
			OptParam("table", typ.String).
			Returns(updateBuilderType).
			Build()},
		{Name: "delete", Type: typ.Func().
			OptParam("table", typ.String).
			Returns(deleteBuilderType).
			Build()},
		{Name: "expr", Type: typ.Func().
			Param("expr", typ.String).
			Variadic(typ.Any).
			Returns(sqlizerType).
			Build()},
		{Name: "eq", Type: typ.Func().
			Param("column", typ.String).
			Param("value", typ.Any).
			Returns(sqlizerType).
			Build()},
		{Name: "not_eq", Type: typ.Func().
			Param("column", typ.String).
			Param("value", typ.Any).
			Returns(sqlizerType).
			Build()},
		{Name: "lt", Type: typ.Func().
			Param("column", typ.String).
			Param("value", typ.Any).
			Returns(sqlizerType).
			Build()},
		{Name: "lte", Type: typ.Func().
			Param("column", typ.String).
			Param("value", typ.Any).
			Returns(sqlizerType).
			Build()},
		{Name: "gt", Type: typ.Func().
			Param("column", typ.String).
			Param("value", typ.Any).
			Returns(sqlizerType).
			Build()},
		{Name: "gte", Type: typ.Func().
			Param("column", typ.String).
			Param("value", typ.Any).
			Returns(sqlizerType).
			Build()},
		{Name: "like", Type: typ.Func().
			Param("column", typ.String).
			Param("value", typ.Any).
			Returns(sqlizerType).
			Build()},
		{Name: "not_like", Type: typ.Func().
			Param("column", typ.String).
			Param("value", typ.Any).
			Returns(sqlizerType).
			Build()},
		{Name: "and_", Type: typ.Func().
			Variadic(sqlizerType).
			Returns(sqlizerType).
			Build()},
		{Name: "or_", Type: typ.Func().
			Variadic(sqlizerType).
			Returns(sqlizerType).
			Build()},
	})
}

// Type constants - use Record for field-only types
var sqlTypeConstType = typ.NewRecord().
	Field("POSTGRES", typ.String).
	Field("MYSQL", typ.String).
	Field("SQLITE", typ.String).
	Field("UNKNOWN", typ.String).
	Build()

// Isolation constants
var isolationConstType = typ.NewRecord().
	Field("DEFAULT", typ.String).
	Field("READ_UNCOMMITTED", typ.String).
	Field("READ_COMMITTED", typ.String).
	Field("WRITE_COMMITTED", typ.String).
	Field("REPEATABLE_READ", typ.String).
	Field("SERIALIZABLE", typ.String).
	Build()

// Type casting submodule for explicit type hints
var asType = typ.NewInterface("sql.as", []typ.Method{
	{Name: "int", Type: typ.Func().
		Param("value", typ.Any).
		Returns(typ.Any).
		Build()},
	{Name: "float", Type: typ.Func().
		Param("value", typ.Any).
		Returns(typ.Any).
		Build()},
	{Name: "string", Type: typ.Func().
		Param("value", typ.Any).
		Returns(typ.Any).
		Build()},
	{Name: "bool", Type: typ.Func().
		Param("value", typ.Any).
		Returns(typ.Any).
		Build()},
	{Name: "null", Type: typ.Func().
		Returns(typ.Any).
		Build()},
	{Name: "json", Type: typ.Func().
		Param("value", typ.Any).
		Returns(typ.Any).
		Build()},
})

// Builder submodule type
var builderType typ.Type

// Builder fields for placeholder formats
var builderFieldsType = typ.NewRecord().
	Field("question", placeholderFormatType).
	Field("dollar", placeholderFormatType).
	Field("at", placeholderFormatType).
	Field("colon", placeholderFormatType).
	Field("default_placeholder", placeholderFormatType).
	Build()

// ModuleTypes returns the type manifest for the sql module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("sql")

	m.DefineType("DB", sqlDBType)
	m.DefineType("Statement", statementType)
	m.DefineType("Transaction", transactionType)
	m.DefineType("DBStats", dbStatsType)
	m.DefineType("ExecuteResult", executeResultType)
	m.DefineType("Sqlizer", sqlizerType)
	m.DefineType("QueryExecutor", queryExecutorType)
	m.DefineType("SelectBuilder", selectBuilderType)
	m.DefineType("InsertBuilder", insertBuilderType)
	m.DefineType("UpdateBuilder", updateBuilderType)
	m.DefineType("DeleteBuilder", deleteBuilderType)

	// Module methods
	moduleMethodsType := typ.NewInterface("sql", []typ.Method{
		{Name: "get", Type: typ.Func().
			Param("dsn", typ.String).
			Returns(sqlDBType, typ.NewOptional(typ.LuaError)).
			Spec(contract.NewSpec().WithEffects(effect.ErrorReturn{ValueIndex: 0, ErrorIndex: 1})).
			Build()},
	})

	// Module fields (constants and submodules)
	moduleFieldsType := typ.NewRecord().
		Field("NULL", typ.Any).
		Field("type", sqlTypeConstType).
		Field("isolation", isolationConstType).
		Field("builder", typ.NewIntersection(builderType, builderFieldsType)).
		Field("as", asType).
		Build()

	m.SetExport(typ.NewIntersection(moduleMethodsType, moduleFieldsType))
	return m
}
