package worksim

import (
	"context"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
)

// Process implements process.Process using a shared Workload.
// Multiple Process instances share the same Workload to simulate
// contention and resource limits.
type Process struct {
	workload *Workload
}

// NewProcess creates a process that uses the given workload.
func NewProcess(w *Workload) *Process {
	return &Process{workload: w}
}

// NewFactory returns a factory function for creating processes with shared workload.
func NewFactory(w *Workload) process.FactoryFunc {
	return func() (process.Process, error) {
		return NewProcess(w), nil
	}
}

func (p *Process) Init(_ctx context.Context, _method string, _input payload.Payloads) error {
	return nil
}

func (p *Process) Step(_events []process.Event, out *process.StepOutput) error {
	// Use background context for work - the pool executor manages cancellation
	// We simulate work here, then immediately complete
	ctx := context.Background()

	if err := p.workload.Work(ctx); err != nil {
		out.Done(nil)
		return nil
	}

	out.Done(nil)
	return nil
}

func (p *Process) Close() {}
