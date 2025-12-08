package pool

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

// mockProcess is a test process that returns immediately.
type mockProcess struct {
	mu         sync.Mutex
	execCount  int
	stepCount  int
	closeCount int
	latency    time.Duration
}

func (p *mockProcess) Init(ctx context.Context, method string, input payload.Payloads) error {
	p.mu.Lock()
	p.execCount++
	p.mu.Unlock()
	return nil
}

func (p *mockProcess) Step(_ []process.Event, out *process.StepOutput) error {
	p.mu.Lock()
	p.stepCount++
	latency := p.latency
	p.mu.Unlock()

	if latency > 0 {
		time.Sleep(latency)
	}

	out.Done(nil)
	return nil
}

func (p *mockProcess) Close() {
	p.mu.Lock()
	p.closeCount++
	p.mu.Unlock()
}

func (p *mockProcess) stats() (exec, step, closed int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.execCount, p.stepCount, p.closeCount
}

// mockDispatcher returns no handlers.
type mockDispatcher struct{}

func (d *mockDispatcher) Dispatch(dispatcher.Command) dispatcher.Handler { return nil }

// testContext creates a context with FrameContext and PID for testing.
func testContext() context.Context {
	return testContextWithPID("test-pid")
}

// testContextWithPID creates a context with a specific PID for testing.
func testContextWithPID(pid string) context.Context {
	ctx, _ := ctxapi.AcquireFrameContext(context.Background())
	_ = runtime.SetFramePID(ctx, relay.PID{UniqID: pid})
	return ctx
}

// factories

func newMockFactory(latency time.Duration) Factory {
	return func() (process.Process, error) {
		return &mockProcess{latency: latency}, nil
	}
}

func newCountingFactory() (Factory, *atomic.Int32) {
	count := &atomic.Int32{}
	return func() (process.Process, error) {
		count.Add(1)
		return &mockProcess{}, nil
	}, count
}

func newErrorFactory() Factory {
	return func() (process.Process, error) {
		return nil, fmt.Errorf("factory error")
	}
}

// resultProcess returns a result on StepDone
type resultProcess struct {
	result payload.Payload
}

func (p *resultProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	return nil
}

func (p *resultProcess) Step(_ []process.Event, out *process.StepOutput) error {
	out.Done(p.result)
	return nil
}

func (p *resultProcess) Close()                    {}
func (p *resultProcess) Send(*relay.Package) error { return nil }

// Hooks tests

func TestHooksOnStartCalled(t *testing.T) {
	var startCount atomic.Int32
	hooks := Hooks{
		OnStart: func(process.Process) {
			startCount.Add(1)
		},
	}

	factory := WrapFactoryWithHooks(newMockFactory(0), hooks)
	pool, err := NewStatic(factory, &mockDispatcher{}, Config{Workers: 3})
	if err != nil {
		t.Fatal(err)
	}
	pool.Start()
	defer pool.Stop()

	if startCount.Load() != 3 {
		t.Fatalf("expected 3 OnStart calls, got %d", startCount.Load())
	}
}

func TestHooksOnStopCalled(t *testing.T) {
	var startCount, stopCount atomic.Int32
	hooks := Hooks{
		OnStart: func(process.Process) {
			startCount.Add(1)
		},
		OnStop: func(process.Process) {
			stopCount.Add(1)
		},
	}

	factory := WrapFactoryWithHooks(newMockFactory(0), hooks)
	pool, err := NewStatic(factory, &mockDispatcher{}, Config{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	pool.Start()

	// Do some calls
	for i := 0; i < 5; i++ {
		_, _ = pool.Call(testContext(), "test", nil)
	}

	pool.Stop()

	if startCount.Load() != 2 {
		t.Fatalf("expected 2 OnStart calls, got %d", startCount.Load())
	}
	if stopCount.Load() != 2 {
		t.Fatalf("expected 2 OnStop calls, got %d", stopCount.Load())
	}
}

func TestHooksNoHooks(t *testing.T) {
	factory := newMockFactory(0)
	wrapped := WrapFactoryWithHooks(factory, Hooks{})

	proc1, _ := factory()
	proc2, _ := wrapped()

	if proc1 == nil || proc2 == nil {
		t.Fatal("expected non-nil processes")
	}
	proc1.Close()
	proc2.Close()
}
