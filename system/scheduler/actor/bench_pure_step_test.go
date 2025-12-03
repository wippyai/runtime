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

// Process that does N yields before completing
type NYieldProcess struct {
	remaining int
}

func (p *NYieldProcess) Execute(ctx context.Context, method string, input payload.Payloads) error {
	if len(input) > 0 {
		p.remaining = input[0].Data().(int)
	}
	return nil
}

func (p *NYieldProcess) Step(results *YieldResults) (StepResult, error) {
	if p.remaining <= 0 {
		var r StepResult
		r.Status = StepDone
		return r, nil
	}
	p.remaining--
	var r StepResult
	r.Status = StepContinue
	r.AddYield(YieldCmd{})
	return r, nil
}

func (p *NYieldProcess) Send(pkg *relay.Package) error { return nil }
func (p *NYieldProcess) Close()                        {}

func ImmediateHandler2() dispatcher.Handler {
	return dispatcher.HandlerFunc(func(ctx context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
		emit.Emit(nil, nil)
		return nil
	})
}

// Benchmark: 1 execute with 100 yields = amortize execute cost
func BenchmarkManyYieldsPerExecute(b *testing.B) {
	var completed atomic.Int64
	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			completed.Add(1)
		},
	}

	registry := NewRegistry()
	registry.Register(CmdYield, ImmediateHandler2())
	sched := NewScheduler(registry, WithWorkers(1), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop()

	ctx := context.Background()
	input := payload.Payloads{payload.New(100)} // 100 yields per execute

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pid := relay.PID{UniqID: fmt.Sprintf("test-%d", i)}
		sched.Submit(ctx, pid, &NYieldProcess{}, "", input)
	}

	// Wait for completion
	for completed.Load() < int64(b.N) {
	}
}
