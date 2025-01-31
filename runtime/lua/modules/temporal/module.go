package temporal

import (
	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/registry"
	tempsrv "github.com/ponyruntime/pony/service/temporal"
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

// Loader registers the module into Lua state
func (m *Module) Loader(l *lua.LState) int {
	mod := l.NewTable()

	// Register client type
	registerClient(l, mod)

	// Register module functions
	l.SetFuncs(mod, map[string]lua.LGFunction{
		"client": m.getClient,
	})

	l.Push(mod)
	return 1
}

// getClient implements temporal.client() constructor
func (m *Module) getClient(l *lua.LState) int {
	clientID := l.CheckString(1)
	if clientID == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("client id is required"))
		return 2
	}

	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context available"))
		return 2
	}

	temporalSvc, ok := ctx.Value(ctxapi.TemporalCtx).(*tempsrv.Manager)
	if !ok || temporalSvc == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("temporal service not available"))
		return 2
	}

	client, err := temporalSvc.GetClient(registry.ID(clientID))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	ud := l.NewUserData()
	ud.Value = &Client{
		client: client,
		log:    m.log.With(zap.String("client_id", clientID)),
	}
	l.SetMetatable(ud, l.GetTypeMetatable("Temporal.Client"))
	l.Push(ud)
	return 1
}
