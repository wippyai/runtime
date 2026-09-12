// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
)

// Retire permanently seals this participant's naming mutations, joins admitted
// mutations, closes its shared name guard, and calls withdraw before committing
// exact-incarnation retirement through the existing KV authority path.
//
// The trusted runtime lifecycle owner must supply withdrawal of every LOCAL and
// EVENTUAL registry sharing that guard. This is not an application admission API.
// Failure leaves any completed seals in place; an explicit retry can resolve an
// uncertain commit. Stop cancels and joins accepted retirement calls, so call
// Retire before Stop while KV and transport are still available.
//
// Success does not stop processes, drain remote tombstones, or authorize a fresh
// incarnation. The lifecycle owner must join the old endpoint and transport and
// separately establish replacement ownership. Disconnect is never that proof.
func (p *ParticipantEndpoint) Retire(ctx context.Context, withdraw func(context.Context) error) error {
	if ctx == nil || withdraw == nil {
		return errors.New("participant retirement requires context and withdrawal")
	}
	p.mu.Lock()
	if p.stopping || p.ctx.Err() != nil {
		p.mu.Unlock()
		return context.Canceled
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
	runCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(p.ctx, cancel)
	defer stop()
	defer cancel()
	return p.service.retireParticipant(runCtx, withdraw)
}
