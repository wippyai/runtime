// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"sync"

	lua "github.com/wippyai/go-lua"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

const (
	TypePostgres = "postgres"
	TypeMySQL    = "mysql"
	TypeSQLite   = "sqlite"
	TypeUnknown  = "unknown"

	IsolationDefault         = "default"
	IsolationReadUncommitted = "read_uncommitted"
	IsolationReadCommitted   = "read_committed"
	IsolationWriteCommitted  = "write_committed"
	IsolationRepeatableRead  = "repeatable_read"
	IsolationSerializable    = "serializable"

	dbTypeName          = "sql.DB"
	statementTypeName   = "sql.Statement"
	transactionTypeName = "sql.Transaction"
)

var (
	moduleTable *lua.LTable
	initOnce    sync.Once
)

func init() {
	value.RegisterTypeMethods(nil, dbTypeName, nil, dbMethods)
	value.RegisterTypeMethods(nil, statementTypeName, nil, statementMethods)
	value.RegisterTypeMethods(nil, transactionTypeName, nil, transactionMethods)
}

func initModuleTable() {
	mod := lua.CreateTable(0, 6)

	mod.RawSetString("get", lua.LGoFunc(sqlGet))

	nullUD := &lua.LUserData{Value: "SQL_NULL"}
	mod.RawSetString("NULL", nullUD)

	types := lua.CreateTable(0, 4)
	types.RawSetString("POSTGRES", lua.LString(TypePostgres))
	types.RawSetString("MYSQL", lua.LString(TypeMySQL))
	types.RawSetString("SQLITE", lua.LString(TypeSQLite))
	types.RawSetString("UNKNOWN", lua.LString(TypeUnknown))
	types.Immutable = true
	mod.RawSetString("type", types)

	isolation := lua.CreateTable(0, 6)
	isolation.RawSetString("DEFAULT", lua.LString(IsolationDefault))
	isolation.RawSetString("READ_UNCOMMITTED", lua.LString(IsolationReadUncommitted))
	isolation.RawSetString("READ_COMMITTED", lua.LString(IsolationReadCommitted))
	isolation.RawSetString("WRITE_COMMITTED", lua.LString(IsolationWriteCommitted))
	isolation.RawSetString("REPEATABLE_READ", lua.LString(IsolationRepeatableRead))
	isolation.RawSetString("SERIALIZABLE", lua.LString(IsolationSerializable))
	isolation.Immutable = true
	mod.RawSetString("isolation", isolation)

	registerAsSubmodule(mod)
	registerBuilderSubmodule(mod)

	mod.Immutable = true
	moduleTable = mod
}

// Module is the sql module definition.
var Module = &luaapi.ModuleDef{
	Name:        "sql",
	Description: "SQL database operations",
	Class:       []string{luaapi.ClassStorage, luaapi.ClassIO, luaapi.ClassNondeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		initOnce.Do(initModuleTable)
		return moduleTable, []luaapi.YieldType{
			{Sample: &QueryYield{}, CmdID: sqlapi.Query},
			{Sample: &ExecuteYield{}, CmdID: sqlapi.Execute},
			{Sample: &PrepareYield{}, CmdID: sqlapi.Prepare},
			{Sample: &BeginYield{}, CmdID: sqlapi.Begin},
			{Sample: &StmtQueryYield{}, CmdID: sqlapi.StmtQuery},
			{Sample: &StmtExecuteYield{}, CmdID: sqlapi.StmtExecute},
			{Sample: &StmtCloseYield{}, CmdID: sqlapi.StmtClose},
			{Sample: &TxQueryYield{}, CmdID: sqlapi.TxQuery},
			{Sample: &TxExecuteYield{}, CmdID: sqlapi.TxExecute},
			{Sample: &TxSavepointYield{}, CmdID: sqlapi.TxExecute},
			{Sample: &TxPrepareYield{}, CmdID: sqlapi.TxPrepare},
			{Sample: &TxCommitYield{}, CmdID: sqlapi.TxCommit},
			{Sample: &TxRollbackYield{}, CmdID: sqlapi.TxRollback},
		}
	},
	Types: ModuleTypes,
}

func mapDBTypeFromResourceKind(dbType string) string {
	switch dbType {
	case sqlapi.Postgres:
		return TypePostgres
	case sqlapi.MySQL:
		return TypeMySQL
	case sqlapi.SQLite:
		return TypeSQLite
	default:
		return TypeUnknown
	}
}
