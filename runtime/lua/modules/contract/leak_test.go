// SPDX-License-Identifier: MPL-2.0

package contract_test

import (
	"context"
	"fmt"
	"testing"
	stdtime "time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	apicontract "github.com/wippyai/runtime/api/contract"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/runtime/lua/engine"
	luapayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	systempayload "github.com/wippyai/runtime/system/payload"
	sysrelay "github.com/wippyai/runtime/system/relay"
)

// leakTestHostID is the relay host the scheduler registers under so the async
// contract dispatcher's result package (node.Send to the frame PID) routes
// back into the running process and resolves the future.
const leakTestHostID = "contract.leak.test"

// asyncFrameCtx registers the scheduler as a relay host (idempotent across
// tests sharing a node), returns a frame context with the frame PID set, and
// the matching PID. The async dispatcher reads the frame PID to address the
// result package; the host registration routes that package back to the
// process.
func asyncFrameCtx(t *testing.T, tc *integrationTestContext) (context.Context, pid.PID) {
	t.Helper()
	node, ok := tc.node.(*sysrelay.Node)
	require.True(t, ok, "expected *sysrelay.Node")
	// RegisterHost rejects duplicates; ignore the duplicate error so multiple
	// processes in one test share the host.
	_ = node.RegisterHost(leakTestHostID, tc.scheduler.Scheduler)

	p := pid.PID{Host: leakTestHostID, UniqID: uniqueTestPID().UniqID}
	p = p.Precomputed()

	// The future handler transcodes the Go-format result payload to Lua when
	// the script reads p:data(); without a transcoder on the context that
	// conversion fails. Production wires this through the runtime payload
	// transcoder.
	transcoder := systempayload.NewTranscoder()
	luapayload.Register(transcoder)
	luapayload.RegisterAllBasicFormats(transcoder)
	ctx := payload.WithTranscoder(tc.ctx, transcoder)

	frameCtx, _ := ctxapi.OpenFrameContext(ctx)
	require.NoError(t, runtime.SetFramePID(frameCtx, p))
	return frameCtx, p
}

// subSampler reads proc.LiveSubscriptionCount() on a ticker. Its observation
// window is bounded strictly to the process's live phase by lifecycle hooks:
// begin() is called from OnStart (after Process.Init has allocated the subs
// map) and end() is called from OnComplete on the worker goroutine before
// Process.Close (which nils the subs map). LiveSubscriptionCount takes the subs
// RLock and the step thread mutates the subscription maps under the same lock,
// so reads inside this window never race.
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

// results returns (maxLive, lastLive, samples). Safe to call after end().
func (s *subSampler) results() (int, int, int) {
	return s.maxLive, s.lastLive, s.samples
}

// A single long-lived actor that calls a contract method asynchronously
// hundreds of times in a sequential async/await loop must not accumulate
// subscriptions: each future is a one-shot topic reclaimed by the terminal
// frame the dispatcher appends to the result. The live subscription count is
// sampled concurrently while the actor runs and must stay bounded near the
// single concurrently-pending call, then converge to zero at completion.
func TestLeak_ContractAsyncLoopNoSubscriptionAccumulation(t *testing.T) {
	tc := setupIntegrationTest(t, 4)
	defer tc.Close(t)

	const iterations = 300

	funcID := registry.NewID("test", "echo_async")
	tc.registerFunction(t, funcID, func(_ context.Context, task runtime.Task) (*runtime.Result, error) {
		var v int64
		if len(task.Payloads) > 0 {
			v = extractInt64(task.Payloads[0].Data())
		}
		return &runtime.Result{Value: payload.New(v + 1)}, nil
	})

	contractID := registry.NewID("test", "echo_contract")
	tc.registerContract(t, contractID, &apicontract.Definition{
		Methods: []apicontract.MethodDef{{Name: "echo"}},
	})

	bindingID := registry.NewID("test", "echo_binding")
	tc.registerBinding(t, bindingID, &apicontract.Binding{
		Contracts: []apicontract.BoundContract{{
			Contract: contractID,
			Methods:  map[string]registry.ID{"echo": funcID},
			Default:  true,
		}},
	})

	// Each iteration starts an async call and immediately awaits it. The
	// per-call future subscribes to a unique @future:<uuid> topic; awaiting
	// drains the cap-1 channel. sum proves every result is real.
	script := fmt.Sprintf(`
		local instance, err = contract.open("test:echo_binding")
		if err then return nil, tostring(err) end

		local n = %d
		local sum = 0
		for i = 1, n do
			local future, err = instance:echo_async(i)
			if err then return nil, "async start failed: " .. tostring(err) end
			local p, ok = future:response():receive()
			if not ok then return nil, "future channel closed without result at " .. i end
			if p == nil then return nil, "nil result at " .. i end
			sum = sum + p:data()
		end
		return sum
	`, iterations)

	frameCtx, runPID := asyncFrameCtx(t, tc)
	proc := newLuaProcess(t, script)

	sampler := newSubSampler(proc)
	tc.scheduler.setLifecycleHooks(runPID, sampler.begin, sampler.end)
	result, err := tc.scheduler.Execute(frameCtx, runPID, proc, "", nil)
	maxSeen, lastSeen, sampleCount := sampler.results()

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.Error, "script error: %v", result.Error)
	require.NotNil(t, result.Value)

	// sum of 2..(iterations+1) == iterations*(iterations+3)/2
	expected := int64(iterations) * int64(iterations+3) / 2
	assert.Equal(t, expected, extractInt64(result.Value.Data()), "results must be real, not nil/error")

	t.Logf("contract async loop: %d iterations, %d samples, max live subscriptions=%d, last=%d", iterations, sampleCount, maxSeen, lastSeen)

	// Sequential async/await: at most one pending future at a time. Allow a
	// small slack for sampling-vs-step timing, but a leak would push this
	// toward `iterations`.
	require.GreaterOrEqual(t, sampleCount, 1, "sampler never observed the process")
	assert.LessOrEqualf(t, maxSeen, 8, "live subscriptions climbed to %d over %d calls (leak: should stay ~1)", maxSeen, iterations)
}

// concurrent async fan-out: a single actor starts a batch of async calls
// before awaiting any of them. Live subscriptions rise to the batch size while
// in flight, then every future's terminal frame reclaims its subscription as
// results arrive, converging to zero. This proves bounded-by-concurrency, not
// bounded-by-total.
func TestLeak_ContractAsyncFanOutReclaims(t *testing.T) {
	tc := setupIntegrationTest(t, 4)
	defer tc.Close(t)

	const batch = 32
	const rounds = 20

	funcID := registry.NewID("test", "fanout_fn")
	tc.registerFunction(t, funcID, func(_ context.Context, task runtime.Task) (*runtime.Result, error) {
		var v int64
		if len(task.Payloads) > 0 {
			v = extractInt64(task.Payloads[0].Data())
		}
		return &runtime.Result{Value: payload.New(v)}, nil
	})

	contractID := registry.NewID("test", "fanout_contract")
	tc.registerContract(t, contractID, &apicontract.Definition{
		Methods: []apicontract.MethodDef{{Name: "work"}},
	})

	bindingID := registry.NewID("test", "fanout_binding")
	tc.registerBinding(t, bindingID, &apicontract.Binding{
		Contracts: []apicontract.BoundContract{{
			Contract: contractID,
			Methods:  map[string]registry.ID{"work": funcID},
			Default:  true,
		}},
	})

	script := fmt.Sprintf(`
		local instance, err = contract.open("test:fanout_binding")
		if err then return nil, tostring(err) end

		local batch, rounds = %d, %d
		local total = 0
		for r = 1, rounds do
			local futures = {}
			for i = 1, batch do
				local f, err = instance:work_async(1)
				if err then return nil, "start failed: " .. tostring(err) end
				futures[i] = f
			end
			for i = 1, batch do
				local p, ok = futures[i]:response():receive()
				if not ok then return nil, "channel closed at r=" .. r .. " i=" .. i end
				if p == nil then return nil, "nil result at r=" .. r .. " i=" .. i end
				total = total + p:data()
			end
		end
		return total
	`, batch, rounds)

	frameCtx, runPID := asyncFrameCtx(t, tc)
	proc := newLuaProcess(t, script)

	sampler := newSubSampler(proc)
	tc.scheduler.setLifecycleHooks(runPID, sampler.begin, sampler.end)
	result, err := tc.scheduler.Execute(frameCtx, runPID, proc, "", nil)
	maxSeen, lastSeen, sampleCount := sampler.results()

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.Error, "script error: %v", result.Error)
	require.NotNil(t, result.Value)
	assert.Equal(t, int64(batch*rounds), extractInt64(result.Value.Data()))

	t.Logf("contract async fan-out: batch=%d rounds=%d (total=%d calls), %d samples, max live subscriptions=%d, last=%d", batch, rounds, batch*rounds, sampleCount, maxSeen, lastSeen)

	// In flight is bounded by the batch, never by total calls, plus a small
	// sampling slack.
	require.GreaterOrEqual(t, sampleCount, 1, "sampler never observed the process")
	assert.LessOrEqualf(t, maxSeen, batch+8, "in-flight subscriptions %d exceed batch concurrency", maxSeen)
}
