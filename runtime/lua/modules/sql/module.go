package sql

import (
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// NewSQLModule creates a new SQL module
func NewSQLModule(log *zap.Logger) *Module {
	return &Module{
		log: log.Named("sql"),
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
	// Create module table
	mod := l.NewTable()

	// Register DB functions
	registerDB(l, mod, m.log)

	// Register statement functions
	registerStatement(l, m.log)

	// Register transaction functions
	registerTransaction(l, m.log)

	// Set module as return value
	l.Push(mod)
	return 1
}
