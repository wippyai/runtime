// SPDX-License-Identifier: MPL-2.0
package relay

import (
	"context"

	"github.com/wippyai/runtime/api/pid"
	api "github.com/wippyai/runtime/api/relay"
)

func (m *Mailbox) BindLocal(target pid.PID) (api.ContextSender, error) {
	m.lifecycle.RLock()
	defer m.lifecycle.RUnlock()
	if m.closed || m.ctx.Err() != nil {
		return nil, api.ErrBindingRetired
	}
	value, ok := m.receivers.Load(target.String())
	if !ok {
		return nil, api.ErrBindingRetired
	}
	receiver, ok := value.(*mailboxReceiver)
	if !ok || receiver == nil {
		return nil, api.ErrBindingRetired
	}
	return &boundMailbox{mailbox: m, receiver: receiver, target: target}, nil
}

type boundMailbox struct {
	mailbox  *Mailbox
	receiver *mailboxReceiver
	target   pid.PID
}

func (b *boundMailbox) SendContext(ctx context.Context, pkg *api.Package) error {
	if pkg == nil {
		return NewNilPackageError()
	}
	if !pkg.Target.Equal(b.target) {
		return api.ErrBindingTarget
	}
	return b.mailbox.sendContext(ctx, pkg, b.receiver)
}

var _ api.LocalBinder = (*Mailbox)(nil)
