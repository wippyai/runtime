package sql

import (
	"sync"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/service/sql"
	"github.com/wippyai/runtime/runtime/lua/modules/sql/builder"
	"github.com/wippyai/runtime/runtime/lua/modules/sql/sqlutil"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

const (
	// TypePostgres identifies a PostgreSQL database
	TypePostgres = "postgres"

	// TypeMySQL identifies a MySQL database
	TypeMySQL = "mysql"

	// TypeSQLite identifies a SQLite database
	TypeSQLite = "sqlite"

	// TypeMSSQL identifies a Microsoft SQL Server database
	TypeMSSQL = "mssql"

	// TypeOracle identifies an Oracle database
	TypeOracle = "oracle"

	// TypeUnknown for unrecognized database types
	TypeUnknown = "unknown"

	// Isolation level constants
	IsolationDefault         = "default"
	IsolationReadUncommitted = "read_uncommitted"
	IsolationReadCommitted   = "read_committed"
	IsolationWriteCommitted  = "write_committed"
	IsolationRepeatableRead  = "repeatable_read"
	IsolationSerializable    = "serializable"
)

// Module represents the SQL module for Lua
type Module struct {
	log         *zap.Logger
	moduleTable *lua.LTable
	once        sync.Once
}

// NewSQLModule creates a new SQL module
func NewSQLModule(log *zap.Logger) *Module {
	if log == nil {
		log = zap.NewNop()
	}
	return &Module{
		log: log,
	}
}

func (m *Module) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "sql",
		Description: "SQL database access",
		Class:       []string{luaapi.ClassStorage, luaapi.ClassIO},
	}
}

// Loader is the entry point for loading the module into Lua
func (m *Module) Loader(l *lua.LState) int {
	m.once.Do(func() {
		m.initModuleTable(l)
	})

	l.Push(m.moduleTable)
	return 1
}

// initModuleTable creates and initializes the module table once
func (m *Module) initModuleTable(l *lua.LState) {
	// Create main module table with exact capacity
	mod := l.CreateTable(0, 5) // 5 elements: get function, type table, isolation table, NULL, and submodules

	// Register main DB functions
	registerDB(l, mod, m.log)

	// Register statement and transaction methods
	registerStatement(l, m.log)
	registerTransaction(l, m.log)

	// Create NULL value
	nullUserData := l.NewUserData()
	nullUserData.Value = "SQL_NULL"
	mod.RawSetString("NULL", nullUserData)

	// Create database type constants table
	types := l.CreateTable(0, 6) // 6 database types
	types.RawSetString("POSTGRES", lua.LString(TypePostgres))
	types.RawSetString("MYSQL", lua.LString(TypeMySQL))
	types.RawSetString("SQLITE", lua.LString(TypeSQLite))
	types.RawSetString("MSSQL", lua.LString(TypeMSSQL))
	types.RawSetString("ORACLE", lua.LString(TypeOracle))
	types.RawSetString("UNKNOWN", lua.LString(TypeUnknown))
	types.Immutable = true
	mod.RawSetString("type", types)

	// Create isolation level constants table
	isolation := l.CreateTable(0, 6) // 6 isolation levels
	isolation.RawSetString("DEFAULT", lua.LString(IsolationDefault))
	isolation.RawSetString("READ_UNCOMMITTED", lua.LString(IsolationReadUncommitted))
	isolation.RawSetString("READ_COMMITTED", lua.LString(IsolationReadCommitted))
	isolation.RawSetString("WRITE_COMMITTED", lua.LString(IsolationWriteCommitted))
	isolation.RawSetString("REPEATABLE_READ", lua.LString(IsolationRepeatableRead))
	isolation.RawSetString("SERIALIZABLE", lua.LString(IsolationSerializable))
	isolation.Immutable = true
	mod.RawSetString("isolation", isolation)

	// Register sqlutil as submodule
	sqlutil.RegisterAsModule(l, mod)

	// Register builder submodule
	builder.RegisterBuilderModule(l, mod)

	// Make the main module table immutable
	mod.Immutable = true

	m.moduleTable = mod
}

// mapDBTypeFromResourceKind maps a registry.Kind to a database type string
func mapDBTypeFromResourceKind(dbType string) string {
	switch dbType {
	case sql.KindPostgres:
		return TypePostgres
	case sql.KindMySQL:
		return TypeMySQL
	case sql.KindSQLite:
		return TypeSQLite
	case sql.KindMSSQL:
		return TypeMSSQL
	case sql.KindOracle:
		return TypeOracle
	default:
		return TypeUnknown
	}
}
