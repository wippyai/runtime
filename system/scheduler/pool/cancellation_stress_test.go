package pool

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
)

// slowYieldingProcess yields multiple times with small delays
type slowYieldingProcess struct {
	mu          sync.Mutex
	steps       int
	maxSteps    int
	stepLatency time.Duration
	closed      atomic.Bool
}

func (p *slowYieldingProcess) Init(_ context.Context, _ string, input payload.Payloads) error {
	p.maxSteps = 10
	p.stepLatency = 1 * time.Millisecond
	return nil
}

func (p *slowYieldingProcess) Step(events []process.Event, out *process.StepOutput) error {
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

	// Return continue (no yields means instant continue)
	out.Continue()
	return nil
}

func (p *slowYieldingProcess) Close() {
	p.closed.Store(true)
}

func (p *slowYieldingProcess) Send(_ *relay.Package) error { return nil }

// yieldingDispatcher handles yield commands
type yieldingDispatcher struct{}

func (d *yieldingDispatcher) Dispatch(cmd dispatcher.Command) dispatcher.Handler {
	return &instantYieldHandler{}
}

type instantYieldHandler struct{}

func (h *instantYieldHandler) Handle(_ context.Context, _ dispatcher.Command, tag any, receiver dispatcher.ResultReceiver) error {
	go receiver.CompleteYield(tag, nil, nil)
	return nil
}

// timerYieldDispatcher dispatches handlers that use time.AfterFunc
type timerYieldDispatcher struct {
	delay time.Duration
}

func (d *timerYieldDispatcher) Dispatch(cmd dispatcher.Command) dispatcher.Handler {
	return &timerYieldHandler{delay: d.delay}
}

type timerYieldHandler struct {
	delay time.Duration
}

func (h *timerYieldHandler) Handle(_ context.Context, _ dispatcher.Command, tag any, receiver dispatcher.ResultReceiver) error {
	time.AfterFunc(h.delay, func() {
		receiver.CompleteYield(tag, nil, nil)
	})
	return nil
}

func newSlowYieldingFactory() Factory {
	return func() (process.Process, error) {
		return &slowYieldingProcess{}, nil
	}
}

// actualYieldingProcess yields commands that need handler completion
type actualYieldingProcess struct {
	mu       sync.Mutex
	steps    int
	maxSteps int
	closed   atomic.Bool
	waiting  map[int]bool
	nextTag  int
}

func (p *actualYieldingProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	p.maxSteps = 10
	p.waiting = make(map[int]bool)
	return nil
}

func (p *actualYieldingProcess) Step(events []process.Event, out *process.StepOutput) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Process completed yields
	for _, ev := range events {
		if ev.Type == process.EventYieldComplete {
			tag := ev.Tag.(int)
			delete(p.waiting, tag)
		}
	}

	p.steps++
	if p.steps >= p.maxSteps && len(p.waiting) == 0 {
		out.Done(nil)
		return nil
	}

	// Emit a yield
	p.nextTag++
	tag := p.nextTag
	p.waiting[tag] = true
	out.Yield(&testYieldCmd{}, tag)
	return nil
}

func (p *actualYieldingProcess) Close() {
	p.closed.Store(true)
}

func (p *actualYieldingProcess) Send(_ *relay.Package) error { return nil }

type testYieldCmd struct{}

func (c *testYieldCmd) CmdID() dispatcher.CommandID { return 1 }

func newActualYieldingFactory() Factory {
	return func() (process.Process, error) {
		return &actualYieldingProcess{}, nil
	}
}

// TestStaticConcurrentCancellationStress tests that cancelling requests during execution
// doesn't cause panics or race conditions.
func TestStaticConcurrentCancellationStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	pool, err := NewStatic(newSlowYieldingFactory(), &yieldingDispatcher{}, Config{
		Workers:   4,
		QueueSize: 100,
	})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}

	pool.Start()

	var wg sync.WaitGroup
	var completed, cancelled, errors atomic.Int64

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

			result, err := pool.Call(ctx, "test", nil)
			if err != nil {
				if ctx.Err() != nil {
					cancelled.Add(1)
				} else {
					errors.Add(1)
					t.Logf("request %d error: %v", id, err)
				}
				return
			}
			if result.Error != nil {
				errors.Add(1)
			} else {
				completed.Add(1)
			}
		}(i)
	}

	// Wait for all requests
	wg.Wait()

	// Now stop the pool
	pool.Stop()

	t.Logf("Completed: %d, Cancelled: %d, Errors: %d",
		completed.Load(), cancelled.Load(), errors.Load())
}

// TestStaticStopDuringExecution tests that Stop() waits for in-flight requests
func TestStaticStopDuringExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	pool, err := NewStatic(newSlowYieldingFactory(), &yieldingDispatcher{}, Config{
		Workers:   4,
		QueueSize: 100,
	})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}

	pool.Start()

	var wg sync.WaitGroup
	var completed atomic.Int64

	// Start 50 requests
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := pool.Call(context.Background(), "test", nil)
			if err == nil && result.Error == nil {
				completed.Add(1)
			}
		}()
	}

	// Let some start executing
	time.Sleep(5 * time.Millisecond)

	// Stop while requests are in-flight
	pool.Stop()

	// Wait for all goroutines
	wg.Wait()

	t.Logf("Completed after stop: %d/50", completed.Load())
}

// TestLazyConcurrentCancellationStress tests lazy pool with cancellations
func TestLazyConcurrentCancellationStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	pool, err := NewLazy(newSlowYieldingFactory(), &yieldingDispatcher{}, LazyConfig{
		MaxWorkers:  8,
		IdleTimeout: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewLazy: %v", err)
	}

	pool.Start()

	var wg sync.WaitGroup
	var completed, cancelled, errors atomic.Int64

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

			result, err := pool.Call(ctx, "test", nil)
			if err != nil {
				if ctx.Err() != nil {
					cancelled.Add(1)
				} else {
					errors.Add(1)
				}
				return
			}
			if result.Error != nil {
				errors.Add(1)
			} else {
				completed.Add(1)
			}
		}(i)
	}

	wg.Wait()
	pool.Stop()

	t.Logf("Completed: %d, Cancelled: %d, Errors: %d",
		completed.Load(), cancelled.Load(), errors.Load())
}

// TestInlineConcurrentCancellationStress tests inline pool with cancellations
func TestInlineConcurrentCancellationStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	pool, err := NewInline(newSlowYieldingFactory(), &yieldingDispatcher{})
	if err != nil {
		t.Fatalf("NewInline: %v", err)
	}

	pool.Start()

	var wg sync.WaitGroup
	var completed, cancelled, errors atomic.Int64

	// Inline is single-threaded so fewer requests
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			ctx := context.Background()
			var cancel context.CancelFunc
			if id%5 == 0 {
				ctx, cancel = context.WithTimeout(ctx, time.Duration(id%20)*time.Millisecond)
				defer cancel()
			}

			result, err := pool.Call(ctx, "test", nil)
			if err != nil {
				if ctx.Err() != nil {
					cancelled.Add(1)
				} else {
					errors.Add(1)
				}
				return
			}
			if result.Error != nil {
				errors.Add(1)
			} else {
				completed.Add(1)
			}
		}(i)
	}

	wg.Wait()
	pool.Stop()

	t.Logf("Completed: %d, Cancelled: %d, Errors: %d",
		completed.Load(), cancelled.Load(), errors.Load())
}

// steppingPoolProcess tracks whether Step is being called
type steppingPoolProcess struct {
	stepping atomic.Bool
	steps    atomic.Int64
	closed   atomic.Bool
}

func (p *steppingPoolProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	return nil
}

func (p *steppingPoolProcess) Step(events []process.Event, out *process.StepOutput) error {
	p.stepping.Store(true)
	defer p.stepping.Store(false)

	p.steps.Add(1)
	time.Sleep(1 * time.Millisecond)

	if p.steps.Load() >= 5 {
		out.Done(nil)
		return nil
	}

	out.Continue()
	return nil
}

func (p *steppingPoolProcess) Close()                      { p.closed.Store(true) }
func (p *steppingPoolProcess) Send(_ *relay.Package) error { return nil }

// TestStaticStopNoStepping verifies that after Stop() returns, no process is mid-Step
func TestStaticStopNoStepping(t *testing.T) {
	var processes []*steppingPoolProcess
	var mu sync.Mutex

	factory := func() (process.Process, error) {
		proc := &steppingPoolProcess{}
		mu.Lock()
		processes = append(processes, proc)
		mu.Unlock()
		return proc, nil
	}

	pool, err := NewStatic(factory, &yieldingDispatcher{}, Config{
		Workers:   4,
		QueueSize: 100,
	})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}

	pool.Start()

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pool.Call(context.Background(), "test", nil)
		}()
	}

	// Let some start stepping
	time.Sleep(5 * time.Millisecond)

	// Stop should wait for any in-flight Step calls
	pool.Stop()

	// Wait for Call goroutines to return
	wg.Wait()

	// After Stop returns, NO process should be mid-step
	for i, proc := range processes {
		if proc.stepping.Load() {
			t.Fatalf("process %d still stepping after Stop()", i)
		}
	}
}

// TestPoolStopNoSteppingStress runs many iterations
func TestPoolStopNoSteppingStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	for iter := 0; iter < 20; iter++ {
		var processes []*steppingPoolProcess
		var mu sync.Mutex

		factory := func() (process.Process, error) {
			proc := &steppingPoolProcess{}
			mu.Lock()
			processes = append(processes, proc)
			mu.Unlock()
			return proc, nil
		}

		pool, err := NewStatic(factory, &yieldingDispatcher{}, Config{
			Workers:   8,
			QueueSize: 200,
		})
		if err != nil {
			t.Fatalf("NewStatic: %v", err)
		}

		pool.Start()

		const n = 100
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				pool.Call(context.Background(), "test", nil)
			}()
		}

		// Random delay
		time.Sleep(time.Duration(iter%5) * time.Millisecond)

		pool.Stop()
		wg.Wait()

		for i, proc := range processes {
			if proc.stepping.Load() {
				t.Fatalf("iteration %d: process %d still stepping after Stop()", iter, i)
			}
		}
	}
}

// TestAllPoolsRapidStopStart tests rapid start/stop cycles don't cause races
func TestAllPoolsRapidStopStart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	factories := map[string]func() (Pool, error){
		"static": func() (Pool, error) {
			return NewStatic(newSlowYieldingFactory(), &yieldingDispatcher{}, Config{Workers: 2})
		},
		"lazy": func() (Pool, error) {
			return NewLazy(newSlowYieldingFactory(), &yieldingDispatcher{}, LazyConfig{MaxWorkers: 2})
		},
		"inline": func() (Pool, error) {
			return NewInline(newSlowYieldingFactory(), &yieldingDispatcher{})
		},
	}

	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			for cycle := 0; cycle < 10; cycle++ {
				pool, err := factory()
				if err != nil {
					t.Fatalf("factory: %v", err)
				}

				pool.Start()

				// Submit some work
				var wg sync.WaitGroup
				for i := 0; i < 10; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
						defer cancel()
						pool.Call(ctx, "test", nil)
					}()
				}

				// Don't wait for completion, just stop
				time.Sleep(2 * time.Millisecond)
				pool.Stop()

				// Wait for goroutines to finish
				wg.Wait()
			}
		})
	}
}

// TestTimerBasedYieldCancellation tests that timer-based yield handlers
// don't race with context cancellation. This simulates time.AfterFunc
// completing a yield while the context is being cancelled externally.
func TestTimerBasedYieldCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	timerDispatcher := &timerYieldDispatcher{delay: 2 * time.Millisecond}

	factories := map[string]func() (Pool, error){
		"static": func() (Pool, error) {
			return NewStatic(newActualYieldingFactory(), timerDispatcher, Config{Workers: 4})
		},
		"lazy": func() (Pool, error) {
			return NewLazy(newActualYieldingFactory(), timerDispatcher, LazyConfig{MaxWorkers: 4})
		},
		"inline": func() (Pool, error) {
			return NewInline(newActualYieldingFactory(), timerDispatcher)
		},
	}

	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			pool, err := factory()
			if err != nil {
				t.Fatalf("factory: %v", err)
			}

			pool.Start()

			var wg sync.WaitGroup
			var completed, cancelled atomic.Int64

			// Fire 100 requests with very short timeouts
			// The timer delay (2ms) should often fire after context is cancelled
			for i := 0; i < 100; i++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()

					// Short timeout: 1-5ms while handler takes 2ms
					timeout := time.Duration(1+id%5) * time.Millisecond
					ctx, cancel := context.WithTimeout(context.Background(), timeout)
					defer cancel()

					result, err := pool.Call(ctx, "test", nil)
					if err != nil || (result != nil && result.Error != nil) {
						cancelled.Add(1)
					} else {
						completed.Add(1)
					}
				}(i)
			}

			wg.Wait()
			pool.Stop()

			t.Logf("%s: completed=%d, cancelled=%d", name, completed.Load(), cancelled.Load())
		})
	}
}

// TestExternalCancellationDuringYield tests cancelling context externally
// while a yield handler is in-flight. This is the most aggressive test.
func TestExternalCancellationDuringYield(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	// Use a 5ms timer delay to give us time to cancel externally
	timerDispatcher := &timerYieldDispatcher{delay: 5 * time.Millisecond}

	factories := map[string]func() (Pool, error){
		"static": func() (Pool, error) {
			return NewStatic(newActualYieldingFactory(), timerDispatcher, Config{Workers: 4})
		},
		"lazy": func() (Pool, error) {
			return NewLazy(newActualYieldingFactory(), timerDispatcher, LazyConfig{MaxWorkers: 4})
		},
		"inline": func() (Pool, error) {
			return NewInline(newActualYieldingFactory(), timerDispatcher)
		},
	}

	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			for iter := 0; iter < 20; iter++ {
				pool, err := factory()
				if err != nil {
					t.Fatalf("factory: %v", err)
				}

				pool.Start()

				var wg sync.WaitGroup

				// Launch 10 requests with manual cancel
				cancels := make([]context.CancelFunc, 10)
				for i := 0; i < 10; i++ {
					wg.Add(1)
					ctx, cancel := context.WithCancel(context.Background())
					cancels[i] = cancel
					go func() {
						defer wg.Done()
						pool.Call(ctx, "test", nil)
					}()
				}

				// Wait a bit for yields to be in-flight
				time.Sleep(1 * time.Millisecond)

				// Cancel all contexts while handlers are active
				for _, cancel := range cancels {
					cancel()
				}

				wg.Wait()
				pool.Stop()
			}
		})
	}
}
