package btea

import (
	"sync"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/channel"
	"github.com/wippyai/runtime/runtime/lua/engine/subscribe"
	"github.com/wippyai/runtime/runtime/lua/modules/btea/models"
	"github.com/wippyai/runtime/runtime/lua/modules/btea/models/list"
	"github.com/wippyai/runtime/runtime/lua/modules/btea/protocol"
	"github.com/wippyai/runtime/runtime/lua/modules/btea/render"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Module provides Lua bindings for Bubbletea TUI framework
type Module struct {
	log         *zap.Logger
	once        sync.Once
	moduleTable *lua.LTable
}

// NewBteaModule creates a new bubbletea module
func NewBteaModule(log *zap.Logger) *Module {
	return &Module{log: log}
}

func (m *Module) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "btea",
		Description: "Bubbletea TUI framework bindings",
		Class:       []string{luaapi.ClassIO},
	}
}

// Loader is the entry point for loading the module into Lua
func (m *Module) Loader(l *lua.LState) int {
	m.once.Do(func() {
		mod := l.NewTable()

		protocol.RegisterCmd(l, mod)
		protocol.RegisterKeyBinding(l, mod)

		render.RegisterTextUtils(l, mod)
		render.RegisterStyle(l, mod)
		render.RegisterZone(l, mod)

		models.RegisterTextInput(l, mod)
		models.RegisterTextArea(l, mod)
		models.RegisterPaginator(l, mod)
		models.RegisterViewport(l, mod)
		models.RegisterTable(l, mod)

		list.RegisterList(l, mod)

		models.RegisterHelp(l, mod)
		models.RegisterSpinner(l, mod)
		models.RegisterProgress(l, mod)

		mod.RawSetString("events", l.NewFunction(m.events))
		mod.Immutable = true
		m.moduleTable = mod
	})
	l.Push(m.moduleTable)
	return 1
}

// btea events handler (internal)
func (m *Module) events(l *lua.LState) int {
	return subscribe.Subscribe(l, channel.Named("btea.events", 1), "@btea/events")
}
