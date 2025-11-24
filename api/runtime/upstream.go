package runtime

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
)

// Context keys for upstream configuration
var (
	// UpstreamSenderKey stores an UpstreamSender for fire-and-forget upstream messaging
	UpstreamSenderKey = &ctxapi.Key{Name: "upstream.sender"}

	// UpstreamHandlerKey stores an Upstream handler for workflow request queuing
	UpstreamHandlerKey = &ctxapi.Key{Name: "upstream.handler"}
)

// UpstreamSender is the interface for fire-and-forget upstream messaging
type UpstreamSender interface {
	Send(payload.Payload) error
}

// Upstream is the interface for workflow request queuing.
// Workflows collect Commands during execution and flush them after each step.
type Upstream interface {
	// SendRequest queues a command for later execution
	SendRequest(Command) error

	// FlushRequests returns all queued commands and clears the queue
	FlushRequests() []Command
}

// WithUpstreamSender attaches an upstream sender to the frame context
// for fire-and-forget upstream messaging.
// Returns error if frame context is not present.
func WithUpstreamSender(ctx context.Context, sender UpstreamSender) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctxapi.ErrNoFrameContext
	}

	return fc.Set(UpstreamSenderKey, sender)
}

// WithUpstream attaches an Upstream handler to the frame context
// for workflow request queuing.
// Returns error if frame context is not present.
func WithUpstream(ctx context.Context, handler Upstream) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctxapi.ErrNoFrameContext
	}

	return fc.Set(UpstreamHandlerKey, handler)
}

// GetUpstreamSender retrieves the upstream sender from context
func GetUpstreamSender(ctx context.Context) (UpstreamSender, bool) {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil, false
	}

	val, ok := fc.Get(UpstreamSenderKey)
	if !ok {
		return nil, false
	}

	sender, ok := val.(UpstreamSender)
	return sender, ok
}

// GetUpstream retrieves the upstream handler from context
func GetUpstream(ctx context.Context) (Upstream, bool) {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil, false
	}

	val, ok := fc.Get(UpstreamHandlerKey)
	if !ok {
		return nil, false
	}

	handler, ok := val.(Upstream)
	return handler, ok
}
