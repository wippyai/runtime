package temporal

import (
	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/runtime/temporal"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Module represents the temporal Lua module
type Module struct {
	log *zap.Logger
}

// NewTemporalModule creates a new temporal module
func NewTemporalModule(logger *zap.Logger) *Module {
	return &Module{
		log: logger,
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "temporal"
}

// Loader is the entry point for loading the module into Lua
func (m *Module) Loader(l *lua.LState) int {
	mod := l.NewTable()

	// Register core functions
	l.SetFuncs(mod, map[string]lua.LGFunction{
		"client": m.getClient,
	})

	l.Push(mod)
	return 1
}

// getClient implements temporal.client() in Lua
func (m *Module) getClient(l *lua.LState) int {
	// Get client ID from arguments
	clientID := l.CheckString(1)
	if clientID == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("client id is required"))
		return 2
	}

	// Get temporal service from context
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context available"))
		return 2
	}

	temporalSvc, ok := ctx.Value(ctxapi.TemporalCtx).(temporal.Service)
	if !ok || temporalSvc == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("temporal service not available"))
		return 2
	}

	// Get the client
	client, err := temporalSvc.GetClient(clientID)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Create and return client wrapper
	l.Push(newClient(l, client, m.log.With(zap.String("client_id", clientID))))
	return 1
}
