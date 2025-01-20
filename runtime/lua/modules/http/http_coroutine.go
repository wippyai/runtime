package http

import (
	"context"
	"github.com/ponyruntime/pony/internal/closer"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/lua/modules/stream"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"io"
	"net/http"
)

// executeRequestYield performs HTTP request asynchronously using coroutines
func (m *Module) executeRequestYield(l *lua.LState, req *http.Request, opts *requestOptions) int {
	ctx := req.Context()
	if l.Context() != nil {
		ctx = l.Context()
	}

	cleanup := closer.FromContext(ctx)
	if cleanup == nil {
		// should never happen
		ctx, cleanup = closer.WithContext(ctx)
		defer func() { _ = cleanup.Close() }()
	}

	if opts.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.timeout)
		cleanup.Add(func() error { cancel(); return nil })
	}

	req = req.WithContext(ctx)

	m.log.Debug("executing async request",
		zap.String("method", req.Method),
		zap.String("url", req.URL.String()),
	)

	coroutine.Wrap(l, func() engine.Result {
		resp, err := m.client.Do(req)
		if err != nil {
			return engine.Result{Result: []lua.LValue{lua.LNil, lua.LString(err.Error())}}
		}
		cleanup.Add(resp.Body.Close)

		if opts.stream != nil {
			return m.handleStreamResponseAsync(l, resp, opts.stream, ctx)
		}
		return m.handleRegularResponseAsync(l, resp)
	})

	return -1
}

func (m *Module) handleStreamResponseAsync(
	l *lua.LState,
	resp *http.Response,
	streamOpts *stream.Options,
	ctx context.Context,
) engine.Result {
	s, err := stream.NewStream(ctx, resp.Body, streamOpts)
	if err != nil {
		return engine.Result{Result: []lua.LValue{lua.LNil, lua.LString(err.Error())}}
	}

	luaStream := &stream.LuaStream{Stream: s}
	ud := l.NewUserData()
	ud.Value = luaStream
	l.SetMetatable(ud, l.GetTypeMetatable("Stream"))

	return engine.Result{Result: []lua.LValue{newResponseWithStream(resp, ud, l), lua.LNil}}
}

func (m *Module) handleRegularResponseAsync(l *lua.LState, resp *http.Response) engine.Result {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return engine.Result{Result: []lua.LValue{lua.LNil, lua.LString(err.Error())}}
	}

	return engine.Result{Result: []lua.LValue{newResponse(resp, &body, len(body), l), lua.LNil}}
}
