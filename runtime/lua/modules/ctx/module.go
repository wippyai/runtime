package ctx

import (
	"github.com/ponyruntime/go-lua"
	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"go.uber.org/zap"
)

// Module (ctx) accepts 1 argument, a string, and returns a context value found by that key.
type Module[T any] struct {
	log *zap.Logger
}

func New[T any](log *zap.Logger) *Module[T] {
	return &Module[T]{
		// TODO: context might have a cancel function, do we need to handle context cancellation and propagate it to the lua?
		log: log,
	}
}

func (m *Module[T]) get(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context found"))
		return 2
	}

	k := l.CheckString(1)
	if k == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("empty key provided"))
		return 2
	}

	ctxer := ctx.Value(ctxapi.ContexterKey)
	if ctxer == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no contexter found"))
		return 2
	}

	sharedvals := ctxer.(*ctxapi.Contexter[T])
	if sharedvals == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no shared values found"))
		return 2
	}

	if _, ok := sharedvals.Value(k); !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("no value found for key: " + k))
		return 2
	}

	vv, _ := sharedvals.Value(k)
	l.Push(engine.GoToLua(l, vv))
	l.Push(lua.LNil)

	return 2
}
