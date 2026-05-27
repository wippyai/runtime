// SPDX-License-Identifier: MPL-2.0

package process_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	stdtime "time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	procapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/internal/uniqid"
	"github.com/wippyai/runtime/runtime/lua/engine"
	luapayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
	systempayload "github.com/wippyai/runtime/system/payload"
	sysprocess "github.com/wippyai/runtime/system/process"
	sysrelay "github.com/wippyai/runtime/system/relay"
	"github.com/wippyai/runtime/system/scheduler"
	"github.com/wippyai/runtime/system/scheduler/actor"
	"go.uber.org/zap"
)

// processLeakHostID is the relay host the scheduler registers under so a
// process.send addressed to the running actor's own PID routes back into it as
// a topic message that a process.listen subscription receives.
const processLeakHostID = "process.leak.test"

type processTestScheduler struct {
	*actor.Scheduler
	pending    map[string]chan *runtime.Result
	startHooks map[string]func()
	stopHooks  map[string]func()
	mu         sync.Mutex
}

func (ts *processTestScheduler) Stop() { ts.Scheduler.Stop(context.Background()) }

func (ts *processTestScheduler) setLifecycleHooks(p pid.PID, onStart, onStop func()) {
	ts.mu.Lock()
	ts.startHooks[p.UniqID] = onStart
	ts.stopHooks[p.UniqID] = onStop
	ts.mu.Unlock()
}

func (ts *processTestScheduler) OnStart(_ context.Context, p pid.PID, _ procapi.Process) error {
	ts.mu.Lock()
	hook := ts.startHooks[p.UniqID]
	ts.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}

func (ts *processTestScheduler) OnComplete(_ context.Context, p pid.PID, result *runtime.Result) {
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

func (ts *processTestScheduler) Execute(ctx context.Context, p pid.PID, proc procapi.Process, method string, input payload.Payloads) (*runtime.Result, error) {
	resultCh := make(chan *runtime.Result, 1)
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

type processTestContext struct {
	ctx       context.Context
	scheduler *processTestScheduler
	node      *sysrelay.Node
	stopped   bool
}

// stopScheduler stops the worker pool, joining all worker goroutines via
// wg.Wait(). This establishes a happens-before edge after Process.Close so a
// subsequent read of process state from the test goroutine is race-free.
// Idempotent.
func (tc *processTestContext) stopScheduler() {
	if tc.stopped {
		return
	}
	tc.stopped = true
	tc.scheduler.Stop()
}

func setupProcessLeakTest(t *testing.T, numWorkers int) *processTestContext {
	t.Helper()
	logger := zap.NewNop()
	node := sysrelay.NewNode("process-leak-test")

	transcoder := systempayload.NewTranscoder()
	luapayload.Register(transcoder)
	luapayload.RegisterAllBasicFormats(transcoder)

	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	ctx = relay.WithNode(ctx, node)
	ctx = payload.WithTranscoder(ctx, transcoder)

	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "test")
	ctx = procapi.WithPIDGenerator(ctx, pidGen)

	reg := scheduler.NewRegistry()
	// The process Send handler only routes through the relay node; manager and
	// topology are unused for self-addressed topic sends.
	procDisp := sysprocess.NewDispatcher(nil, node, nil, logger)
	procDisp.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		reg.Register(id, h)
	})

	ts := &processTestScheduler{
		pending:    make(map[string]chan *runtime.Result),
		startHooks: make(map[string]func()),
		stopHooks:  make(map[string]func()),
	}
	ts.Scheduler = actor.NewScheduler(reg, actor.WithWorkers(numWorkers), actor.WithLifecycle(ts))
	ts.Start()
	ts.EnableStats()
	require.NoError(t, node.RegisterHost(processLeakHostID, ts.Scheduler))

	return &processTestContext{
		ctx:       ctx,
		scheduler: ts,
		node:      node,
	}
}

func (tc *processTestContext) Close(t *testing.T) {
	tc.stopScheduler()
}

func (tc *processTestContext) frameCtxPID(t *testing.T) (context.Context, pid.PID) {
	t.Helper()
	p := pid.PID{Host: processLeakHostID, UniqID: stdtime.Now().Format("150405.000000000")}
	p = p.Precomputed()
	frameCtx, _ := ctxapi.OpenFrameContext(tc.ctx)
	require.NoError(t, runtime.SetFramePID(frameCtx, p))
	return frameCtx, p
}

func newProcessLeakProcess(t *testing.T, script string) *engine.Process {
	t.Helper()
	proto, err := lua.CompileString(script, "test.lua")
	require.NoError(t, err)
	proc, err := engine.NewProcess(
		engine.WithProto(proto),
		engine.WithModuleBinder(func(l *lua.LState) error {
			engine.LoadModuleDef(l, engine.ChannelModule)
			return nil
		}),
		engine.WithModuleBinder(func(l *lua.LState) error {
			mod, _ := processmod.Module.Build()
			l.SetGlobal("process", mod)
			return nil
		}),
	)
	require.NoError(t, err)
	return proc
}

// processSubSampler reads proc.LiveSubscriptionCount() on a ticker, bounded to
// the process's live phase by lifecycle hooks: begin() fires from OnStart
// (after Process.Init allocated the subs map) and end() fires from OnComplete
// before Process.Close. LiveSubscriptionCount takes the subs RLock that the
// step thread also holds for map mutations, so reads inside this window never
// race.
type processSubSampler struct {
	proc     *engine.Process
	beginCh  chan struct{}
	stopCh   chan struct{}
	doneCh   chan struct{}
	maxLive  int
	lastLive int
	samples  int
}

func newProcessSubSampler(proc *engine.Process) *processSubSampler {
	s := &processSubSampler{
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

func (s *processSubSampler) begin() {
	select {
	case <-s.beginCh:
	default:
		close(s.beginCh)
	}
}

func (s *processSubSampler) end() {
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
	<-s.doneCh
}

func (s *processSubSampler) results() (int, int, int) {
	return s.maxLive, s.lastLive, s.samples
}

func extractProcessInt64(v any) int64 {
	switch val := v.(type) {
	case int64:
		return val
	case int:
		return int64(val)
	case lua.LInteger:
		return int64(val)
	case lua.LNumber:
		return int64(val)
	}
	return 0
}

// liveTopicCounts returns the byTopic / byChannel / handler map sizes. Read
// after the worker pool is stopped so Process.Close has run on a joined
// goroutine, establishing a happens-before edge with this read.
func liveTopicCounts(proc *engine.Process) (int, int, int) {
	return proc.LiveSubscriptionCount(), proc.LiveChannelSubscriptionCount(), proc.TopicHandlerCount()
}

// Lane B: a single long-lived actor calls process.listen(topic) HUNDREDS of
// times. Each cycle drives a real producer -- process.send(self, topic, value)
// routes through the process Send dispatcher -> relay node -> back into this
// same actor as a topic message the listen subscription channel receives. The
// actor asserts the received value is the genuine published payload (not a
// vacuous nil), then process.unlisten(ch) removes byTopic + byChannel + handler
// and closes the channel. Live subscriptions stay near one and converge to
// zero; after the loop the subscription maps and handler map are all empty.
func TestLeak_ProcessListenUnlistenLoopNoAccumulation(t *testing.T) {
	tc := setupProcessLeakTest(t, 4)
	defer tc.Close(t)

	const iterations = 300

	script := fmt.Sprintf(`
		local self = process.pid()
		local n = %d
		local received = 0
		for i = 1, n do
			local topic = "leak.topic." .. i
			local ch, err = process.listen(topic)
			if err then return nil, "listen failed at " .. i .. ": " .. tostring(err) end

			local ok, serr = process.send(self, topic, {seq = i})
			if not ok then return nil, "send failed at " .. i .. ": " .. tostring(serr) end

			local msg, rok = ch:receive()
			if not rok then return nil, "channel closed at " .. i end
			if msg == nil then return nil, "nil message at " .. i end
			if msg.seq ~= i then return nil, "wrong payload at " .. i .. ": " .. tostring(msg.seq) end
			received = received + 1

			local uok, uerr = process.unlisten(ch)
			if not uok then return nil, "unlisten failed at " .. i .. ": " .. tostring(uerr) end
		end
		return received
	`, iterations)

	frameCtx, runPID := tc.frameCtxPID(t)
	proc := newProcessLeakProcess(t, script)

	sampler := newProcessSubSampler(proc)
	tc.scheduler.setLifecycleHooks(runPID, sampler.begin, sampler.end)

	ctx, cancel := context.WithTimeout(frameCtx, 60*stdtime.Second)
	defer cancel()
	result, err := tc.scheduler.Execute(ctx, runPID, proc, "", nil)
	maxSeen, lastSeen, sampleCount := sampler.results()

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.Error, "script error: %v", result.Error)
	require.NotNil(t, result.Value)
	assert.Equal(t, int64(iterations), extractProcessInt64(result.Value.Data()), "every cycle must receive a real message")

	t.Logf("process listen/unlisten loop: %d iterations, %d samples, max live subscriptions=%d, last=%d", iterations, sampleCount, maxSeen, lastSeen)

	require.GreaterOrEqual(t, sampleCount, 1, "sampler never observed the process")
	assert.LessOrEqualf(t, maxSeen, 8, "live subscriptions climbed to %d over %d listen/unlisten cycles (leak: should stay ~1)", maxSeen, iterations)

	// Stop the worker pool to join the goroutine that ran Process.Close before
	// reading map sizes. After hundreds of listen/unlisten cycles every topic,
	// channel, and handler entry must be gone -- no orphan handler at scale.
	tc.stopScheduler()
	subs, chans, handlers := liveTopicCounts(proc)
	assert.Equal(t, 0, subs, "subs.byTopic not empty after listen/unlisten loop")
	assert.Equal(t, 0, chans, "subs.byChannel not empty after listen/unlisten loop")
	assert.Equal(t, 0, handlers, "proc.handlers not empty after listen/unlisten loop (orphan handler leak)")
}

// Lane B "dropped = reclaimed at process exit": a single long-lived actor calls
// process.listen on N distinct topics with the message-mode handler installed
// and NEVER unlistens -- each channel is held in a Lua table so it cannot be
// reclaimed early. Every iteration sends a message to itself on that topic and
// receives it (real producer delivery, asserted non-nil), so each held
// subscription is proven live. After building all N, the actor subscribes to a
// gate topic and parks on it: while parked the live count is read determinist
// -ically from the test goroutine (LiveSubscriptionCount takes the subs RLock)
// and must equal N+1, proving the held subscriptions and handlers really
// accumulated and were not vacuously reclaimed. The test then releases the
// gate; after the process completes drainSubscriptionChannels (run on Close)
// must reclaim every held subscription and handler -- live, channel, and
// handler counts all converge to zero.
func TestLeak_ProcessListenWithoutUnlistenReclaimedOnDrain(t *testing.T) {
	tc := setupProcessLeakTest(t, 4)
	defer tc.Close(t)

	const iterations = 150

	script := fmt.Sprintf(`
		local self = process.pid()
		local n = %d
		local held = {}
		local received = 0
		for i = 1, n do
			local topic = "hold.topic." .. i
			-- message=true installs a Go-side topic handler, so each held
			-- subscription owns both a byTopic/byChannel entry and a handler.
			local ch, err = process.listen(topic, {message = true})
			if err then return nil, "listen failed at " .. i .. ": " .. tostring(err) end
			held[i] = ch -- retain so it is never unlistened or GC'd

			local ok, serr = process.send(self, topic, {seq = i})
			if not ok then return nil, "send failed at " .. i .. ": " .. tostring(serr) end

			local msg, rok = ch:receive()
			if not rok then return nil, "channel closed at " .. i end
			if msg == nil then return nil, "nil message at " .. i end
			if msg:payload():data().seq ~= i then return nil, "wrong payload at " .. i end
			received = received + 1
		end

		-- Park on a gate topic with all n held subscriptions still live. The
		-- test reads the live count here, then releases us by sending "gate".
		local gate, gerr = process.listen("gate")
		if gerr then return nil, "gate listen failed: " .. tostring(gerr) end
		local g, gok = gate:receive()
		if not gok then return nil, "gate channel closed" end
		return received
	`, iterations)

	frameCtx, runPID := tc.frameCtxPID(t)
	proc := newProcessLeakProcess(t, script)

	// started fires from OnStart, after Process.Init has assigned the subs map.
	// Reading LiveSubscriptionCount before this point would race the Init write
	// to p.subs; gating on it mirrors the sampler's begin() hook.
	started := make(chan struct{})
	tc.scheduler.setLifecycleHooks(runPID, func() { close(started) }, func() {})

	ctx, cancel := context.WithTimeout(frameCtx, 60*stdtime.Second)
	defer cancel()

	resultCh := make(chan struct {
		res *runtime.Result
		err error
	}, 1)
	go func() {
		res, err := tc.scheduler.Execute(ctx, runPID, proc, "", nil)
		resultCh <- struct {
			res *runtime.Result
			err error
		}{res, err}
	}()

	<-started

	// While the actor is parked on the gate, all n held subscriptions plus the
	// gate subscription are live. Poll until the count settles at n+1; the read
	// is race-free under the subs RLock. This is the non-vacuous proof: the
	// held subscriptions and their handlers really accumulated.
	const want = iterations + 1
	deadline := stdtime.Now().Add(30 * stdtime.Second)
	var peak int
	for stdtime.Now().Before(deadline) {
		live := proc.LiveSubscriptionCount()
		if live > peak {
			peak = live
		}
		if live == want {
			break
		}
		stdtime.Sleep(stdtime.Millisecond)
	}
	t.Logf("process held-listen: %d held subscriptions + gate, peak live observed=%d", iterations, peak)
	require.Equal(t, want, peak, "held subscriptions did not accumulate to n+1 (vacuous harness or reclaim-too-early)")

	// Release the gate so the actor completes.
	require.NoError(t, tc.node.Send(relay.NewMessagePackage(runPID, runPID, &relay.Message{
		Topic:    "gate",
		Payloads: payload.Payloads{payload.New("go")},
	})))

	out := <-resultCh
	require.NoError(t, out.err)
	require.NotNil(t, out.res)
	require.Nil(t, out.res.Error, "script error: %v", out.res.Error)
	require.NotNil(t, out.res.Value)
	assert.Equal(t, int64(iterations), extractProcessInt64(out.res.Value.Data()), "every cycle must receive a real message")

	// Stop the worker pool to join the goroutine that ran Process.Close,
	// establishing a happens-before edge before reading process state.
	// drainSubscriptionChannels (Close path) must have reclaimed every held
	// subscription and handler: nothing survives process drain.
	tc.stopScheduler()
	subs, chans, handlers := liveTopicCounts(proc)
	assert.Equal(t, 0, subs, "subs.byTopic survived process drain")
	assert.Equal(t, 0, chans, "subs.byChannel survived process drain")
	assert.Equal(t, 0, handlers, "proc.handlers survived process drain (handler leak)")
}
