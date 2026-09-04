// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"context"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
)

const OptionTerminal = "terminal"

type Snapshot struct {
	// Cursor is nil until a producer first publishes explicit cursor state.
	// Row-only frames preserve the last explicit value.
	Cursor *Cursor
	// Rows is immutable and remains valid after later presents. Consumers must
	// not modify it. This lets unchanged UI frames inspect snapshots without an
	// allocation or copy.
	Rows     []string
	Revision uint64
	Width    int
	Height   int
}

// Update announces that a newer snapshot may be read. Notifications are
// coalesced: consumers must treat Revision as a watermark and read Snapshot
// for state, rather than assuming that every intermediate revision is sent.
type Update struct {
	Revision uint64
}

type Viewport interface {
	Grant() string
	Handle() string
	Snapshot() Snapshot
	// Updates is closed when this viewport is detached. The returned channel is
	// bounded and emits only state-change hints; a slow consumer never blocks a
	// producer presenting a frame.
	Updates() <-chan Update
	Send(Event) error
	Resize(width, height int) error
	// Close detaches this consumer. It never terminates the producer process.
	Close() error
}

type Service interface {
	Create(context.Context, int, int) (Viewport, error)
	Attach(context.Context, string) (Viewport, error)
	Binding(grant string) (Binding, error)
	Close() error
}

var serviceKey = &ctxapi.Key{Name: "tty.service"}

func WithService(ctx context.Context, service Service) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac != nil && ac.Get(serviceKey) == nil {
		ac.With(serviceKey, service)
	}
	return ctx
}

func GetService(ctx context.Context) Service {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	service, _ := ac.Get(serviceKey).(Service)
	return service
}

func ResolveTerminalOption(ctx context.Context, options attrs.Attributes) ([]ctxapi.Pair, error) {
	if options == nil {
		return nil, nil
	}
	grant := options.GetString(OptionTerminal, "")
	if grant == "" {
		return nil, nil
	}
	service := GetService(ctx)
	if service == nil {
		return nil, ErrServiceUnavailable
	}
	binding, err := service.Binding(grant)
	if err != nil {
		return nil, err
	}
	return []ctxapi.Pair{BindingPair(binding)}, nil
}
