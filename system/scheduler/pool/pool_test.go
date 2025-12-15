package pool

import (
	"context"
	"sync"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
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

// testContextWithPID creates a context with a specific PID for testing.
func testContextWithPID(testPID string) context.Context {
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	_ = runtime.SetFramePID(ctx, pid.PID{UniqID: testPID})
	return ctx
}
