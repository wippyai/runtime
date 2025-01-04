package ctx

import (
	ctxapi "github.com/ponyruntime/pony/api/context" // Make sure this import path is correct
	transcoder "github.com/ponyruntime/pony/pkg/payload/lua"
	"github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Module (ctx) gets or sets a context value found by a given key.
type Module struct{}

func New(log *zap.Logger) *Module {
	return &Module{}
}

// Name returns the module name
func (m *Module) Name() string {
	return "ctx"
}

// Loader is the entry point for loading the plugin
func (m *Module) Loader(l *lua.LState) int {
	t := l.NewTable()

	lapi := map[string]lua.LGFunction{
		"get": m.get,
		"set": m.set,
	}

	l.SetFuncs(t, lapi)
	l.Push(t)
	return 1
}

func (m *Module) get(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.ArgError(1, "no context found")
		return 0
	}

	k := l.CheckString(1)
	if k == "" {
		l.ArgError(1, "empty key provided")
		return 0
	}

	ctxter, ok := ctx.Value(ctxapi.ValuesCtx).(*ctxapi.Contexter[any])
	if !ok {
		l.ArgError(1, "invalid context")
		return 0
	}

	vv, ok := ctxter.Value(k)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("no value found for key: " + k))
		return 2
	}

	l.Push(transcoder.GoToLua(l, vv))
	l.Push(lua.LNil)

	return 2
}

func (m *Module) set(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.ArgError(1, "no context found")
		return 0
	}

	k := l.CheckString(1)
	if k == "" {
		l.ArgError(1, "empty key provided")
		return 0
	}

	ctxter, ok := ctx.Value(ctxapi.ValuesCtx).(*ctxapi.Contexter[any])
	if !ok {
		l.ArgError(1, "invalid context")
		return 0
	}

	ctxter.WithValue(k, transcoder.ToGoAny(l.CheckAny(2)))

	l.Push(lua.LTrue)
	l.Push(lua.LNil)

	return 2
}
