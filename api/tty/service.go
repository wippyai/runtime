// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"context"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
)

const (
	OptionTerminal        = "terminal"
	FrameResolverClaimTTY = "terminal"
	MaxViewportDimension  = 65535
	MaxViewportCells      = 1 << 18
)

// ValidateViewportSize bounds geometry accepted by the in-memory broker and
// Lua-facing viewport API.
func ValidateViewportSize(width, height int) error {
	if width < 1 || width > MaxViewportDimension || height < 1 || height > MaxViewportDimension ||
		height > MaxViewportCells/width {
		return ErrInvalidViewportSize
	}
	return nil
}

// TerminalOptionSelected reports whether a process requested a virtual
// terminal attachment. The boot composition root registers this claim before
// optional TTY services are wired, so omitted services fail closed.
func TerminalOptionSelected(_ context.Context, options attrs.Attributes) bool {
	return options != nil && options.GetString(OptionTerminal, "") != ""
}

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
	if !TerminalOptionSelected(ctx, options) {
		return nil, nil
	}
	grant := options.GetString(OptionTerminal, "")
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
