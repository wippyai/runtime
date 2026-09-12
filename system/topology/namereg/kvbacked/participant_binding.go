// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

// BindLocal captures this one-shot endpoint, never a replacement registry at
// the same sysreg address. Admission joins the endpoint's existing shutdown gate.
func (p *ParticipantEndpoint) BindLocal(target pid.PID) (relay.ContextSender, error) {
	if !target.Equal(p.service.self) {
		return nil, relay.ErrBindingTarget
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.started || p.stopping || p.ctx.Err() != nil {
		return nil, relay.ErrBindingRetired
	}
	return &boundParticipant{endpoint: p, target: target}, nil
}

type boundParticipant struct {
	endpoint *ParticipantEndpoint
	target   pid.PID
}

func (b *boundParticipant) SendContext(ctx context.Context, pkg *relay.Package) error {
	if pkg == nil || !pkg.Target.Equal(b.target) {
		return relay.ErrBindingTarget
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p := b.endpoint
	p.mu.Lock()
	if p.stopping || p.ctx.Err() != nil {
		p.mu.Unlock()
		return relay.ErrBindingRetired
	}
	p.operations++
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.operations--
		if p.stopping && p.operations == 0 {
			close(p.done)
		}
		p.mu.Unlock()
	}()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(p.ctx, cancel)
	defer cancel()
	defer stop()
	return p.host.SendContext(ctx, pkg)
}

var _ relay.LocalBinder = (*ParticipantEndpoint)(nil)
