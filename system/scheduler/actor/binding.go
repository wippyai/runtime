// SPDX-License-Identifier: MPL-2.0
package actor

import (
	"context"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
)

// BindLocal captures the immutable signal reference and its queue generation.
// Keeping a Processor pointer alone would permit pool reuse to change the target.
func (s *Scheduler) BindLocal(target pid.PID) (relay.ContextSender, error) {
	value, ok := s.byPID.Load(target.String())
	if !ok {
		return nil, process.ErrProcessNotFound
	}
	proc := value.(*Processor)
	ref := proc.sig.Load()
	if ref == nil || !ref.pid.Equal(target) {
		return nil, process.ErrProcessClosed
	}
	return &boundProcessor{scheduler: s, processor: proc, target: target, generation: ref.gen}, nil
}

type boundProcessor struct {
	scheduler  *Scheduler
	processor  *Processor
	target     pid.PID
	generation uint64
}

func (b *boundProcessor) SendContext(ctx context.Context, pkg *relay.Package) error {
	if pkg == nil {
		return errNilPackage
	}
	if !pkg.Target.Equal(b.target) {
		return relay.ErrBindingTarget
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return b.scheduler.deliverToProcError(b.processor, b.generation, pkg)
}

var _ relay.LocalBinder = (*Scheduler)(nil)
