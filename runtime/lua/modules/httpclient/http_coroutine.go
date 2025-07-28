package httpclient

import (
	"context"
	"io"
	"net/http"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	"github.com/ponyruntime/pony/runtime/lua/modules/stream"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// executeRequestYield performs HTTP request asynchronously using coroutines
func (m *Module) executeRequestYield(l *lua.LState, req *http.Request, opts *requestOptions) int {
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("unit of work not found in context")
		return 0
	}

	var closer context.CancelFunc
	ctx := uw.Context()
	if opts.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(uw.Context(), opts.timeout)
		closer = uw.AddCleanup(func() error { cancel(); return nil })
	}

	req = req.WithContext(ctx)

	m.log.Debug("executing async request",
		zap.String("method", req.Method),
		zap.String("url", req.URL.String()),
	)

	coroutine.Wrap(l, func() *engine.Update {
		// Get appropriate client based on timeout and unix socket
		client := m.getClientForTimeout(opts.timeout, opts.unixSocket)

		resp, err := client.Do(req)
		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		if opts.stream {
			return m.handleStreamResponseAsync(ctx, l, resp, uw, closer)
		}
		defer closer()
		defer func() { _ = resp.Body.Close() }()

		return m.handleRegularResponseAsync(l, resp)
	})

	return -1
}

func (m *Module) handleStreamResponseAsync(
	ctx context.Context,
	l *lua.LState,
	r *http.Response,
	uw engine.UnitOfWork,
	closer context.CancelFunc,
) *engine.Update {
	s, err := stream.NewStream(ctx, r.Body)
	if err != nil {
		return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
	}

	// Create the LuaStream which will be managed by the UoW
	luaStream := stream.NewLuaStream(uw, s, closer)
	ud := l.NewUserData()
	ud.Value = luaStream
	ud.Metatable = value.GetTypeMetatable(l, "Stream")

	return engine.NewUpdate(nil, []lua.LValue{newResponseWithStream(r, ud, l), lua.LNil}, nil)
}

func (m *Module) handleRegularResponseAsync(l *lua.LState, resp *http.Response) *engine.Update {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
	}
	_ = resp.Body.Close()

	return engine.NewUpdate(nil, []lua.LValue{newResponse(resp, &body, len(body), l), lua.LNil}, nil)
}
