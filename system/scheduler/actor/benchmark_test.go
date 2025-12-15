package actor

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	apiruntime "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/system/scheduler"
)

// Minimal process - single step, immediate complete
type SingleStepProcess struct{}

func (p *SingleStepProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	return nil
}

func (p *SingleStepProcess) Step(_ []process.Event, out *process.StepOutput) error {
	out.Done(nil)
	return nil
}

func (p *SingleStepProcess) Send(*relay.Package) error { return nil }
func (p *SingleStepProcess) Close()                    {}

// Process that yields once with immediate handler
type OneYieldProcess struct {
	done bool
}

func (p *OneYieldProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	return nil
}

func (p *OneYieldProcess) Step(_ []process.Event, out *process.StepOutput) error {
	if p.done {
		out.Done(nil)
		return nil
	}
	p.done = true
	out.Yield(YieldCmd{}, 0)
	out.Continue()
	return nil
}

func (p *OneYieldProcess) Send(*relay.Package) error { return nil }
func (p *OneYieldProcess) Close()                    {}

// Process that does N yields before completing
type NYieldProcess struct {
	remaining int
}

func (p *NYieldProcess) Init(_ context.Context, _ string, input payload.Payloads) error {
	if len(input) > 0 {
		p.remaining = input[0].Data().(int)
	}
	return nil
}

func (p *NYieldProcess) Step(_ []process.Event, out *process.StepOutput) error {
	if p.remaining <= 0 {
		out.Done(nil)
		return nil
	}
	p.remaining--
	out.Yield(YieldCmd{}, 0)
	out.Continue()
	return nil
}

func (p *NYieldProcess) Send(*relay.Package) error { return nil }
func (p *NYieldProcess) Close()                    {}

type RandomYieldProcess struct {
	steps    int
	maxSteps int
}

func (p *RandomYieldProcess) Init(_ context.Context, _ string, input payload.Payloads) error {
	if len(input) > 0 {
		if v, ok := input[0].Data().(int); ok {
			p.maxSteps = v
		}
	}
	if p.maxSteps == 0 {
		p.maxSteps = 5
	}
	return nil
}

func (p *RandomYieldProcess) Step(_ []process.Event, out *process.StepOutput) error {
	p.steps++
	if p.steps >= p.maxSteps {
		out.Done(nil)
		return nil
	}

	out.Yield(YieldCmd{}, 0)
	out.Continue()
	return nil
}

func (p *RandomYieldProcess) Send(_ *relay.Package) error { return nil }
func (p *RandomYieldProcess) Close()                      {}

func benchImmediateHandler() dispatcher.Handler {
	return dispatcher.HandlerFunc(func(_ context.Context, _ dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
		receiver.CompleteYield(tag, nil, nil)
		return nil
	})
}

type InstantHandler struct{}

func (h *InstantHandler) Handle(_ context.Context, _ dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	receiver.CompleteYield(tag, nil, nil)
	return nil
}

// BenchmarkSingleStep measures overhead of single-step process execution.
func BenchmarkSingleStep(b *testing.B) {
	var completed atomic.Int64
	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, _ *apiruntime.Result) {
			completed.Add(1)
		},
	}

	registry := scheduler.NewRegistry()
	sched := NewScheduler(registry, WithWorkers(1), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop(context.Background())

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pid := pidapi.PID{UniqID: fmt.Sprintf("test-%d", i)}
		_, _ = sched.Submit(ctx, pid, &SingleStepProcess{}, "", nil)
	}

	for completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
}

// BenchmarkOneYield measures overhead of single yield + immediate completion.
func BenchmarkOneYield(b *testing.B) {
	var completed atomic.Int64
	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, _ *apiruntime.Result) {
			completed.Add(1)
		},
	}

	registry := scheduler.NewRegistry()
	registry.Register(CmdYield, benchImmediateHandler())
	sched := NewScheduler(registry, WithWorkers(1), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop(context.Background())

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pid := pidapi.PID{UniqID: fmt.Sprintf("test-%d", i)}
		_, _ = sched.Submit(ctx, pid, &OneYieldProcess{}, "", nil)
	}

	for completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
}

// BenchmarkManyYieldsPerExecute measures amortized cost with 100 yields per process.
func BenchmarkManyYieldsPerExecute(b *testing.B) {
	var completed atomic.Int64
	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, _ *apiruntime.Result) {
			completed.Add(1)
		},
	}

	registry := scheduler.NewRegistry()
	registry.Register(CmdYield, benchImmediateHandler())
	sched := NewScheduler(registry, WithWorkers(1), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop(context.Background())

	ctx := context.Background()
	input := payload.Payloads{payload.New(100)}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pid := pidapi.PID{UniqID: fmt.Sprintf("test-%d", i)}
		_, _ = sched.Submit(ctx, pid, &NYieldProcess{}, "", input)
	}

	for completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
}

// BenchmarkWorkerExecute measures raw worker.executeOne performance.
func BenchmarkWorkerExecute(b *testing.B) {
	sched := newTestScheduler(1)
	worker := sched.workers[0]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := &CounterProcess{}
		_ = p.Init(context.Background(), "", testInput(0))

		proc := &Processor{
			id:        uint64(i), //nolint:gosec // benchmark counter always positive
			Process:   p,
			scheduler: sched,
			queue:     process.NewEventQueue(),
		}
		proc.state.Store(int32(StateReady))

		worker.executeOne(proc)
	}
}

func BenchmarkExecuteThroughput(b *testing.B) {
	te := newTestExecutor(runtime.GOMAXPROCS(0))
	te.Start()
	defer te.Stop()

	ctx := context.Background()
	input := testInput(10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pid := pidapi.PID{UniqID: fmt.Sprintf("bench-%d", i)}
		_, _ = te.Execute(ctx, pid, &CounterProcess{}, "", input)
	}
}

// BenchmarkExecuteParallelThroughput measures parallel Execute throughput.
func BenchmarkExecuteParallelThroughput(b *testing.B) {
	te := newTestExecutor(runtime.GOMAXPROCS(0))
	te.Start()
	defer te.Stop()

	input := testInput(10)
	var counter atomic.Int64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			i := counter.Add(1)
			pid := pidapi.PID{UniqID: fmt.Sprintf("bench-%d", i)}
			_, _ = te.Execute(ctx, pid, &CounterProcess{}, "", input)
		}
	})
}

// BenchmarkWorkerScaling1 through BenchmarkWorkerScaling32 measure scaling.
func BenchmarkWorkerScaling1(b *testing.B)  { benchmarkWithWorkers(b, 1) }
func BenchmarkWorkerScaling2(b *testing.B)  { benchmarkWithWorkers(b, 2) }
func BenchmarkWorkerScaling4(b *testing.B)  { benchmarkWithWorkers(b, 4) }
func BenchmarkWorkerScaling8(b *testing.B)  { benchmarkWithWorkers(b, 8) }
func BenchmarkWorkerScaling16(b *testing.B) { benchmarkWithWorkers(b, 16) }
func BenchmarkWorkerScaling32(b *testing.B) { benchmarkWithWorkers(b, 32) }

func benchmarkWithWorkers(b *testing.B, workers int) {
	te := newTestExecutor(workers)
	te.Start()
	defer te.Stop()

	ctx := context.Background()
	input := testInput(10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pid := pidapi.PID{UniqID: fmt.Sprintf("bench-%d", i)}
		_, _ = te.Execute(ctx, pid, &CounterProcess{}, "", input)
	}
}

// BenchmarkQueueSize16 through BenchmarkQueueSize1024 measure queue impact.
func BenchmarkQueueSize16(b *testing.B)   { benchmarkWithQueueSize(b, 16) }
func BenchmarkQueueSize64(b *testing.B)   { benchmarkWithQueueSize(b, 64) }
func BenchmarkQueueSize256(b *testing.B)  { benchmarkWithQueueSize(b, 256) }
func BenchmarkQueueSize1024(b *testing.B) { benchmarkWithQueueSize(b, 1024) }

func benchmarkWithQueueSize(b *testing.B, queueSize int) {
	te := newTestExecutorWithOptions(4, WithQueueSize(queueSize))
	te.Start()
	defer te.Stop()

	ctx := context.Background()
	input := testInput(10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pid := pidapi.PID{UniqID: fmt.Sprintf("bench-%d", i)}
		_, _ = te.Execute(ctx, pid, &CounterProcess{}, "", input)
	}
}

func BenchmarkScheduler4Workers(b *testing.B)  { benchmarkScheduler(b, 4) }
func BenchmarkScheduler32Workers(b *testing.B) { benchmarkScheduler(b, 32) }

func benchmarkScheduler(b *testing.B, workers int) {
	registry := scheduler.NewRegistry()
	registry.Register(1, &InstantHandler{})

	var completed atomic.Int64
	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, _ *apiruntime.Result) {
			completed.Add(1)
		},
	}

	sched := NewScheduler(registry, WithWorkers(workers), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop(context.Background())

	var counter atomic.Int64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			i := counter.Add(1)
			proc := &RandomYieldProcess{}
			pid := pidapi.PID{UniqID: fmt.Sprintf("bench-%d", i)}
			_, _ = sched.Submit(context.Background(), pid, proc, "", testInput(3))
		}
	})

	for completed.Load() < counter.Load() {
		runtime.Gosched()
	}
}

// BenchmarkSchedulerHighContention measures performance under high contention.
func BenchmarkSchedulerHighContention(b *testing.B) {
	for _, workers := range []int{4, 16, 64} {
		name := fmt.Sprintf("%dworkers", workers)
		b.Run(name, func(b *testing.B) {
			registry := scheduler.NewRegistry()
			registry.Register(1, &InstantHandler{})

			var completed atomic.Int64
			lc := &testLifecycle{
				onComplete: func(_ context.Context, _ pidapi.PID, _ *apiruntime.Result) {
					completed.Add(1)
				},
			}

			sched := NewScheduler(registry, WithWorkers(workers), WithLifecycle(lc))
			sched.Start()
			defer sched.Stop(context.Background())

			var counter atomic.Uint64
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					id := counter.Add(1)
					proc := &RandomYieldProcess{}
					pid := pidapi.PID{UniqID: fmt.Sprintf("bench-%d", id)}
					_, _ = sched.Submit(context.Background(), pid, proc, "", testInput(5))
				}
			})

			for completed.Load() < int64(counter.Load()) { //nolint:gosec // counter is always small in benchmarks
				runtime.Gosched()
			}
		})
	}
}

// BenchmarkSchedulerMemory measures allocation overhead.
func BenchmarkSchedulerMemory(b *testing.B) {
	registry := scheduler.NewRegistry()
	registry.Register(1, &InstantHandler{})

	var completed atomic.Int64
	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, _ *apiruntime.Result) {
			completed.Add(1)
		},
	}

	sched := NewScheduler(registry, WithWorkers(runtime.GOMAXPROCS(0)), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop(context.Background())

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		proc := &RandomYieldProcess{}
		pid := pidapi.PID{UniqID: fmt.Sprintf("bench-%d", i)}
		_, _ = sched.Submit(context.Background(), pid, proc, "", testInput(3))
	}

	for completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
}

// BenchmarkIdleOverhead measures overhead of idle workers.
func BenchmarkIdleOverhead(b *testing.B) {
	sched := newTestScheduler(runtime.GOMAXPROCS(0))
	sched.Start()
	defer sched.Stop(context.Background())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		time.Sleep(time.Microsecond)
	}
}

// BenchmarkWakeupLatency measures latency of waking idle workers.
func BenchmarkWakeupLatency(b *testing.B) {
	te := newTestExecutor(4)
	te.Start()
	defer te.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		time.Sleep(100 * time.Microsecond)
		pid := pidapi.PID{UniqID: fmt.Sprintf("wake-%d", i)}
		_, _ = te.Execute(context.Background(), pid, &CounterProcess{}, "", testInput(1))
	}
}

func BenchmarkSchedulerSubmit(b *testing.B) {
	var completed atomic.Int64

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, _ *apiruntime.Result) {
			completed.Add(1)
		},
	}
	sched := newTestSchedulerWithLifecycle(runtime.GOMAXPROCS(0), lc)

	sched.Start()
	defer sched.Stop(context.Background())

	ctx := context.Background()
	pid := testPID()
	input := testInput(1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = sched.Submit(ctx, pid, &CounterProcess{}, "", input)
	}

	for completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
}

func BenchmarkSchedulerThroughput(b *testing.B) {
	var completed atomic.Int64

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, _ *apiruntime.Result) {
			completed.Add(1)
		},
	}
	sched := newTestSchedulerWithLifecycle(runtime.GOMAXPROCS(0), lc)

	sched.Start()
	defer sched.Stop(context.Background())

	ctx := context.Background()
	pid := testPID()
	input := testInput(10)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = sched.Submit(ctx, pid, &CounterProcess{}, "", input)
	}

	for completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
}

func BenchmarkSchedulerParallelSubmit(b *testing.B) {
	var completed atomic.Int64

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, _ *apiruntime.Result) {
			completed.Add(1)
		},
	}
	sched := newTestSchedulerWithLifecycle(runtime.GOMAXPROCS(0), lc)

	sched.Start()
	defer sched.Stop(context.Background())

	ctx := context.Background()
	pid := testPID()
	input := testInput(1)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = sched.Submit(ctx, pid, &CounterProcess{}, "", input)
		}
	})

	for completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
}

func BenchmarkSchedulerExecute(b *testing.B) {
	te := newTestExecutor(runtime.GOMAXPROCS(0))
	te.Start()
	defer te.Stop()

	ctx := context.Background()
	input := testInput(1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pid := pidapi.PID{UniqID: fmt.Sprintf("bench-%d", i)}
		_, _ = te.Execute(ctx, pid, &CounterProcess{}, "", input)
	}
}

func BenchmarkSchedulerParallelExecute(b *testing.B) {
	te := newTestExecutor(runtime.GOMAXPROCS(0))
	te.Start()
	defer te.Stop()

	ctx := context.Background()
	input := testInput(1)
	var counter atomic.Int64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			i := counter.Add(1)
			pid := pidapi.PID{UniqID: fmt.Sprintf("bench-%d", i)}
			_, _ = te.Execute(ctx, pid, &CounterProcess{}, "", input)
		}
	})
}
