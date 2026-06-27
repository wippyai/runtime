// SPDX-License-Identifier: MPL-2.0

package metrics

import (
	"context"

	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/pid"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/runtime"
)

const (
	ProcessStarted    = "wippy_process_started_total"
	ProcessTerminated = "wippy_process_terminated_total"
	ProcessActive     = "wippy_process_active"
)

// ProcessLifecycle is a process.Lifecycle handler that emits process
// supervision metrics. A nil collector makes every method a no-op.
type ProcessLifecycle struct {
	coll metrics.Collector
}

func NewProcessLifecycle(coll metrics.Collector) *ProcessLifecycle {
	p := &ProcessLifecycle{coll: coll}
	if coll != nil {
		coll.GaugeSet(ProcessActive, 0, metrics.Labels{})
		coll.CounterAdd(ProcessTerminated, 0, metrics.Labels{"result": "completed"})
		coll.CounterAdd(ProcessTerminated, 0, metrics.Labels{"result": "error"})
	}
	return p
}

func (p *ProcessLifecycle) OnStart(_ context.Context, _ pid.PID, _ processapi.Process) error {
	if p == nil || p.coll == nil {
		return nil
	}
	p.coll.CounterInc(ProcessStarted, metrics.Labels{})
	p.coll.GaugeInc(ProcessActive, metrics.Labels{})
	return nil
}

func (p *ProcessLifecycle) OnComplete(_ context.Context, _ pid.PID, result *runtime.Result) {
	if p == nil || p.coll == nil {
		return
	}
	p.coll.GaugeDec(ProcessActive, metrics.Labels{})
	outcome := "completed"
	if result != nil && result.Error != nil {
		outcome = "error"
	}
	p.coll.CounterInc(ProcessTerminated, metrics.Labels{"result": outcome})
}
