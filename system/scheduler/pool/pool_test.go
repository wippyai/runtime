package pool

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
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

func (p *mockProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
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

// mockDispatcher returns no handlers.
type mockDispatcher struct{}

func (d *mockDispatcher) Dispatch(dispatcher.Command) dispatcher.Handler { return nil }

// testContext creates a context with FrameContext and PID for testing.
func testContext() context.Context {
	return testContextWithPID("test-pid")
}

// testContextWithPID creates a context with a specific PID for testing.
func testContextWithPID(testPID string) context.Context {
	ctx, _ := ctxapi.AcquireFrameContext(context.Background())
	_ = runtime.SetFramePID(ctx, pid.PID{UniqID: testPID})
	return ctx
}

// factories

func newMockFactory(latency time.Duration) process.FactoryFunc {
	return func() (process.Process, error) {
		return &mockProcess{latency: latency}, nil
	}
}

func newCountingFactory() (process.FactoryFunc, *atomic.Int32) {
	count := &atomic.Int32{}
	return func() (process.Process, error) {
		count.Add(1)
		return &mockProcess{}, nil
	}, count
}

func newErrorFactory() process.FactoryFunc {
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
