// SPDX-License-Identifier: MPL-2.0

package sserelay

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	contextapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	httpapi "github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/api/topology"
	httpmodule "github.com/wippyai/runtime/runtime/lua/modules/http"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestMiddlewareLuaManagedTargetExit(t *testing.T) {
	target := mustPID("{n1@app:llm|lua-target-exit}")
	host := newMockHost()
	node := newMockNode()
	topo := newMockTopology()
	core, logs := observer.New(zap.WarnLevel)
	node.setSendError(LeaveTopic, target, process.ErrProcessNotFound)
	manager := NewSSERelay(context.Background(), zap.New(core), &testPIDGen{})
	manager.node = node
	manager.topo = topo
	manager.transcoder = &mockTranscoder{}

	config, err := json.Marshal(RelayCommand{TargetPID: target.String(), HeartbeatInterval: "0s"})
	require.NoError(t, err)
	luaErr := make(chan error, 1)
	handler := manager.CreateMiddleware(map[string]string{OptionAllowedOrigins: "*"})(newLuaRelayHandler(string(config), luaErr))
	joinReady := node.notifyOnSend(JoinTopic, target)
	req, cancel, releaseFrame := newLuaRelayRequest(t, host)
	writer := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(writer, req)
		close(done)
	}()
	defer func() {
		cancel()
		waitMiddlewareDone(t, done)
		releaseFrame()
	}()

	select {
	case err := <-luaErr:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Lua handler did not finish")
	}
	select {
	case <-joinReady:
	case <-time.After(time.Second):
		t.Fatal("managed Lua session did not join its target")
	}
	stream := node.sendSource(JoinTopic, target)
	require.False(t, stream.Equal(pid.Zero()))
	require.NoError(t, host.Send(relay.NewPackage(pid.Zero(), stream, topology.TopicEvents,
		payload.New(&topology.ExitEvent{From: target, Kind: topology.Exit}))))
	waitMiddlewareDone(t, done)

	assert.Equal(t, http.StatusOK, writer.Code)
	assert.Equal(t, "text/event-stream", writer.Header().Get("Content-Type"))
	assert.Empty(t, writer.Header().Get(RelayHeader))
	assert.Contains(t, writer.Body.String(), "event: done")
	assert.Contains(t, writer.Body.String(), `"reason":"target process exited"`)
	assert.Equal(t, 0, node.sendCount(LeaveTopic, target))
	assert.Empty(t, logs.FilterMessage("failed to send leave").All())
	assert.False(t, host.hasStream(stream))
	assert.Equal(t, 1, topo.completeCount(stream))
}

func TestMiddlewareLuaDisconnectMissingTargetWarns(t *testing.T) {
	target := mustPID("{n1@app:llm|lua-target-disconnect}")
	host := newMockHost()
	node := newMockNode()
	topo := newMockTopology()
	core, logs := observer.New(zap.WarnLevel)
	node.setSendError(LeaveTopic, target, process.ErrProcessNotFound)
	manager := NewSSERelay(context.Background(), zap.New(core), &testPIDGen{})
	manager.node = node
	manager.topo = topo
	manager.transcoder = &mockTranscoder{}

	config, err := json.Marshal(RelayCommand{TargetPID: target.String(), HeartbeatInterval: "0s"})
	require.NoError(t, err)
	luaErr := make(chan error, 1)
	handler := manager.CreateMiddleware(map[string]string{OptionAllowedOrigins: "*"})(newLuaRelayHandler(string(config), luaErr))
	joinReady := node.notifyOnSend(JoinTopic, target)
	req, cancel, releaseFrame := newLuaRelayRequest(t, host)
	writer := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(writer, req)
		close(done)
	}()
	defer func() {
		cancel()
		waitMiddlewareDone(t, done)
		releaseFrame()
	}()

	select {
	case err := <-luaErr:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Lua handler did not finish")
	}
	select {
	case <-joinReady:
	case <-time.After(time.Second):
		t.Fatal("managed Lua session did not join its target")
	}
	stream := node.sendSource(JoinTopic, target)
	cancel()
	waitMiddlewareDone(t, done)

	assert.Equal(t, http.StatusOK, writer.Code)
	assert.Equal(t, 1, node.sendCount(LeaveTopic, target))
	assert.Equal(t, 1, logs.FilterMessage("failed to send leave").Len())
	assert.False(t, host.hasStream(stream))
	assert.Equal(t, 1, topo.completeCount(stream))
}

func newLuaRelayHandler(config string, luaErrors chan<- error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		frame := contextapi.FrameFromContext(r.Context())
		if frame == nil {
			luaErrors <- errors.New("frame context missing")
			return
		}
		if err := frame.Set(httpapi.RequestKey(), httpapi.NewRequestContext(r, w)); err != nil {
			luaErrors <- err
			return
		}
		state := lua.NewState()
		defer state.Close()
		module, _ := httpmodule.Module.Build()
		state.SetGlobal("http", module)
		state.SetGlobal("relay_config", lua.LString(config))
		state.SetContext(r.Context())
		luaErrors <- state.DoString(`
			local response, err = http.response()
			assert(err == nil, tostring(err))
			assert(response:set_header("X-SSE-Relay", relay_config) == nil)
		`)
	})
}

func newLuaRelayRequest(t *testing.T, host *mockHost) (*http.Request, context.CancelFunc, func()) {
	t.Helper()
	baseCtx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequestWithContext(baseCtx, http.MethodGet, "http://example.com/sse", nil)
	ctx, frame := contextapi.OpenFrameContext(req.Context())
	require.NoError(t, frame.Set(httpapi.ServerKey(), host))
	require.NoError(t, frame.Set(httpapi.ServerIDKey(), registry.NewID("app", "lua-server")))
	return req.WithContext(ctx), cancel, func() { contextapi.ReleaseFrameContext(frame) }
}

func waitMiddlewareDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("HTTP middleware did not finish")
	}
}
