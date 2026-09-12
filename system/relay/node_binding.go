// SPDX-License-Identifier: MPL-2.0
package relay

import (
	"context"

	"github.com/wippyai/runtime/api/pid"
	api "github.com/wippyai/runtime/api/relay"
)

func (n *Node) BindLocal(target pid.PID) (api.ContextSender, error) {
	if target.Node != "" && target.Node != n.nodeID {
		return nil, NewExternalNodeError(target.Node)
	}
	value, ok := n.hosts.Load(target.Host)
	if !ok {
		return nil, api.ErrBindingRetired
	}
	registration, ok := value.(*hostRegistration)
	if !ok || !registration.acquire() {
		return nil, api.ErrBindingRetired
	}
	defer registration.releaseAdmission()
	binder, ok := registration.receiver.(api.LocalBinder)
	if !ok {
		return nil, api.ErrBindingUnsupported
	}
	sender, err := binder.BindLocal(target)
	if err != nil {
		return nil, err
	}
	if sender == nil {
		return nil, api.ErrBindingUnsupported
	}
	if registration.retired.Load() {
		return nil, api.ErrBindingRetired
	}
	return &boundHost{registration: registration, sender: sender, target: target}, nil
}

type boundHost struct {
	registration *hostRegistration
	sender       api.ContextSender
	target       pid.PID
}

func (b *boundHost) SendContext(ctx context.Context, pkg *api.Package) error {
	if pkg == nil {
		return NewNilPackageError()
	}
	if !pkg.Target.Equal(b.target) {
		return api.ErrBindingTarget
	}
	return b.registration.withContext(ctx, func(ctx context.Context) error {
		return b.sender.SendContext(ctx, pkg)
	})
}

// BindLocal never resolves a remote peer or internode fallback.
func (r *Router) BindLocal(target pid.PID) (api.ContextSender, error) {
	if target.Node != "" && target.Node != r.localNode.ID() {
		return nil, NewExternalNodeError(target.Node)
	}
	binder, ok := r.localNode.(api.LocalBinder)
	if !ok {
		return nil, api.ErrBindingUnsupported
	}
	return binder.BindLocal(target)
}

var _ api.LocalBinder = (*Node)(nil)
var _ api.LocalBinder = (*Router)(nil)
