package actor

import (
	"context"
	"testing"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
)

// Minimal process - single step, immediate complete
type SingleStepProcess struct{}

func (p *SingleStepProcess) Execute(ctx context.Context, method string, input payload.Payloads) error {
	return nil
}

func (p *SingleStepProcess) Step(results *YieldResults) (StepResult, error) {
	var r StepResult
	r.Status = StepDone
	return r, nil
}

func (p *SingleStepProcess) Send(pkg *relay.Package) error { return nil }
func (p *SingleStepProcess) Close()                        {}

// Process that yields once with immediate handler
type OneYieldProcess struct {
	done bool
}

func (p *OneYieldProcess) Execute(ctx context.Context, method string, input payload.Payloads) error {
	return nil
}

func (p *OneYieldProcess) Step(results *YieldResults) (StepResult, error) {
	if p.done {
		var r StepResult
		r.Status = StepDone
		return r, nil
	}
	p.done = true
	var r StepResult
	r.Status = StepContinue
	r.AddYield(YieldCmd{})
	return r, nil
}

func (p *OneYieldProcess) Send(pkg *relay.Package) error { return nil }
func (p *OneYieldProcess) Close()                        {}

// Immediate sync handler - no goroutine
func ImmediateHandler() dispatcher.Handler {
	return dispatcher.HandlerFunc(func(ctx context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
		emit.Emit(nil, nil)
		return nil
	})
}

func BenchmarkSingleStep(b *testing.B) {
	registry := NewRegistry()
	sched := NewScheduler(registry, WithWorkers(1))
	sched.Start()
	defer sched.Stop()

	ctx := context.Background()
	pid := relay.PID{UniqID: "test"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sched.Execute(ctx, pid, &SingleStepProcess{}, "", nil)
	}
}

func BenchmarkOneYield(b *testing.B) {
	registry := NewRegistry()
	registry.Register(CmdYield, ImmediateHandler())
	sched := NewScheduler(registry, WithWorkers(1))
	sched.Start()
	defer sched.Stop()

	ctx := context.Background()
	pid := relay.PID{UniqID: "test"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sched.Execute(ctx, pid, &OneYieldProcess{}, "", nil)
	}
}
