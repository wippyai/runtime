package eventbus

import (
	"github.com/ponyruntime/pony/api/event"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

const (
	subscriptionMetatable = "event.Subscription"
)

// Module represents the event bus module for Lua
type Module struct {
	log *zap.Logger
}

// NewEventBusModule creates a new event bus module
func NewEventBusModule(log *zap.Logger) *Module {
	return &Module{
		log: log.Named("eventbus"),
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "event"
}

// Loader is the entry point for loading the module into Lua
func (m *Module) Loader(l *lua.LState) int {
	// Create module table
	mod := l.NewTable()

	// Register module functions
	mod.RawSetString("get_bus", l.NewFunction(m.getBus))

	// Register subscription metatable
	mt := l.NewTypeMetatable(subscriptionMetatable)
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"channel": m.subChannel,
		"close":   m.subClose,
	}))

	// Set module as return value
	l.Push(mod)
	return 1
}

// getBus retrieves the event bus from the context
func (m *Module) getBus(l *lua.LState) int {
	// Get event bus from context
	ctx := l.Context()
	bus := event.GetBus(ctx)
	if bus == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("event bus not found in context"))
		return 2
	}

	// Create bus table with functions
	busTable := l.NewTable()
	l.SetFuncs(busTable, map[string]lua.LGFunction{
		"subscribe": func(l *lua.LState) int {
			return m.subscribe(l, bus)
		},
	})

	l.Push(busTable)
	return 1
}
