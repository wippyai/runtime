package btea

import (
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/models"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/render"
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

	// Protocol
	protocol.RegisterCmd(l, mod)
	protocol.RegisterKeyBinding(l, mod)

	// Styling
	render.RegisterTextUtils(l, mod)
	render.RegisterStyle(l, mod)
	render.RegisterZone(l, mod)

	// editable elements
	models.RegisterTextInput(l, mod)
	models.RegisterTextArea(l, mod)
	models.RegisterPaginator(l, mod)
	models.RegisterViewport(l, mod)
	models.RegisterTable(l, mod)

	// additional view components
	models.RegisterHelp(l, mod)
	models.RegisterSpinner(l, mod)
	models.RegisterProgress(l, mod)

	// Set the module
	l.Push(mod)
	return 1
}
