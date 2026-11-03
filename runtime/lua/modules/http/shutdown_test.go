// SPDX-License-Identifier: MPL-2.0

package http

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	httpservice "github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/runtime/lua/engine"
	httpserver "github.com/wippyai/runtime/service/http"
	"github.com/wippyai/runtime/system/scheduler"
	"github.com/wippyai/runtime/system/scheduler/pool/inline"
	"go.uber.org/zap"
)

// luaFunctions runs each function ID on its own inline pool, forking the
// execution frame with the task pairs the way the function registry does.
type luaFunctions map[registry.ID]*inline.Pool

func (f luaFunctions) Call(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
	ctx, fc := ctxapi.ForkFrameContext(ctx)
	defer ctxapi.ReleaseFrameContext(fc)
	if err := fc.SetMultiple(task.Context...); err != nil {
		return nil, err
	}
	return f[task.ID].Call(ctx, "handler", nil)
}

func newLuaHandlerPool(t *testing.T, script string) *inline.Pool {
	t.Helper()
	factory := func() (process.Process, error) {
		return engine.NewFactory(engine.FactoryConfig{
			Script:     script,
			ScriptName: "shutdown_test",
			ModuleBinders: append(engine.CoreBinders(), func(l *lua.LState) error {
				bind(l)
				return nil
			}),
		})()
	}
	pool, err := inline.New(factory, scheduler.NewRegistry())
	require.NoError(t, err)
	t.Cleanup(pool.Stop)
	return pool
}

func listenAddr(t *testing.T, status <-chan any) string {
	t.Helper()
	for {
		select {
		case msg := <-status:
			if s, ok := msg.(string); ok && strings.HasPrefix(s, "service listening on ") {
				return strings.TrimPrefix(s, "service listening on ")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("server did not report its listen address")
		}
	}
}

func TestServerStopEndsLuaStreamsAndCompletesNormalRequests(t *testing.T) {
	for _, transfer := range []string{"sse", "chunked"} {
		t.Run(transfer, func(t *testing.T) {
			streamID := registry.NewID("test", "stream")
			normalID := registry.NewID("test", "normal")
			funcs := luaFunctions{
				streamID: newLuaHandlerPool(t, `
					local function handler()
						local res = http.response()
						assert(res:set_transfer("`+transfer+`") == nil)
						assert(res:write("ready\n") == nil)
						assert(res:flush() == nil)
						channel.new():receive()
					end
					return {handler = handler}
				`),
				normalID: newLuaHandlerPool(t, `
					local function handler()
						local req = http.request()
						local res = http.response()
						local body, err = req:body()
						assert(err == nil, tostring(err))
						res:set_status(http.STATUS.OK)
						assert(res:write("done:" .. body) == nil)
					end
					return {handler = handler}
				`),
			}

			normalServing := make(chan struct{})
			endpoints, err := httpserver.NewEndpointFactory(funcs)
			require.NoError(t, err)
			server, err := httpserver.NewServerService(registry.NewID("test", "server"),
				&httpservice.ServerConfig{Addr: "127.0.0.1:0"}, httpserver.NewMiddlewareRegistry(zap.NewNop()))
			require.NoError(t, err)
			routerID := registry.NewID("test", "router")
			require.NoError(t, server.UpsertRouter(routerID, &httpservice.RouterConfig{Prefix: "/api"}))
			for _, ep := range []struct {
				id     registry.ID
				path   string
				method string
			}{{streamID, "/stream", http.MethodGet}, {normalID, "/normal", http.MethodPost}} {
				handler, err := endpoints.CreateHandler(context.Background(), &httpservice.EndpointConfig{
					Meta: map[string]any{httpservice.RouterID: routerID.String()},
					Path: ep.path, Method: ep.method, Func: ep.id,
				})
				require.NoError(t, err)
				if ep.id == normalID {
					inner := handler
					handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						close(normalServing)
						inner.ServeHTTP(w, r)
					})
				}
				require.NoError(t, server.UpsertEndpoint(routerID, ep.id, ep.path, ep.method, handler))
			}
			require.NoError(t, server.Rebuild(context.Background()))

			ctx, cancel := context.WithTimeout(ctxapi.NewRootContext(), 20*time.Second)
			defer cancel()
			status, err := server.Start(ctx)
			require.NoError(t, err)
			addr := listenAddr(t, status)

			streamReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/api/stream", nil)
			require.NoError(t, err)
			streamResp, err := http.DefaultClient.Do(streamReq)
			require.NoError(t, err)
			defer streamResp.Body.Close()
			stream := bufio.NewReader(streamResp.Body)
			line, err := stream.ReadString('\n')
			require.NoError(t, err)
			require.Equal(t, "ready\n", line)

			bodyReader, bodyWriter := io.Pipe()
			normalReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/api/normal", bodyReader)
			require.NoError(t, err)
			type normalResult struct {
				body   string
				status int
			}
			normalDone := make(chan normalResult, 1)
			go func() {
				resp, err := http.DefaultClient.Do(normalReq)
				if !assert.NoError(t, err) {
					normalDone <- normalResult{}
					return
				}
				defer resp.Body.Close()
				body, err := io.ReadAll(resp.Body)
				assert.NoError(t, err)
				normalDone <- normalResult{status: resp.StatusCode, body: string(body)}
			}()
			_, err = bodyWriter.Write([]byte("part1-"))
			require.NoError(t, err)
			<-normalServing

			stopCtx, stopCancel := context.WithTimeout(ctx, 5*time.Second)
			defer stopCancel()
			stopped := make(chan error, 1)
			began := time.Now()
			go func() { stopped <- server.Stop(stopCtx) }()

			streamEnded := make(chan error, 1)
			go func() {
				rest, err := io.ReadAll(stream)
				assert.Empty(t, rest)
				streamEnded <- err
			}()
			select {
			case err := <-streamEnded:
				require.NoError(t, err, "the stream ends with a clean end of body")
			case <-time.After(2 * time.Second):
				t.Fatal("the stream is still open after the server began its shutdown")
			}

			select {
			case err := <-stopped:
				t.Fatalf("stop returned before the in-flight request completed: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			_, err = bodyWriter.Write([]byte("part2"))
			require.NoError(t, err)
			require.NoError(t, bodyWriter.Close())

			normal := <-normalDone
			assert.Equal(t, http.StatusOK, normal.status)
			assert.Equal(t, "done:part1-part2", normal.body)

			require.NoError(t, <-stopped)
			assert.Less(t, time.Since(began), 2*time.Second)
		})
	}
}

func TestResponseStreamAfterServerShutdown(t *testing.T) {
	shutDown := make(chan struct{})
	close(shutDown)
	ended := func() (<-chan struct{}, error) { return shutDown, nil }

	for _, test := range []struct {
		start  func() (<-chan struct{}, error)
		name   string
		script string
	}{
		{name: "writes on an sse stream end", start: ended, script: `
			assert(res:set_transfer(http.TRANSFER.SSE) == nil)
			local writes = {
				function() return res:write_event({name = "tick", data = {}}) end,
				function() return res:write("data") end,
				function() return res:write_json({}) end,
				function() return res:flush() end,
			}
			for i, write in ipairs(writes) do
				local err = write()
				assert(err ~= nil, "write " .. i .. " after shutdown must fail")
				assert(err:kind() == "Canceled", err:kind())
				assert(err:retryable() == true)
			end
		`},
		{name: "first event switches to an ended stream", start: ended, script: `
			local err = res:write_event({name = "tick", data = {}})
			assert(err ~= nil and err:kind() == "Canceled", tostring(err))
		`},
		{name: "chunked stream ends", start: ended, script: `
			assert(res:set_transfer(http.TRANSFER.CHUNKED) == nil)
			local err = res:write("data")
			assert(err ~= nil and err:kind() == "Canceled", tostring(err))
		`},
		{name: "non-streaming response keeps writing", start: ended, script: `
			assert(res:write("data") == nil)
			assert(res:flush() == nil)
		`},
		{name: "stream without a server shutdown signal is refused", start: func() (<-chan struct{}, error) {
			return nil, httpservice.ErrServerShutdownNotFound
		}, script: `
			local err = res:set_transfer(http.TRANSFER.SSE)
			assert(err ~= nil and err:kind() == "Internal", tostring(err))
			err = res:write_event({name = "tick", data = {}})
			assert(err ~= nil and err:kind() == "Internal", tostring(err))
		`},
	} {
		t.Run(test.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()
			bind(l)
			ctx, frame := newTestContext()
			reqCtx := httpservice.NewRequestContext(httptest.NewRequestWithContext(ctx, "GET", "/", nil), httptest.NewRecorder())
			reqCtx.SetStreamStart(test.start)
			require.NoError(t, frame.Set(httpservice.RequestKey(), reqCtx))
			l.SetContext(ctx)
			require.NoError(t, l.DoString(`local res = http.response()`+"\n"+test.script))
		})
	}
}
