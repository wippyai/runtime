// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

type binding struct {
	session *session
	port    *port
	mu      sync.Mutex
	closed  bool
}

func (b *binding) Resolve(ctx context.Context) (ttyapi.Port, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, ttyapi.ErrViewportClosed
	}
	if b.port != nil {
		return b.port, nil
	}
	target, ok := runtime.GetFramePID(ctx)
	if !ok {
		return nil, ttyapi.ErrInvalidGrant
	}
	router := relay.GetNode(ctx)
	if router == nil {
		return nil, ttyapi.ErrServiceUnavailable
	}
	ss := b.session
	ss.mu.Lock()
	if ss.closed || ss.producer {
		ss.mu.Unlock()
		return nil, ttyapi.ErrInvalidGrant
	}
	ss.producer, ss.target, ss.router = true, target, router
	if ss.bindings > 0 {
		ss.bindings--
	}
	ss.mu.Unlock()
	b.port = &port{session: ss, input: &input{session: ss}}
	return b.port, nil
}

func (b *binding) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	p, ss := b.port, b.session
	b.mu.Unlock()
	if p != nil {
		return p.Close()
	}
	ss.mu.Lock()
	if ss.bindings > 0 {
		ss.bindings--
	}
	ss.mu.Unlock()
	ss.service.collect(ss)
	return nil
}
