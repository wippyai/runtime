package actor

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/system/scheduler"
)

// slowProcess yields with small delays to increase chance of race conditions
type slowProcess struct {
	mu          sync.Mutex
	steps       int
	maxSteps    int
	stepLatency time.Duration
	closed      atomic.Bool
}

func (p *slowProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	p.maxSteps = 10
	p.stepLatency = 500 * time.Microsecond
	return nil
}

func (p *slowProcess) Step(events []Event, out *StepOutput) error {
	p.mu.Lock()
	p.steps++
	step := p.steps
	latency := p.stepLatency
	maxSteps := p.maxSteps
	p.mu.Unlock()

	if latency > 0 {
		time.Sleep(latency)
	}

	if step >= maxSteps {
		out.Done(nil)
		return nil
	}

	out.Yield(YieldCmd{}, nil)
	out.Continue()
	return nil
}

func (p *slowProcess) Close() {
	p.closed.Store(true)
}

func (p *slowProcess) Send(_ *relay.Package) error { return nil }

// delayedHandler adds small delay before emitting (async to avoid race)
type delayedHandler struct {
	delay time.Duration
}

func (h *delayedHandler) Handle(ctx context.Context, cmd dispatcher.Command, tag any, receiver dispatcher.ResultReceiver) error {
	go func() {
		time.Sleep(h.delay)
		receiver.CompleteYield(tag, nil, nil)
	}()
	return nil
}

// TestActorConcurrentCancellationStress tests that cancelling requests during execution
// doesn't cause panics or race conditions.
func TestActorConcurrentCancellationStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	var completed, errors atomic.Int64

	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			if result.Error != nil {
				errors.Add(1)
			} else {
				completed.Add(1)
			}
		},
	}

	registry := scheduler.NewRegistry()
	registry.Register(1, &delayedHandler{delay: 100 * time.Microsecond})

	sched := NewScheduler(registry, WithWorkers(4), WithKind(process.KindGlobal), WithLifecycle(lc))
	sched.Start()

	var wg sync.WaitGroup

	// Run 1000 requests with random cancellations
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Some requests get cancelled quickly, others run to completion
			ctx := context.Background()
			var cancel context.CancelFunc
			if id%3 == 0 {
				// Cancel after random short delay
				ctx, cancel = context.WithTimeout(ctx, time.Duration(id%10)*time.Millisecond)
				defer cancel()
			}

			proc := &slowProcess{}
			pid := relay.PID{UniqID: fmt.Sprintf("cancel-test-%d", id)}

			_, err := sched.Submit(ctx, pid, proc, "", nil)
			if err != nil {
				// Submit failed (context cancelled before submit)
				return
			}
		}(i)
	}

	// Wait for all goroutines to finish submitting
	wg.Wait()

	// Wait for all completions
	waitForCompletionInt64(&completed, 1000, 10*time.Second)

	// Now stop the scheduler
	sched.Stop()

	t.Logf("Completed: %d, Errors: %d", completed.Load(), errors.Load())
}

// TestActorStopDuringExecution tests that Stop() waits for in-flight requests
func TestActorStopDuringExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	var completed atomic.Int64

	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			if result.Error == nil {
				completed.Add(1)
			}
		},
	}

	registry := scheduler.NewRegistry()
	registry.Register(1, &delayedHandler{delay: 1 * time.Millisecond})

	sched := NewScheduler(registry, WithWorkers(4), WithKind(process.KindGlobal), WithLifecycle(lc))
	sched.Start()

	var wg sync.WaitGroup

	// Start 50 requests
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			proc := &slowProcess{}
			pid := relay.PID{UniqID: fmt.Sprintf("stop-test-%d", id)}
			sched.Submit(context.Background(), pid, proc, "", nil)
		}(i)
	}

	// Wait for submissions
	wg.Wait()

	// Let some start executing
	time.Sleep(5 * time.Millisecond)

	// Stop while requests are in-flight
	sched.Stop()

	t.Logf("Completed after stop: %d/50", completed.Load())
}

// TestActorStealingConcurrentCancellation tests work-stealing mode with cancellations
func TestActorStealingConcurrentCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	var completed, errors atomic.Int64

	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			if result.Error != nil {
				errors.Add(1)
			} else {
				completed.Add(1)
			}
		},
	}

	registry := scheduler.NewRegistry()
	registry.Register(1, &delayedHandler{delay: 50 * time.Microsecond})

	sched := NewScheduler(registry, WithWorkers(8), WithKind(process.KindStealing), WithLifecycle(lc))
	sched.Start()

	var wg sync.WaitGroup

	for i := 0; i < 500; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			ctx := context.Background()
			var cancel context.CancelFunc
			if id%4 == 0 {
				ctx, cancel = context.WithTimeout(ctx, time.Duration(id%15)*time.Millisecond)
				defer cancel()
			}

			proc := &slowProcess{}
			pid := relay.PID{UniqID: fmt.Sprintf("steal-test-%d", id)}

			sched.Submit(ctx, pid, proc, "", nil)
		}(i)
	}

	wg.Wait()

	// Wait for completions
	waitForCompletionInt64(&completed, 500, 10*time.Second)

	sched.Stop()

	t.Logf("Completed: %d, Errors: %d", completed.Load(), errors.Load())
}

// steppingProcess tracks whether Step is being called
type steppingProcess struct {
	stepping atomic.Bool
	steps    atomic.Int64
	closed   atomic.Bool
}

func (p *steppingProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	return nil
}

func (p *steppingProcess) Step(events []Event, out *StepOutput) error {
	p.stepping.Store(true)
	defer p.stepping.Store(false)

	p.steps.Add(1)
	time.Sleep(1 * time.Millisecond) // Slow step to increase race window

	if p.steps.Load() >= 5 {
		out.Done(nil)
		return nil
	}

	out.Yield(YieldCmd{}, nil)
	out.Continue()
	return nil
}

func (p *steppingProcess) Close()                      { p.closed.Store(true) }
func (p *steppingProcess) Send(_ *relay.Package) error { return nil }

// TestActorStopNoStepping verifies that after Stop() returns, no process is mid-Step
func TestActorStopNoStepping(t *testing.T) {
	for _, kind := range []process.SchedulerKind{process.KindGlobal, process.KindStealing} {
		t.Run(string(kind), func(t *testing.T) {
			registry := scheduler.NewRegistry()
			registry.Register(1, &delayedHandler{delay: 500 * time.Microsecond})

			sched := NewScheduler(registry, WithWorkers(4), WithKind(kind))
			sched.Start()

			// Track all processes
			var processes []*steppingProcess
			var mu sync.Mutex

			const n = 50
			for i := 0; i < n; i++ {
				proc := &steppingProcess{}
				mu.Lock()
				processes = append(processes, proc)
				mu.Unlock()
				pid := relay.PID{UniqID: fmt.Sprintf("step-test-%d", i)}
				sched.Submit(context.Background(), pid, proc, "", nil)
			}

			// Let some start stepping
			time.Sleep(5 * time.Millisecond)

			// Stop should wait for any in-flight Step calls
			sched.Stop()

			// After Stop returns, NO process should be mid-step
			for i, proc := range processes {
				if proc.stepping.Load() {
					t.Fatalf("process %d still stepping after Stop()", i)
				}
			}
		})
	}
}

// TestActorStopNoSteppingStress runs many iterations to catch races
func TestActorStopNoSteppingStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	for iter := 0; iter < 20; iter++ {
		registry := scheduler.NewRegistry()
		registry.Register(1, &delayedHandler{delay: 100 * time.Microsecond})

		sched := NewScheduler(registry, WithWorkers(8), WithKind(process.KindStealing))
		sched.Start()

		var processes []*steppingProcess
		var mu sync.Mutex

		const n = 100
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				proc := &steppingProcess{}
				mu.Lock()
				processes = append(processes, proc)
				mu.Unlock()
				pid := relay.PID{UniqID: fmt.Sprintf("iter%d-proc%d", iter, id)}
				sched.Submit(context.Background(), pid, proc, "", nil)
			}(i)
		}

		wg.Wait()

		// Random delay before stop
		time.Sleep(time.Duration(iter%5) * time.Millisecond)

		sched.Stop()

		// Verify no stepping after Stop
		for i, proc := range processes {
			if proc.stepping.Load() {
				t.Fatalf("iteration %d: process %d still stepping after Stop()", iter, i)
			}
		}
	}
}

// TestActorRapidStopStart tests rapid start/stop cycles don't cause races
func TestActorRapidStopStart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	for _, kind := range []process.SchedulerKind{process.KindGlobal, process.KindStealing} {
		t.Run(string(kind), func(t *testing.T) {
			for cycle := 0; cycle < 10; cycle++ {
				registry := scheduler.NewRegistry()
				registry.Register(1, &delayedHandler{delay: 100 * time.Microsecond})

				sched := NewScheduler(registry, WithWorkers(4), WithKind(kind))
				sched.Start()

				var wg sync.WaitGroup
				for i := 0; i < 20; i++ {
					wg.Add(1)
					go func(id int) {
						defer wg.Done()
						ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
						defer cancel()
						proc := &slowProcess{}
						pid := relay.PID{UniqID: fmt.Sprintf("rapid-%d", id)}
						sched.Submit(ctx, pid, proc, "", nil)
					}(i)
				}

				// Don't wait for completion, just stop
				time.Sleep(2 * time.Millisecond)
				sched.Stop()

				// Wait for goroutines to finish
				wg.Wait()
			}
		})
	}
}
