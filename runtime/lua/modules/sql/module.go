package sql

import (
	"github.com/ponyruntime/pony/api/service/sql"
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
)

// mapDBTypeFromResourceKind maps a registry.Kind to a database type string
func mapDBTypeFromResourceKind(dbType string) string {
	switch dbType {
	case string(sql.KindPostgres):
		return TypePostgres
	case string(sql.KindMySQL):
		return TypeMySQL
	case string(sql.KindSQLite):
		return TypeSQLite
	case string(sql.KindMSSQL):
		return TypeMSSQL
	case string(sql.KindOracle):
		return TypeOracle
	default:
		return TypeUnknown
	}
}

// NewSQLModule creates a new SQL module
func NewSQLModule(log *zap.Logger) *Module {
	return &Module{
		log: log,
	}
}

// Module represents the SQL module for Lua
type Module struct {
	log *zap.Logger
}

// Name returns the module name
func (m *Module) Name() string {
	return "sql"
}

// Loader is the entry point for loading the module into Lua
func (m *Module) Loader(l *lua.LState) int {
	mod := l.CreateTable(0, 3) // 3 elements: NULL, type table, and DB functions

	registerDB(l, mod, m.log)

	// Register statement and transaction functions
	registerStatement(l, m.log)
	registerTransaction(l, m.log)

	// Create NULL value
	nullUserData := l.NewUserData()
	nullUserData.Value = "SQL_NULL" // Marker value
	mod.RawSetString("NULL", nullUserData)

	// Create database type constants table with exact capacity
	types := l.CreateTable(0, 6) // 6 database types

	// Add database types directly
	types.RawSetString("postgres", lua.LString(TypePostgres))
	types.RawSetString("mysql", lua.LString(TypeMySQL))
	types.RawSetString("sqlite", lua.LString(TypeSQLite))
	types.RawSetString("mssql", lua.LString(TypeMSSQL))
	types.RawSetString("oracle", lua.LString(TypeOracle))
	types.RawSetString("unknown", lua.LString(TypeUnknown))

	// Add types table to module
	mod.RawSetString("type", types)

	// Return module
	l.Push(mod)
	return 1
}
