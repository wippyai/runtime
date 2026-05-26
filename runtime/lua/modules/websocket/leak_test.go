// SPDX-License-Identifier: MPL-2.0

package websocket_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	stdtime "time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	apiruntime "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/internal/uniqid"
	"github.com/wippyai/runtime/runtime/lua/engine"
	luapayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	wsmod "github.com/wippyai/runtime/runtime/lua/modules/websocket"
	wssvc "github.com/wippyai/runtime/service/websocket"
	systempayload "github.com/wippyai/runtime/system/payload"
	sysrelay "github.com/wippyai/runtime/system/relay"
	"github.com/wippyai/runtime/system/scheduler"
	"github.com/wippyai/runtime/system/scheduler/actor"
	"go.uber.org/zap"
)

// wsLeakHostID is the relay host the scheduler registers under so the
// dispatcher read-loop relay packages route back into the running process.
const wsLeakHostID = "ws.leak.test"

type wsTestScheduler struct {
	*actor.Scheduler
	pending    map[string]chan *apiruntime.Result
	startHooks map[string]func()
	stopHooks  map[string]func()
	mu         sync.Mutex
}

func (ts *wsTestScheduler) Stop() { ts.Scheduler.Stop(context.Background()) }

func (ts *wsTestScheduler) setLifecycleHooks(p pid.PID, onStart, onStop func()) {
	ts.mu.Lock()
	ts.startHooks[p.UniqID] = onStart
	ts.stopHooks[p.UniqID] = onStop
	ts.mu.Unlock()
}

func (ts *wsTestScheduler) OnStart(_ context.Context, p pid.PID, _ process.Process) error {
	ts.mu.Lock()
	hook := ts.startHooks[p.UniqID]
	ts.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}

func (ts *wsTestScheduler) OnComplete(_ context.Context, p pid.PID, result *apiruntime.Result) {
	ts.mu.Lock()
	ch, ok := ts.pending[p.UniqID]
	if ok {
		delete(ts.pending, p.UniqID)
	}
	stop := ts.stopHooks[p.UniqID]
	delete(ts.stopHooks, p.UniqID)
	delete(ts.startHooks, p.UniqID)
	ts.mu.Unlock()
	if stop != nil {
		stop()
	}
	if ok {
		ch <- result
	}
}

func (ts *wsTestScheduler) Execute(ctx context.Context, p pid.PID, proc process.Process, method string, input payload.Payloads) (*apiruntime.Result, error) {
	resultCh := make(chan *apiruntime.Result, 1)
	ts.mu.Lock()
	ts.pending[p.UniqID] = resultCh
	ts.mu.Unlock()

	if _, err := ts.Submit(ctx, p, proc, method, input); err != nil {
		ts.mu.Lock()
		delete(ts.pending, p.UniqID)
		ts.mu.Unlock()
		return nil, err
	}

	select {
	case result := <-resultCh:
		return result, nil
	case <-ctx.Done():
		ts.mu.Lock()
		delete(ts.pending, p.UniqID)
		ts.mu.Unlock()
		return nil, ctx.Err()
	}
}

type wsTestContext struct {
	ctx       context.Context
	scheduler *wsTestScheduler
	node      *sysrelay.Node
	dispStop  func()
}

// setupWsTest wires a relay node, the real websocket dispatcher, and an actor
// scheduler so the dispatcher read loop produces real relay packages that
// resolve in Lua. Mirrors the funcs leak harness.
func setupWsTest(t *testing.T, numWorkers int) *wsTestContext {
	t.Helper()
	logger := zap.NewNop()
	node := sysrelay.NewNode("ws-leak-test")

	transcoder := systempayload.NewTranscoder()
	luapayload.Register(transcoder)
	luapayload.RegisterAllBasicFormats(transcoder)

	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	ctx = relay.WithNode(ctx, node)
	ctx = payload.WithTranscoder(ctx, transcoder)

	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "test")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	reg := scheduler.NewRegistry()
	disp := wssvc.NewDispatcher(wssvc.WithWorkers(numWorkers), wssvc.WithLogger(logger))
	require.NoError(t, disp.Start(ctx))
	disp.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		reg.Register(id, h)
	})

	ts := &wsTestScheduler{
		pending:    make(map[string]chan *apiruntime.Result),
		startHooks: make(map[string]func()),
		stopHooks:  make(map[string]func()),
	}
	ts.Scheduler = actor.NewScheduler(reg, actor.WithWorkers(numWorkers), actor.WithLifecycle(ts))
	ts.Start()
	ts.EnableStats()
	require.NoError(t, node.RegisterHost(wsLeakHostID, ts.Scheduler))

	return &wsTestContext{
		ctx:       ctx,
		scheduler: ts,
		node:      node,
		dispStop:  func() { _ = disp.Stop(ctx) },
	}
}

func (tc *wsTestContext) Close() {
	tc.scheduler.Stop()
	tc.dispStop()
}

func (tc *wsTestContext) frameCtxPID(t *testing.T) (context.Context, pid.PID) {
	t.Helper()
	p := pid.PID{Host: wsLeakHostID, UniqID: stdtime.Now().Format("150405.000000000")}
	p = p.Precomputed()
	frameCtx, _ := ctxapi.OpenFrameContext(tc.ctx)
	require.NoError(t, apiruntime.SetFramePID(frameCtx, p))
	return frameCtx, p
}

func bindWsModule(l *lua.LState) error {
	tbl, _ := wsmod.Module.Build()
	l.SetGlobal(wsmod.Module.Name, tbl)
	return nil
}

func newWsProcess(t *testing.T, script string) *engine.Process {
	t.Helper()
	proto, err := lua.CompileString(script, "ws_test.lua")
	require.NoError(t, err)
	proc, err := engine.NewProcess(
		engine.WithProto(proto),
		engine.WithModuleBinder(func(l *lua.LState) error {
			engine.LoadModuleDef(l, engine.ChannelModule)
			return nil
		}),
		engine.WithModuleBinder(bindWsModule),
	)
	require.NoError(t, err)
	return proc
}

// subSampler reads proc.LiveSubscriptionCount on a ticker, bounded by lifecycle
// hooks (begin from OnStart after Init, end from OnComplete before Close).
type subSampler struct {
	proc     *engine.Process
	beginCh  chan struct{}
	stopCh   chan struct{}
	doneCh   chan struct{}
	maxLive  int
	lastLive int
	samples  int
}

func newSubSampler(proc *engine.Process) *subSampler {
	s := &subSampler{
		proc:    proc,
		beginCh: make(chan struct{}),
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
	go func() {
		defer close(s.doneCh)
		select {
		case <-s.beginCh:
		case <-s.stopCh:
			return
		}
		ticker := stdtime.NewTicker(100 * stdtime.Microsecond)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
				live := s.proc.LiveSubscriptionCount()
				s.samples++
				s.lastLive = live
				if live > s.maxLive {
					s.maxLive = live
				}
			}
		}
	}()
	return s
}

func (s *subSampler) begin() {
	select {
	case <-s.beginCh:
	default:
		close(s.beginCh)
	}
}

func (s *subSampler) end() {
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
	<-s.doneCh
}

func (s *subSampler) results() (int, int, int) {
	return s.maxLive, s.lastLive, s.samples
}

// echoServer echoes every received frame back to the client.
func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		for {
			mt, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			if err := conn.Write(r.Context(), mt, data); err != nil {
				return
			}
		}
	}))
}

// silentServer accepts the connection then never reads or writes, so the
// client read loop blocks waiting for frames. Used to exercise drain while a
// connection is open and idle.
func silentServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()
	hold := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		select {
		case <-hold:
		case <-r.Context().Done():
		}
	}))
	return srv, func() { close(hold) }
}

func wsURLOf(srv *httptest.Server) string { return "ws" + srv.URL[4:] }

// resultString extracts the string payload from a result value, tolerating the
// lua.LString wrapper the Lua payload transcoder produces.
func resultString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case lua.LString:
		return string(s)
	case fmt.Stringer:
		return s.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

func resultInt(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case lua.LInteger:
		return int64(n)
	case lua.LNumber:
		return int64(n)
	}
	return -1
}

// goroutinesSettle polls until runtime.NumGoroutine drops to at most baseline
// within budget, returning the final count. Read-loop goroutine exit is
// asynchronous to the step thread, so a bounded settle window avoids flakes.
func goroutinesSettle(baseline int, budget stdtime.Duration) int {
	deadline := stdtime.Now().Add(budget)
	last := runtime.NumGoroutine()
	for stdtime.Now().Before(deadline) {
		last = runtime.NumGoroutine()
		if last <= baseline {
			return last
		}
		stdtime.Sleep(2 * stdtime.Millisecond)
	}
	return last
}

// WS normal connect/use/close: the subscription returns to 0 and the read-loop
// goroutine exits.
func TestLeak_WsConnectUseCloseReclaims(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	tc := setupWsTest(t, 4)
	defer tc.Close()

	runtime.GC()
	baseline := runtime.NumGoroutine()

	script := fmt.Sprintf(`
		local conn, err = websocket.connect(%q)
		if err then return nil, "connect: " .. tostring(err) end
		local ch = conn:channel()
		local ok = conn:send("hello")
		local msg, recv_ok = ch:receive()
		if not recv_ok then return nil, "channel closed before message" end
		if msg == nil or msg.data ~= "hello" then return nil, "wrong echo: " .. tostring(msg and msg.data) end
		conn:close()
		return "ok"
	`, wsURLOf(srv))

	frameCtx, runPID := tc.frameCtxPID(t)
	proc := newWsProcess(t, script)

	sampler := newSubSampler(proc)
	tc.scheduler.setLifecycleHooks(runPID, sampler.begin, sampler.end)
	result, err := tc.scheduler.Execute(frameCtx, runPID, proc, "", nil)
	maxSeen, lastSeen, samples := sampler.results()

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.Error, "script error: %v", result.Error)
	require.NotNil(t, result.Value)
	assert.Equal(t, "ok", resultString(result.Value.Data()))

	t.Logf("ws connect/use/close: %d samples, max live=%d, last=%d", samples, maxSeen, lastSeen)
	require.GreaterOrEqual(t, samples, 1, "sampler never observed the process")
	assert.LessOrEqual(t, maxSeen, 2, "live subscriptions should stay near one for a single connection")
	assert.LessOrEqual(t, lastSeen, 1, "subscription must not accumulate after conn:close()")

	// Read-loop goroutine settling back to baseline proves the producer-stop
	// cleanup fired through closeChannel, i.e. the subscription was reclaimed.
	settled := goroutinesSettle(baseline, 3*stdtime.Second)
	assert.LessOrEqualf(t, settled, baseline+1, "read-loop goroutine leaked: baseline=%d settled=%d", baseline, settled)
}

// WS remote disconnect delivers a terminal that reclaims the subscription and
// stops the read loop without an explicit conn:close().
func TestLeak_WsRemoteDisconnectReclaims(t *testing.T) {
	closeServer := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		// One echo, then close from the server side to drive the terminal.
		mt, data, err := conn.Read(r.Context())
		if err == nil {
			_ = conn.Write(r.Context(), mt, data)
		}
		<-closeServer
		_ = conn.Close(coderws.StatusNormalClosure, "bye")
	}))
	defer srv.Close()

	tc := setupWsTest(t, 4)
	defer tc.Close()

	runtime.GC()
	baseline := runtime.NumGoroutine()

	script := fmt.Sprintf(`
		local conn, err = websocket.connect(%q)
		if err then return nil, "connect: " .. tostring(err) end
		local ch = conn:channel()
		conn:send("ping")
		local msg, ok = ch:receive()
		if not ok then return nil, "closed before echo" end
		if msg == nil or msg.data ~= "ping" then return nil, "wrong echo" end
		-- Block on the channel until the server closes; the read loop relays a
		-- terminal which closes this channel from the engine side.
		local _, still_open = ch:receive()
		if still_open then return nil, "expected channel close on remote disconnect" end
		return "ok"
	`, wsURLOf(srv))

	frameCtx, runPID := tc.frameCtxPID(t)
	proc := newWsProcess(t, script)

	sampler := newSubSampler(proc)
	tc.scheduler.setLifecycleHooks(runPID, sampler.begin, sampler.end)

	go func() {
		stdtime.Sleep(50 * stdtime.Millisecond)
		close(closeServer)
	}()

	result, err := tc.scheduler.Execute(frameCtx, runPID, proc, "", nil)
	maxSeen, lastSeen, samples := sampler.results()

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.Error, "script error: %v", result.Error)
	require.NotNil(t, result.Value)
	assert.Equal(t, "ok", resultString(result.Value.Data()))

	t.Logf("ws remote disconnect: %d samples, max live=%d, last=%d", samples, maxSeen, lastSeen)
	assert.LessOrEqual(t, maxSeen, 2, "single connection: subscriptions must not accumulate")
	assert.LessOrEqual(t, lastSeen, 1, "remote disconnect terminal must reclaim the subscription")

	settled := goroutinesSettle(baseline, 3*stdtime.Second)
	assert.LessOrEqualf(t, settled, baseline+1, "read-loop goroutine leaked after remote disconnect: baseline=%d settled=%d", baseline, settled)
}

// WS slow-consumer overflow drives the exact producer frame shape the
// dispatcher read loop emits (non-frame relay packages on a ws@<n> topic for an
// SubscribeExistingStream subscription) directly through Process.Step with no
// waiting receiver. Forcing the bounded buffer to overflow deterministically
// from a single-threaded Lua actor is racy — a parked receiver prevents
// overflow by definition — so this proves the OverflowClose engine semantics at
// the step level: the channel closes on overflow AND the producer-stop cleanup
// (the wired read-loop cancel) fires, reclaiming the subscription.
//
// Real end-to-end delivery and read-loop cancel are proven by the connect/use/
// close, remote-disconnect, drain, and hundreds tests above.
func TestLeak_WsSlowConsumerOverflowStopsReadLoop(t *testing.T) {
	const topic = "ws@overflow"
	const bufCap = 4
	wsCh := engine.NewChannel(bufCap)

	// stopCalled records that the producer-stop cleanup fired on overflow — the
	// same closure path the dispatcher wires through SetSubscriptionCleanup.
	var stopCalled atomic.Bool

	binder := func(l *lua.LState) error {
		mod := l.CreateTable(0, 1)
		mod.RawSetString("stream", lua.LGoFunc(func(s *lua.LState) int {
			p := engine.GetProcess(s)
			require.NotNil(t, p)
			require.NoError(t, p.SubscribeExistingStream(topic, wsCh))
			p.SetTopicHandler(topic, func(_ context.Context, _ *lua.LState, _ pid.PID, _ string, payloads []payload.Payload) lua.LValue {
				if len(payloads) == 0 {
					return lua.LNil
				}
				return lua.LString(fmt.Sprintf("%v", payloads[0].Data()))
			})
			require.True(t, p.SetSubscriptionCleanup(wsCh, func() { stopCalled.Store(true) }))
			engine.PushChannel(s, wsCh)
			return 1
		}))
		l.SetGlobal("websocket_overflow", mod)
		return nil
	}

	proc, err := engine.NewProcess(
		engine.WithScript(`
			local ws = require("websocket_overflow")
			-- Subscribe to obtain the producer-fed stream channel, then park on a
			-- separate never-ready channel so the actor is NOT a waiting receiver
			-- on the ws channel while frames flood in.
			local ch = ws.stream()
			local idle = channel.new(0)
			idle:receive()
			return "done"
		`, "overflow.lua"),
		engine.WithModuleBinder(func(l *lua.LState) error {
			engine.LoadModuleDef(l, engine.ChannelModule)
			return nil
		}),
		engine.WithModuleBinder(binder),
	)
	require.NoError(t, err)
	defer proc.Close()

	procPID := pid.PID{Host: "h", UniqID: "u"}
	procPID = procPID.Precomputed()

	frameCtx, _ := ctxapi.OpenFrameContext(security.SetStrictMode(ctxapi.NewRootContext(), false))
	require.NoError(t, apiruntime.SetFramePID(frameCtx, procPID))
	require.NoError(t, proc.Init(frameCtx, "", nil))

	// First step: run the script up to the idle:receive() park. This registers
	// the SubscribeExistingStream subscription and the cleanup.
	var out process.StepOutput
	require.NoError(t, proc.Step(nil, &out))
	require.Equal(t, 1, proc.LiveSubscriptionCount(), "stream subscription must be live after subscribe")

	// Flood the producer frame shape past the bounded buffer with no waiting
	// receiver. Each is the dispatcher's non-frame string payload package.
	for i := 0; i < bufCap*4; i++ {
		pkg := relay.NewPackage(pid.Zero(), procPID, topic, payload.NewPayload([]byte("x"), payload.String))
		out.Reset()
		require.NoError(t, proc.Step([]process.Event{{Type: process.EventMessage, Data: pkg}}, &out))
	}

	assert.True(t, wsCh.IsClosed(), "bounded buffer overflow must close the ordered stream channel")
	assert.True(t, stopCalled.Load(), "overflow must fire the producer-stop cleanup (read-loop cancel)")
	assert.Equal(t, 0, proc.LiveSubscriptionCount(), "overflow must reclaim the subscription")
}

// WS process-exit drain: the actor opens a connection on a silent server and
// returns without closing it. drainSubscriptionChannels in Close must fire the
// producer-stop cleanup so the read loop exits.
func TestLeak_WsProcessExitDrainStopsReadLoop(t *testing.T) {
	srv, release := silentServer(t)
	defer srv.Close()
	defer release()

	tc := setupWsTest(t, 4)
	defer tc.Close()

	runtime.GC()
	baseline := runtime.NumGoroutine()

	script := fmt.Sprintf(`
		local conn, err = websocket.connect(%q)
		if err then return nil, "connect: " .. tostring(err) end
		local ch = conn:channel()
		conn:send("hi")
		-- Exit while the subscription is live and the read loop is blocked on
		-- the silent server. No close, no terminal: drain must reclaim.
		return "exited-open"
	`, wsURLOf(srv))

	frameCtx, runPID := tc.frameCtxPID(t)
	proc := newWsProcess(t, script)

	result, err := tc.scheduler.Execute(frameCtx, runPID, proc, "", nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.Error, "script error: %v", result.Error)
	require.NotNil(t, result.Value)
	assert.Equal(t, "exited-open", resultString(result.Value.Data()))

	settled := goroutinesSettle(baseline, 3*stdtime.Second)
	t.Logf("ws process-exit drain: baseline=%d settled=%d", baseline, settled)
	assert.LessOrEqualf(t, settled, baseline+1, "read-loop goroutine leaked after process drain: baseline=%d settled=%d", baseline, settled)
}

// No-accumulation: one long-lived actor opens, uses, and closes hundreds of
// connections in a loop. Max live subscriptions stays ~1 and converges to 0.
func TestLeak_WsHundredsOfConnectionsNoAccumulation(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	tc := setupWsTest(t, 4)
	defer tc.Close()

	runtime.GC()
	baseline := runtime.NumGoroutine()

	const iterations = 300
	script := fmt.Sprintf(`
		local n = %d
		for i = 1, n do
			local conn, err = websocket.connect(%q)
			if err then return nil, "connect at " .. i .. ": " .. tostring(err) end
			local ch = conn:channel()
			conn:send("m" .. i)
			local msg, ok = ch:receive()
			if not ok then return nil, "channel closed at " .. i end
			if msg == nil or msg.data ~= ("m" .. i) then
				return nil, "wrong echo at " .. i .. ": " .. tostring(msg and msg.data)
			end
			conn:close()
		end
		return n
	`, iterations, wsURLOf(srv))

	frameCtx, runPID := tc.frameCtxPID(t)
	proc := newWsProcess(t, script)

	sampler := newSubSampler(proc)
	tc.scheduler.setLifecycleHooks(runPID, sampler.begin, sampler.end)
	result, err := tc.scheduler.Execute(frameCtx, runPID, proc, "", nil)
	maxSeen, lastSeen, samples := sampler.results()

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.Error, "script error: %v", result.Error)
	require.NotNil(t, result.Value)
	assert.EqualValues(t, iterations, resultInt(result.Value.Data()), "all echoes must be real, not nil/error")

	t.Logf("ws %d connections: %d samples, max live=%d, last=%d", iterations, samples, maxSeen, lastSeen)
	require.GreaterOrEqual(t, samples, 1, "sampler never observed the process")
	assert.LessOrEqualf(t, maxSeen, 4, "live subscriptions climbed to %d over %d connections (leak: should stay ~1)", maxSeen, iterations)
	assert.LessOrEqual(t, lastSeen, 1, "subscriptions must converge near 0")

	settled := goroutinesSettle(baseline, 5*stdtime.Second)
	assert.LessOrEqualf(t, settled, baseline+2, "read-loop goroutines accumulated: baseline=%d settled=%d", baseline, settled)
}
