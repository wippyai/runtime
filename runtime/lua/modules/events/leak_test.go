// SPDX-License-Identifier: MPL-2.0

package events_test

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
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/internal/uniqid"
	"github.com/wippyai/runtime/runtime/lua/engine"
	luapayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	eventsmod "github.com/wippyai/runtime/runtime/lua/modules/events"
	"github.com/wippyai/runtime/system/eventbus"
	systempayload "github.com/wippyai/runtime/system/payload"
	sysrelay "github.com/wippyai/runtime/system/relay"
	"github.com/wippyai/runtime/system/scheduler"
	"github.com/wippyai/runtime/system/scheduler/actor"
)

// eventsLeakHostID is the relay host the scheduler registers under so the
// event bus dispatcher's routed package reaches the running process.
const eventsLeakHostID = "events.leak.test"

type eventsTestScheduler struct {
	*actor.Scheduler
	pending    map[string]chan *runtime.Result
	startHooks map[string]func()
	stopHooks  map[string]func()
	mu         sync.Mutex
}

func (ts *eventsTestScheduler) Stop() { ts.Scheduler.Stop(context.Background()) }

func (ts *eventsTestScheduler) setLifecycleHooks(p pid.PID, onStart, onStop func()) {
	ts.mu.Lock()
	ts.startHooks[p.UniqID] = onStart
	ts.stopHooks[p.UniqID] = onStop
	ts.mu.Unlock()
}

func (ts *eventsTestScheduler) OnStart(_ context.Context, p pid.PID, _ process.Process) error {
	ts.mu.Lock()
	hook := ts.startHooks[p.UniqID]
	ts.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}

func (ts *eventsTestScheduler) OnComplete(_ context.Context, p pid.PID, result *runtime.Result) {
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

func (ts *eventsTestScheduler) Execute(ctx context.Context, p pid.PID, proc process.Process, method string, input payload.Payloads) (*runtime.Result, error) {
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

type eventsTestContext struct {
	ctx       context.Context
	bus       *eventbus.Bus
	eventsSvc *eventbus.Dispatcher
	scheduler *eventsTestScheduler
	node      *sysrelay.Node
	stopped   bool
}

// stopScheduler stops the worker pool, joining all worker goroutines via
// wg.Wait(). This establishes a happens-before edge after Process.Close so a
// subsequent read of process state from the test goroutine is race-free.
// Idempotent.
func (tc *eventsTestContext) stopScheduler() {
	if tc.stopped {
		return
	}
	tc.stopped = true
	tc.scheduler.Stop()
}

func setupEventsLeakTest(t *testing.T, numWorkers int) *eventsTestContext {
	t.Helper()
	bus := eventbus.NewBus()
	node := sysrelay.NewNode("events-leak-test")

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
	eventsSvc := eventbus.NewDispatcher(bus, node)
	require.NoError(t, eventsSvc.Start(ctx))
	eventsSvc.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		reg.Register(id, h)
	})

	ts := &eventsTestScheduler{
		pending:    make(map[string]chan *runtime.Result),
		startHooks: make(map[string]func()),
		stopHooks:  make(map[string]func()),
	}
	ts.Scheduler = actor.NewScheduler(reg, actor.WithWorkers(numWorkers), actor.WithLifecycle(ts))
	ts.Start()
	ts.EnableStats()
	require.NoError(t, node.RegisterHost(eventsLeakHostID, ts.Scheduler))

	return &eventsTestContext{
		ctx:       ctx,
		bus:       bus,
		eventsSvc: eventsSvc,
		scheduler: ts,
		node:      node,
	}
}

func (tc *eventsTestContext) Close(t *testing.T) {
	tc.stopScheduler()
	require.NoError(t, tc.eventsSvc.Stop(context.Background()))
	tc.bus.Stop()
}

func (tc *eventsTestContext) frameCtxPID(t *testing.T) (context.Context, pid.PID) {
	t.Helper()
	p := pid.PID{Host: eventsLeakHostID, UniqID: stdtime.Now().Format("150405.000000000")}
	p = p.Precomputed()
	frameCtx, _ := ctxapi.OpenFrameContext(tc.ctx)
	require.NoError(t, runtime.SetFramePID(frameCtx, p))
	return frameCtx, p
}

func newEventsProcess(t *testing.T, script string) *engine.Process {
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
			mod, _ := eventsmod.Module.Build()
			l.SetGlobal("events", mod)
			return nil
		}),
	)
	require.NoError(t, err)
	return proc
}

// eventsSubSampler reads proc.LiveSubscriptionCount() on a ticker, bounded to
// the process's live phase by lifecycle hooks: begin() fires from OnStart
// (after Process.Init allocated the subs map) and end() fires from OnComplete
// before Process.Close. LiveSubscriptionCount takes the subs RLock that the
// step thread also holds for map mutations, so reads inside this window never
// race.
type eventsSubSampler struct {
	proc     *engine.Process
	beginCh  chan struct{}
	stopCh   chan struct{}
	doneCh   chan struct{}
	maxLive  int
	lastLive int
	samples  int
}

func newEventsSubSampler(proc *engine.Process) *eventsSubSampler {
	s := &eventsSubSampler{
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

func (s *eventsSubSampler) begin() {
	select {
	case <-s.beginCh:
	default:
		close(s.beginCh)
	}
}

func (s *eventsSubSampler) end() {
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
	<-s.doneCh
}

func (s *eventsSubSampler) results() (int, int, int) {
	return s.maxLive, s.lastLive, s.samples
}

func extractEventsInt64(v any) int64 {
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

// A single long-lived actor subscribes to the event bus, sends one event to
// itself, receives that real event off the subscription channel, then closes
// the subscription -- HUNDREDS of times. Each cycle drives a real producer:
// events.send routes through the event bus -> dispatcher -> relay back to this
// process channel, so the received value is the genuine published event (not a
// vacuous nil). sub:close() runs UnsubscribeChannel, removing byTopic +
// byChannel + handler and closing the channel, so live subscriptions stay near
// one and converge to zero. The script also asserts each received event is the
// real published value, and the test asserts the final result is real with
// result.Error nil.
func TestLeak_EventsSubscribeReceiveCloseLoop(t *testing.T) {
	tc := setupEventsLeakTest(t, 4)
	defer tc.Close(t)

	const iterations = 300

	script := fmt.Sprintf(`
		local n = %d
		local received = 0
		for i = 1, n do
			local sub, err = events.subscribe("leak.system")
			if err then return nil, "subscribe failed at " .. i .. ": " .. tostring(err) end
			local ch = sub:channel()

			local ok, serr = events.send("leak.system", "leak.kind", "/leak/" .. i, {seq = i})
			if not ok then return nil, "send failed at " .. i .. ": " .. tostring(serr) end

			local evt, rok = ch:receive()
			if not rok then return nil, "channel closed at " .. i end
			if evt == nil then return nil, "nil event at " .. i end
			if evt.system ~= "leak.system" then return nil, "wrong system at " .. i .. ": " .. tostring(evt.system) end
			if evt.kind ~= "leak.kind" then return nil, "wrong kind at " .. i .. ": " .. tostring(evt.kind) end
			if evt.data == nil or evt.data.seq ~= i then return nil, "wrong data at " .. i end
			received = received + 1

			sub:close()
		end
		return received
	`, iterations)

	frameCtx, runPID := tc.frameCtxPID(t)
	proc := newEventsProcess(t, script)

	sampler := newEventsSubSampler(proc)
	tc.scheduler.setLifecycleHooks(runPID, sampler.begin, sampler.end)

	ctx, cancel := context.WithTimeout(frameCtx, 60*stdtime.Second)
	defer cancel()
	result, err := tc.scheduler.Execute(ctx, runPID, proc, "", nil)
	maxSeen, lastSeen, sampleCount := sampler.results()

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.Error, "script error: %v", result.Error)
	require.NotNil(t, result.Value)
	assert.Equal(t, int64(iterations), extractEventsInt64(result.Value.Data()), "every cycle must receive a real event")

	t.Logf("events subscribe/receive/close loop: %d iterations, %d samples, max live subscriptions=%d, last=%d", iterations, sampleCount, maxSeen, lastSeen)

	require.GreaterOrEqual(t, sampleCount, 1, "sampler never observed the process")
	assert.LessOrEqualf(t, maxSeen, 8, "live subscriptions climbed to %d over %d subscribe/close cycles (leak: should stay ~1)", maxSeen, iterations)
}

// A single long-lived actor subscribes hundreds of times and never closes a
// single subscription -- each is held in a Lua table so it cannot be GC'd or
// reclaimed early. Every iteration sends one event to itself and receives it
// off the current subscription channel (real producer delivery, asserted
// non-nil), which forces a real step boundary so the concurrent sampler
// observes the live count climbing as held subscriptions accumulate. Because
// each events.subscribe uses a unique topic, the maps grow to the full hold
// count -- this is the upper bound the foundation must reclaim at teardown.
// After the process completes, drainSubscriptionChannels (run on Close) must
// have converged the live count to zero: no held subscription survives drain.
func TestLeak_EventsHeldSubscriptionsReclaimedOnDrain(t *testing.T) {
	tc := setupEventsLeakTest(t, 4)
	defer tc.Close(t)

	// Each held subscription matches the same system, so the bus fan-out grows
	// O(n) per send. A few hundred holds is enough to climb far above the
	// sequential baseline while keeping the test fast.
	const iterations = 150

	script := fmt.Sprintf(`
		local n = %d
		local held = {}
		local received = 0
		for i = 1, n do
			local sub, err = events.subscribe("hold.system")
			if err then return nil, "subscribe failed at " .. i .. ": " .. tostring(err) end
			held[i] = sub -- retain so it is never closed or GC'd

			local ch = sub:channel()
			local ok, serr = events.send("hold.system", "hold.kind", "/hold/" .. i, {seq = i})
			if not ok then return nil, "send failed at " .. i .. ": " .. tostring(serr) end

			-- Receive the event this iteration's subscription will see. Every
			-- live held subscription matches "hold.system", so each gets a
			-- copy; draining the current channel forces a real step boundary.
			local evt, rok = ch:receive()
			if not rok then return nil, "channel closed at " .. i end
			if evt == nil or evt.data == nil or evt.data.seq ~= i then
				return nil, "wrong event at " .. i
			end
			received = received + 1
		end
		return received
	`, iterations)

	frameCtx, runPID := tc.frameCtxPID(t)
	proc := newEventsProcess(t, script)

	sampler := newEventsSubSampler(proc)
	tc.scheduler.setLifecycleHooks(runPID, sampler.begin, sampler.end)

	ctx, cancel := context.WithTimeout(frameCtx, 60*stdtime.Second)
	defer cancel()
	result, err := tc.scheduler.Execute(ctx, runPID, proc, "", nil)
	maxSeen, lastSeen, sampleCount := sampler.results()

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.Error, "script error: %v", result.Error)
	require.NotNil(t, result.Value)
	assert.Equal(t, int64(iterations), extractEventsInt64(result.Value.Data()), "every cycle must receive a real event")

	t.Logf("events held-subscription loop: %d iterations, %d samples, max live subscriptions=%d, last=%d", iterations, sampleCount, maxSeen, lastSeen)

	require.GreaterOrEqual(t, sampleCount, 1, "sampler never observed the process")
	// Held subscriptions accumulate while the process runs (unique topic each,
	// never closed), so the sampler must witness the count climbing well above
	// the sequential baseline -- proof the subscriptions are real and tracked,
	// not vacuously reclaimed.
	assert.Greater(t, maxSeen, 8, "held subscriptions did not accumulate; harness may be vacuous")

	// Stop the worker pool to join the goroutine that ran Process.Close,
	// establishing a happens-before edge before reading process state.
	// drainSubscriptionChannels (Close path) must have reclaimed every held
	// subscription: no held subscription survives process drain.
	tc.stopScheduler()
	assert.Equal(t, 0, proc.LiveSubscriptionCount(), "subscriptions survived process drain")
}
