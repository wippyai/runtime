// SPDX-License-Identifier: MPL-2.0

// Package wasmsched holds a reproduction harness for the worker-class principle:
// CPU-bound work executed directly on actor-scheduler workers (e.g. a CPU-bound
// process.wasm, or any synchronous work on a worker) starves co-scheduled actors,
// and moving that work off the workers (a dedicated pool) keeps the scheduler
// responsive. It validates that the shipped pool.class=wasm pinned pool runs work
// off the actor workers.
//
// Note: the funcs.call path already offloads WASM to a dispatcher goroutine
// (system/function/dispatcher.go handleCall), so this harness models the
// process.wasm / on-worker case and the general principle, not the funcs.call
// path. See proofs/runtime/.../README.md for the full root-cause analysis.
package wasmsched

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/system/scheduler"
	"github.com/wippyai/runtime/system/scheduler/actor"
	"github.com/wippyai/runtime/system/scheduler/pool/static"
)

const cmdHeavy dispatcher.CommandID = 100

type heavyCmd struct{ dur time.Duration }

func (heavyCmd) CmdID() dispatcher.CommandID { return cmdHeavy }

var cpuSink atomic.Uint64

// burn occupies the calling goroutine with real CPU work for at least d.
// It writes to a global sink so the loop cannot be optimized away.
func burn(d time.Duration) {
	start := time.Now()
	var x uint64
	for time.Since(start) < d {
		for i := 0; i < 4096; i++ {
			x = x*1664525 + 1013904223
		}
		cpuSink.Add(x)
	}
}

// heavyInlineProcess models CPU-bound work that runs inside Step, on the
// actor-scheduler worker goroutine, until the deadline (e.g. a CPU-bound
// process.wasm, or any synchronous work that occupies a worker).
type heavyInlineProcess struct{ deadline time.Time }

func (p *heavyInlineProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	return nil
}

func (p *heavyInlineProcess) Step(_ []process.Event, out *process.StepOutput) error {
	for time.Now().Before(p.deadline) {
		burn(5 * time.Millisecond)
	}
	out.Done(nil)
	return nil
}

func (p *heavyInlineProcess) Send(*relay.Package) error { return nil }
func (p *heavyInlineProcess) Close()                    {}

// heavyIsolatedProcess models an offloaded pool: Step yields a command whose
// handler runs the CPU-bound chunk on a separate goroutine, so the actor worker
// is free between chunks. It keeps offloading chunks until the deadline.
type heavyIsolatedProcess struct {
	deadline time.Time
	chunk    time.Duration
	started  bool
}

func (p *heavyIsolatedProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	return nil
}

func (p *heavyIsolatedProcess) Step(_ []process.Event, out *process.StepOutput) error {
	if p.started && time.Now().After(p.deadline) {
		out.Done(nil)
		return nil
	}
	p.started = true
	out.Yield(heavyCmd{dur: p.chunk}, 0)
	out.Continue()
	return nil
}

func (p *heavyIsolatedProcess) Send(*relay.Package) error { return nil }
func (p *heavyIsolatedProcess) Close()                    {}

// heavyHandler runs the CPU chunk off the worker, then completes the yield.
func heavyHandler() dispatcher.Handler {
	return dispatcher.HandlerFunc(func(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
		c := cmd.(heavyCmd)
		go func() {
			burn(c.dur)
			receiver.CompleteYield(tag, nil, nil)
		}()
		return nil
	})
}

// victimProcess is a trivial actor that completes in a single step. Its
// Submit->OnComplete latency measures how long a ready actor waits for a worker.
type victimProcess struct{}

func (victimProcess) Init(_ context.Context, _ string, _ payload.Payloads) error { return nil }
func (victimProcess) Step(_ []process.Event, out *process.StepOutput) error {
	out.Done(nil)
	return nil
}
func (victimProcess) Send(*relay.Package) error { return nil }
func (victimProcess) Close()                    {}

type latencyLifecycle struct {
	starts  map[string]time.Time
	samples []time.Duration
	mu      sync.Mutex
}

func newLatencyLifecycle() *latencyLifecycle {
	return &latencyLifecycle{starts: make(map[string]time.Time)}
}

func (l *latencyLifecycle) markSubmit(id string) {
	l.mu.Lock()
	l.starts[id] = time.Now()
	l.mu.Unlock()
}

func (l *latencyLifecycle) OnStart(context.Context, pidapi.PID, process.Process) error { return nil }

func (l *latencyLifecycle) OnComplete(_ context.Context, p pidapi.PID, _ *runtime.Result) {
	l.mu.Lock()
	if t0, ok := l.starts[p.UniqID]; ok {
		l.samples = append(l.samples, time.Since(t0))
		delete(l.starts, p.UniqID)
	}
	l.mu.Unlock()
}

func (l *latencyLifecycle) percentiles() (p50, p99, mx time.Duration, n int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n = len(l.samples)
	if n == 0 {
		return 0, 0, 0, 0
	}
	s := make([]time.Duration, n)
	copy(s, l.samples)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[n*50/100], s[min(n*99/100, n-1)], s[n-1], n
}

type scenarioResult struct {
	p50, p99, max time.Duration
	victims       int
}

// runScenario starts a scheduler with `workers` workers, launches `heavy`
// heavy actors of the given kind for `window`, and submits a victim actor every
// `tick` throughout, returning the victim latency distribution.
func runScenario(t *testing.T, workers, heavy int, window, tick time.Duration, isolated bool) scenarioResult {
	t.Helper()

	reg := scheduler.NewRegistry()
	reg.Register(cmdHeavy, heavyHandler())

	lc := newLatencyLifecycle()
	s := actor.NewScheduler(reg, actor.WithWorkers(workers), actor.WithLifecycle(lc))
	s.Start()

	ctx := context.Background()
	deadline := time.Now().Add(window)
	var idc atomic.Uint64

	submit := func(prefix string, p process.Process) {
		id := prefix + "-" + time.Now().Format("150405.000000000") + "-" + itoa(idc.Add(1))
		pid := pidapi.PID{UniqID: id}
		if prefix == "victim" {
			lc.markSubmit(id)
		}
		_, _ = s.Submit(ctx, pid, p, "", nil)
	}

	var wg sync.WaitGroup
	wg.Add(heavy)
	for i := 0; i < heavy; i++ {
		go func() {
			defer wg.Done()
			if isolated {
				submit("heavy", &heavyIsolatedProcess{deadline: deadline, chunk: 20 * time.Millisecond})
			} else {
				submit("heavy", &heavyInlineProcess{deadline: deadline})
			}
		}()
	}
	wg.Wait()

	stop := make(chan struct{})
	var vwg sync.WaitGroup
	vwg.Add(1)
	go func() {
		defer vwg.Done()
		tk := time.NewTicker(tick)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
				submit("victim", victimProcess{})
			}
		}
	}()

	time.Sleep(window)
	close(stop)
	vwg.Wait()
	time.Sleep(200 * time.Millisecond)
	s.Stop(context.Background())

	p50, p99, mx, n := lc.percentiles()
	return scenarioResult{p50: p50, p99: p99, max: mx, victims: n}
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

// poolBurnProcess is a pool process whose single step burns CPU for chunk then
// completes — the unit of CPU-bound work executed on a dedicated pool worker.
type poolBurnProcess struct{ chunk time.Duration }

func (p *poolBurnProcess) Init(context.Context, string, payload.Payloads) error { return nil }
func (p *poolBurnProcess) Step(_ []process.Event, out *process.StepOutput) error {
	burn(p.chunk)
	out.Done(nil)
	return nil
}
func (p *poolBurnProcess) Send(*relay.Package) error { return nil }
func (p *poolBurnProcess) Close()                    {}

type nilDispatcher struct{}

func (nilDispatcher) Dispatch(dispatcher.Command) dispatcher.Handler { return nil }

// runScenarioPinnedPool drives heavy CPU work through the real shipped
// thread-pinned static pool (pool.class=wasm) while the actor scheduler only
// serves victims, measuring victim latency under the production code path.
func runScenarioPinnedPool(t *testing.T, workers, poolWorkers int, window, tick time.Duration) scenarioResult {
	t.Helper()

	reg := scheduler.NewRegistry()
	lc := newLatencyLifecycle()
	s := actor.NewScheduler(reg, actor.WithWorkers(workers), actor.WithLifecycle(lc))
	s.Start()

	p, err := static.New(
		func() (process.Process, error) { return &poolBurnProcess{chunk: 20 * time.Millisecond}, nil },
		nilDispatcher{},
		static.Config{Workers: poolWorkers, PinThread: true},
	)
	if err != nil {
		t.Fatalf("static.New: %v", err)
	}
	p.Start()

	ctx := context.Background()
	deadline := time.Now().Add(window)
	var idc atomic.Uint64

	loadStop := make(chan struct{})
	var lwg sync.WaitGroup
	for i := 0; i < poolWorkers; i++ {
		lwg.Add(1)
		go func() {
			defer lwg.Done()
			for time.Now().Before(deadline) {
				select {
				case <-loadStop:
					return
				default:
					_, _ = p.Call(ctx, "burn", nil)
				}
			}
		}()
	}

	vStop := make(chan struct{})
	var vwg sync.WaitGroup
	vwg.Add(1)
	go func() {
		defer vwg.Done()
		tk := time.NewTicker(tick)
		defer tk.Stop()
		for {
			select {
			case <-vStop:
				return
			case <-tk.C:
				id := "victim-" + itoa(idc.Add(1))
				lc.markSubmit(id)
				_, _ = s.Submit(ctx, pidapi.PID{UniqID: id}, victimProcess{}, "", nil)
			}
		}
	}()

	time.Sleep(window)
	close(vStop)
	close(loadStop)
	vwg.Wait()
	lwg.Wait()
	p.Stop()
	time.Sleep(100 * time.Millisecond)
	s.Stop(context.Background())

	p50, p99, mx, n := lc.percentiles()
	return scenarioResult{p50: p50, p99: p99, max: mx, victims: n}
}

// TestPinnedPoolKeepsSchedulerResponsive proves the shipped thread-pinned pool
// keeps the actor scheduler responsive under sustained CPU-bound WASM load.
func TestPinnedPoolKeepsSchedulerResponsive(t *testing.T) {
	const (
		workers = 4
		window  = 800 * time.Millisecond
		tick    = 5 * time.Millisecond
	)

	inline := runScenario(t, workers, workers, window, tick, false)
	pinned := runScenarioPinnedPool(t, workers, workers, window, tick)

	t.Logf("ON-WORKER   (CPU on actor workers):  victims=%d p50=%s p99=%s max=%s",
		inline.victims, inline.p50, inline.p99, inline.max)
	t.Logf("OFF-WORKER  (pool.class=wasm pinned): victims=%d p50=%s p99=%s max=%s",
		pinned.victims, pinned.p50, pinned.p99, pinned.max)

	if pinned.p99 > 50*time.Millisecond {
		t.Errorf("expected pinned-pool p99 to stay responsive (<=50ms), got %s", pinned.p99)
	}
	if inline.p99 < 5*pinned.p99 {
		t.Errorf("expected inline p99 (%s) to dwarf pinned-pool p99 (%s)", inline.p99, pinned.p99)
	}
}

// TestInlineVsIsolatedStarvation proves that CPU-bound work on the actor
// workers (inline) starves co-scheduled actors, while offloading it keeps the
// scheduler responsive. Run with:
//
//	go test ./tests/wasmsched/ -run TestInlineVsIsolatedStarvation -v
func TestInlineVsIsolatedStarvation(t *testing.T) {
	const (
		workers = 4
		heavy   = 4
		window  = 800 * time.Millisecond
		tick    = 5 * time.Millisecond
	)

	inline := runScenario(t, workers, heavy, window, tick, false)
	isolated := runScenario(t, workers, heavy, window, tick, true)

	t.Logf("workers=%d heavy=%d window=%s tick=%s", workers, heavy, window, tick)
	t.Logf("ON-WORKER  (CPU on actor workers):  victims=%d p50=%s p99=%s max=%s",
		inline.victims, inline.p50, inline.p99, inline.max)
	t.Logf("OFF-WORKER (dedicated pool):        victims=%d p50=%s p99=%s max=%s",
		isolated.victims, isolated.p50, isolated.p99, isolated.max)

	if inline.p99 < 100*time.Millisecond {
		t.Errorf("expected inline p99 to show starvation (>=100ms), got %s", inline.p99)
	}
	if isolated.p99 > 50*time.Millisecond {
		t.Errorf("expected isolated p99 to stay responsive (<=50ms), got %s", isolated.p99)
	}
	if inline.p99 < 5*isolated.p99 {
		t.Errorf("expected inline p99 (%s) to dwarf isolated p99 (%s)", inline.p99, isolated.p99)
	}
}
