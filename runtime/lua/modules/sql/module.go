package sql

import (
	"sync"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua2api "github.com/wippyai/runtime/api/runtime/lua2"
	servicesql "github.com/wippyai/runtime/api/service/sql"
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
	moduleTable          *lua.LTable
	registration         *lua2api.Registration
	dbMetatable          *lua.LTable
	statementMetatable   *lua.LTable
	transactionMetatable *lua.LTable
	initOnce             sync.Once
)

// Module is the singleton sql module instance.
var Module = &sqlModule{}

type sqlModule struct{}

func (m *sqlModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "sql",
		Description: "SQL database operations",
		Class:       []string{luaapi.ClassStorage, luaapi.ClassIO, luaapi.ClassNondeterministic},
	}
}

func (m *sqlModule) Register(l *lua.LState) *lua2api.Registration {
	initOnce.Do(func() {
		moduleTable = createModuleTable()
		dbMetatable = value.RegisterTypeMethods(nil, dbTypeName,
			map[string]lua.LGFunction{"__tostring": dbToString},
			dbMethods)
		statementMetatable = value.RegisterTypeMethods(nil, statementTypeName,
			map[string]lua.LGFunction{"__tostring": statementToString},
			statementMethods)
		transactionMetatable = value.RegisterTypeMethods(nil, transactionTypeName,
			map[string]lua.LGFunction{"__tostring": transactionToString},
			transactionMethods)
		registration = &lua2api.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})

	return registration
}

func (m *sqlModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind is deprecated. Use lua2api.LoadModule(l, Module) instead.
func Bind(l *lua.LState) {
	lua2api.LoadModule(l, Module)
}

func createModuleTable() *lua.LTable {
	mod := lua.CreateTable(0, 5)

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

	mod.Immutable = true
	return mod
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
