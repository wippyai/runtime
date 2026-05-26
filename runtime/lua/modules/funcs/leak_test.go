// SPDX-License-Identifier: MPL-2.0

package funcs_test

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
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/internal/uniqid"
	"github.com/wippyai/runtime/runtime/lua/engine"
	luapayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	funcsmod "github.com/wippyai/runtime/runtime/lua/modules/funcs"
	"github.com/wippyai/runtime/system/eventbus"
	sysfunction "github.com/wippyai/runtime/system/function"
	systempayload "github.com/wippyai/runtime/system/payload"
	sysrelay "github.com/wippyai/runtime/system/relay"
	"github.com/wippyai/runtime/system/scheduler"
	"github.com/wippyai/runtime/system/scheduler/actor"
	"go.uber.org/zap"
)

// funcsLeakHostID is the relay host the scheduler registers under so the async
// function dispatcher's result package routes back into the running process.
const funcsLeakHostID = "funcs.leak.test"

type funcsTestScheduler struct {
	*actor.Scheduler
	pending    map[string]chan *runtime.Result
	startHooks map[string]func()
	stopHooks  map[string]func()
	mu         sync.Mutex
}

func (ts *funcsTestScheduler) Stop() { ts.Scheduler.Stop(context.Background()) }

// setLifecycleHooks registers per-PID callbacks fired on OnStart (after
// Process.Init) and at the start of OnComplete (before the result is delivered
// and the worker tears the process down).
func (ts *funcsTestScheduler) setLifecycleHooks(p pid.PID, onStart, onStop func()) {
	ts.mu.Lock()
	if ts.startHooks == nil {
		ts.startHooks = make(map[string]func())
	}
	if ts.stopHooks == nil {
		ts.stopHooks = make(map[string]func())
	}
	ts.startHooks[p.UniqID] = onStart
	ts.stopHooks[p.UniqID] = onStop
	ts.mu.Unlock()
}

func (ts *funcsTestScheduler) OnStart(_ context.Context, p pid.PID, _ process.Process) error {
	ts.mu.Lock()
	hook := ts.startHooks[p.UniqID]
	ts.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}

func (ts *funcsTestScheduler) OnComplete(_ context.Context, p pid.PID, result *runtime.Result) {
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

func (ts *funcsTestScheduler) Execute(ctx context.Context, p pid.PID, proc process.Process, method string, input payload.Payloads) (*runtime.Result, error) {
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

type funcsTestContext struct {
	ctx              context.Context
	bus              event.Bus
	functionRegistry *sysfunction.Registry
	scheduler        *funcsTestScheduler
	node             *sysrelay.Node
}

func setupFuncsTest(t *testing.T, numWorkers int) *funcsTestContext {
	t.Helper()
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	node := sysrelay.NewNode("funcs-leak-test")

	functionRegistry := sysfunction.NewFunctionRegistry(bus, logger)

	transcoder := systempayload.NewTranscoder()
	luapayload.Register(transcoder)
	luapayload.RegisterAllBasicFormats(transcoder)

	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	ctx = relay.WithNode(ctx, node)
	ctx = function.WithRegistry(ctx, functionRegistry)
	ctx = payload.WithTranscoder(ctx, transcoder)

	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "test")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	require.NoError(t, functionRegistry.Start(ctx))

	reg := scheduler.NewRegistry()
	funcDisp := sysfunction.NewDispatcher(node, logger)
	funcDisp.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		reg.Register(id, h)
	})

	ts := &funcsTestScheduler{
		pending:    make(map[string]chan *runtime.Result),
		startHooks: make(map[string]func()),
		stopHooks:  make(map[string]func()),
	}
	ts.Scheduler = actor.NewScheduler(reg, actor.WithWorkers(numWorkers), actor.WithLifecycle(ts))
	ts.Start()
	ts.EnableStats()
	require.NoError(t, node.RegisterHost(funcsLeakHostID, ts.Scheduler))

	return &funcsTestContext{
		ctx:              ctx,
		bus:              bus,
		functionRegistry: functionRegistry,
		scheduler:        ts,
		node:             node,
	}
}

func (tc *funcsTestContext) Close(t *testing.T) {
	tc.scheduler.Stop()
	require.NoError(t, tc.functionRegistry.Stop())
}

func (tc *funcsTestContext) registerFunction(t *testing.T, funcID registry.ID, handler function.Func) {
	t.Helper()
	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(tc.ctx, tc.bus, function.System, "function.*", func(evt event.Event) {
		if evt.Kind == function.FunctionAccept && evt.Path == funcID.String() {
			wg.Done()
		}
	})
	require.NoError(t, err)
	defer sub.Close()

	wg.Add(1)
	tc.bus.Send(tc.ctx, event.Event{
		System: function.System,
		Kind:   function.FunctionRegister,
		Path:   funcID.String(),
		Data:   &function.FuncEntry{Handler: handler},
	})
	wg.Wait()
}

func (tc *funcsTestContext) frameCtxPID(t *testing.T) (context.Context, pid.PID) {
	t.Helper()
	p := pid.PID{Host: funcsLeakHostID, UniqID: stdtime.Now().Format("150405.000000000")}
	p = p.Precomputed()
	frameCtx, _ := ctxapi.OpenFrameContext(tc.ctx)
	require.NoError(t, runtime.SetFramePID(frameCtx, p))
	return frameCtx, p
}

func bindFuncsModule(l *lua.LState) error {
	tbl, _ := funcsmod.Module.Build()
	l.SetGlobal(funcsmod.Module.Name, tbl)
	return nil
}

func newFuncsProcess(t *testing.T, script string) *engine.Process {
	t.Helper()
	proto, err := lua.CompileString(script, "test.lua")
	require.NoError(t, err)
	proc, err := engine.NewProcess(
		engine.WithProto(proto),
		engine.WithModuleBinder(func(l *lua.LState) error {
			engine.LoadModuleDef(l, engine.ChannelModule)
			return nil
		}),
		engine.WithModuleBinder(bindFuncsModule),
	)
	require.NoError(t, err)
	return proc
}

func extractInt64(v any) int64 {
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

// subSampler reads proc.LiveSubscriptionCount() on a ticker, bounded to the
// process's live phase by lifecycle hooks: begin() fires from OnStart (after
// Process.Init allocated the subs map) and end() fires from OnComplete on the
// worker goroutine before Process.Close. LiveSubscriptionCount takes the subs
// RLock that the step thread also holds for subscription map mutations, so
// reads inside this window never race.
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

// A single long-lived actor that calls funcs.async hundreds of times in a
// sequential async/await loop must not accumulate subscriptions. Each future
// is a one-shot @future:<uuid> topic reclaimed by the terminal frame the
// dispatcher appends to the result. The live subscription count is sampled
// concurrently and must stay bounded near one (sequential), then converge to
// zero.
func TestLeak_FuncsAsyncLoopNoSubscriptionAccumulation(t *testing.T) {
	tc := setupFuncsTest(t, 4)
	defer tc.Close(t)

	const iterations = 300

	funcID := registry.NewID("test", "inc")
	tc.registerFunction(t, funcID, func(_ context.Context, task runtime.Task) (*runtime.Result, error) {
		var v int64
		if len(task.Payloads) > 0 {
			v = extractInt64(task.Payloads[0].Data())
		}
		return &runtime.Result{Value: payload.New(v + 1)}, nil
	})

	script := fmt.Sprintf(`
		local n = %d
		local sum = 0
		for i = 1, n do
			local future, err = funcs.async("test:inc", i)
			if err then return nil, "async start failed: " .. tostring(err) end
			local p, ok = future:response():receive()
			if not ok then return nil, "future channel closed at " .. i end
			if p == nil then return nil, "nil result at " .. i end
			sum = sum + p:data()
		end
		return sum
	`, iterations)

	frameCtx, runPID := tc.frameCtxPID(t)
	proc := newFuncsProcess(t, script)

	sampler := newSubSampler(proc)
	tc.scheduler.setLifecycleHooks(runPID, sampler.begin, sampler.end)
	result, err := tc.scheduler.Execute(frameCtx, runPID, proc, "", nil)
	maxSeen, lastSeen, sampleCount := sampler.results()

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.Error, "script error: %v", result.Error)
	require.NotNil(t, result.Value)

	expected := int64(iterations) * int64(iterations+3) / 2
	assert.Equal(t, expected, extractInt64(result.Value.Data()), "results must be real, not nil/error")

	t.Logf("funcs async loop: %d iterations, %d samples, max live subscriptions=%d, last=%d", iterations, sampleCount, maxSeen, lastSeen)

	require.GreaterOrEqual(t, sampleCount, 1, "sampler never observed the process")
	assert.LessOrEqualf(t, maxSeen, 8, "live subscriptions climbed to %d over %d calls (leak: should stay ~1)", maxSeen, iterations)
}

// Concurrent async fan-out via funcs.async: a single actor starts a batch
// before awaiting any. Live subscriptions rise to the batch size while in
// flight, then each future's terminal frame reclaims its subscription as
// results arrive. Bounded by concurrency, not by total calls.
func TestLeak_FuncsAsyncFanOutReclaims(t *testing.T) {
	tc := setupFuncsTest(t, 4)
	defer tc.Close(t)

	const batch = 32
	const rounds = 20

	funcID := registry.NewID("test", "one")
	tc.registerFunction(t, funcID, func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{Value: payload.New(int64(1))}, nil
	})

	script := fmt.Sprintf(`
		local batch, rounds = %d, %d
		local total = 0
		for r = 1, rounds do
			local futures = {}
			for i = 1, batch do
				local f, err = funcs.async("test:one")
				if err then return nil, "start failed: " .. tostring(err) end
				futures[i] = f
			end
			for i = 1, batch do
				local p, ok = futures[i]:response():receive()
				if not ok then return nil, "channel closed r=" .. r .. " i=" .. i end
				if p == nil then return nil, "nil result r=" .. r .. " i=" .. i end
				total = total + p:data()
			end
		end
		return total
	`, batch, rounds)

	frameCtx, runPID := tc.frameCtxPID(t)
	proc := newFuncsProcess(t, script)

	sampler := newSubSampler(proc)
	tc.scheduler.setLifecycleHooks(runPID, sampler.begin, sampler.end)
	result, err := tc.scheduler.Execute(frameCtx, runPID, proc, "", nil)
	maxSeen, lastSeen, sampleCount := sampler.results()

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.Error, "script error: %v", result.Error)
	require.NotNil(t, result.Value)
	assert.Equal(t, int64(batch*rounds), extractInt64(result.Value.Data()))

	t.Logf("funcs async fan-out: batch=%d rounds=%d (total=%d calls), %d samples, max live subscriptions=%d, last=%d", batch, rounds, batch*rounds, sampleCount, maxSeen, lastSeen)

	require.GreaterOrEqual(t, sampleCount, 1, "sampler never observed the process")
	assert.LessOrEqualf(t, maxSeen, batch+8, "in-flight subscriptions %d exceed batch concurrency", maxSeen)
}
