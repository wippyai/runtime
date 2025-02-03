package btea

import (
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Module provides Lua bindings for Bubbletea TUI framework
type Module struct {
	log *zap.Logger
}

// NewBteaModule creates a new bubbletea module
func NewBteaModule(log *zap.Logger) *Module {
	return &Module{log: log}
}

// Name returns the module name
func (m *Module) Name() string {
	return "btea"
}

// Loader is the entry point for loading the module into Lua
func (m *Module) Loader(l *lua.LState) int {
	// Create main module table
	mod := l.NewTable()

	RegisterTextUtils(l, mod)
	RegisterStyle(l, mod)
	RegisterCmd(l, mod)
	RegisterBinding(l, mod)

	// editable elements
	RegisterTextInput(l, mod)
	RegisterSpinner(l, mod)
	RegisterProgress(l, mod)
	RegisterViewport(l, mod)
	RegisterHelp(l, mod)

	// extended visuals
	RegisterTable(l, mod)
	RegisterTree(l, mod)

	// Set the module
	l.Push(mod)
	return 1
}
