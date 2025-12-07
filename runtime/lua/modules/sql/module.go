package sql

import (
	"sync"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	servicesql "github.com/wippyai/runtime/api/service/sql"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

const (
	TypePostgres = "postgres"
	TypeMySQL    = "mysql"
	TypeSQLite   = "sqlite"
	TypeMSSQL    = "mssql"
	TypeOracle   = "oracle"
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

	types := lua.CreateTable(0, 6)
	types.RawSetString("POSTGRES", lua.LString(TypePostgres))
	types.RawSetString("MYSQL", lua.LString(TypeMySQL))
	types.RawSetString("SQLITE", lua.LString(TypeSQLite))
	types.RawSetString("MSSQL", lua.LString(TypeMSSQL))
	types.RawSetString("ORACLE", lua.LString(TypeOracle))
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
			{Sample: &QueryYield{}, CmdID: sqlapi.CmdQuery},
			{Sample: &ExecuteYield{}, CmdID: sqlapi.CmdExecute},
			{Sample: &PrepareYield{}, CmdID: sqlapi.CmdPrepare},
			{Sample: &BeginYield{}, CmdID: sqlapi.CmdBegin},
			{Sample: &StmtQueryYield{}, CmdID: sqlapi.CmdStmtQuery},
			{Sample: &StmtExecuteYield{}, CmdID: sqlapi.CmdStmtExecute},
			{Sample: &StmtCloseYield{}, CmdID: sqlapi.CmdStmtClose},
			{Sample: &TxQueryYield{}, CmdID: sqlapi.CmdTxQuery},
			{Sample: &TxExecuteYield{}, CmdID: sqlapi.CmdTxExecute},
			{Sample: &TxSavepointYield{}, CmdID: sqlapi.CmdTxExecute},
			{Sample: &TxPrepareYield{}, CmdID: sqlapi.CmdTxPrepare},
			{Sample: &TxCommitYield{}, CmdID: sqlapi.CmdTxCommit},
			{Sample: &TxRollbackYield{}, CmdID: sqlapi.CmdTxRollback},
		}
	},
}

func mapDBTypeFromResourceKind(dbType string) string {
	switch dbType {
	case servicesql.KindPostgres:
		return TypePostgres
	case servicesql.KindMySQL:
		return TypeMySQL
	case servicesql.KindSQLite:
		return TypeSQLite
	case servicesql.KindMSSQL:
		return TypeMSSQL
	case servicesql.KindOracle:
		return TypeOracle
	default:
		return TypeUnknown
	}
}
