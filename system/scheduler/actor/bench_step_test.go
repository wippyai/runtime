package actor

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

// Minimal process - single step, immediate complete
type SingleStepProcess struct{}

func (p *SingleStepProcess) Init(ctx context.Context, method string, input payload.Payloads) error {
	return nil
}

func (p *SingleStepProcess) Step(events []Event, out *StepOutput) error {
	out.Done(nil)
	return nil
}

func (p *SingleStepProcess) Send(pkg *relay.Package) error { return nil }
func (p *SingleStepProcess) Close()                        {}

// Process that yields once with immediate handler
type OneYieldProcess struct {
	done bool
}

func (p *OneYieldProcess) Init(ctx context.Context, method string, input payload.Payloads) error {
	return nil
}

func (p *OneYieldProcess) Step(events []Event, out *StepOutput) error {
	if p.done {
		out.Done(nil)
		return nil
	}
	p.done = true
	out.Yield(YieldCmd{}, nil)
	out.Continue()
	return nil
}

func (p *OneYieldProcess) Send(pkg *relay.Package) error { return nil }
func (p *OneYieldProcess) Close()                        {}

// Immediate sync handler - no goroutine
func ImmediateHandler() dispatcher.Handler {
	return dispatcher.HandlerFunc(func(ctx context.Context, cmd dispatcher.Command, tag any, receiver dispatcher.ResultReceiver) error {
		receiver.CompleteYield(tag, nil, nil)
		return nil
	})
}

func BenchmarkSingleStep(b *testing.B) {
	var completed atomic.Int64
	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			completed.Add(1)
		},
	}

	registry := NewRegistry()
	sched := NewScheduler(registry, WithWorkers(1), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pid := relay.PID{UniqID: fmt.Sprintf("test-%d", i)}
		sched.Submit(ctx, pid, &SingleStepProcess{}, "", nil)
	}

	// Wait for completion
	for completed.Load() < int64(b.N) {
	}
}

func BenchmarkOneYield(b *testing.B) {
	var completed atomic.Int64
	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			completed.Add(1)
		},
	}

	registry := NewRegistry()
	registry.Register(CmdYield, ImmediateHandler())
	sched := NewScheduler(registry, WithWorkers(1), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pid := relay.PID{UniqID: fmt.Sprintf("test-%d", i)}
		sched.Submit(ctx, pid, &OneYieldProcess{}, "", nil)
	}

	// Wait for completion
	for completed.Load() < int64(b.N) {
	}
}
