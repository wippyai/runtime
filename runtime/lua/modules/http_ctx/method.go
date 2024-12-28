package httphandler

import (
	"context"

	"github.com/ponyruntime/go-lua"
	actx "github.com/ponyruntime/pony/api/context"
)

// we can use it outside of the module structure
func (m *Module) method(l *lua.LState) int {
	m.log.Debug("called http handler 'method' function")
	ctx := l.Context()

	carrier := m.FromContext(ctx)
	if carrier == nil {
		l.Push(lua.LString("no context found"))
		return 1
	}

	l.Push(lua.LString(carrier.Request().Method))
	return 1
}

func (m *Module) FromContext(ctx context.Context) *actx.HTTPContextCarrier {
	if ctx == nil {
		return nil
	}

	val := ctx.Value(actx.HttpHandlerKey)
	// bad check actually, because any is an interface, but worth checking for the safety
	if val == nil {
		return nil
	}

	carrier, ok := val.(*actx.HTTPContextCarrier)
	if !ok {
		return nil
	}

	return carrier
}
