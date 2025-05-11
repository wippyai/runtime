package upstream

import (
	"context"
	"fmt"

	luaconv "github.com/ponyruntime/pony/system/payload/lua"

	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/payload"
	lua "github.com/yuin/gopher-lua"
)

// Module provides functionality to send values upstream from Lua
type Module struct{}

// ctx is the context key for the upstream channel
//

var Ctx = &ctxapi.Key{Name: "upstream"}

// NewUpstreamModule creates a new upstream module instance
func NewUpstreamModule() *Module {
	return &Module{}
}

// Name returns the module name
func (m *Module) Name() string {
	return "upstream"
}

// Loader registers the module functions
func (m *Module) Loader(l *lua.LState) int {
	// Spawn module table
	mod := l.NewTable()

	// Register functions
	l.SetField(mod, "send", l.NewFunction(m.send))

	// Register module
	l.Push(mod)
	return 1
}

// WithUpstreamChannel adds an upstream channel to the context
func WithUpstreamChannel(ctx context.Context, ch chan<- payload.Payload) context.Context {
	return context.WithValue(ctx, Ctx, ch)
}

// GetUpstreamChannel retrieves the upstream channel from context
func GetUpstreamChannel(l *lua.LState) (chan<- payload.Payload, error) {
	ctx := l.Context()

	if ctx == nil {
		return nil, fmt.Errorf("no context found")
	}

	ch, ok := ctx.Value(Ctx).(chan<- payload.Payload)
	if !ok {
		return nil, fmt.Errorf("no upstream channel found in context")
	}

	return ch, nil
}

// send implements upstream.send(value)
func (m *Module) send(l *lua.LState) int {
	ch, err := GetUpstreamChannel(l)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Create a Lua payload from the input value
	p := luaconv.ExportPayload(l.CheckAny(1))

	select {
	case ch <- p:
		l.Push(lua.LTrue)
		l.Push(lua.LNil)
	default:
		l.Push(lua.LFalse)
		l.Push(lua.LString("channel full"))
	}

	return 2
}
