package pool

import (
	"context"

	"github.com/wippyai/runtime/api/process"
)

// Inbox is an alias for process.MessageInbox for pool-local use.
type Inbox = process.MessageInbox

// NewInbox creates an Inbox with default capacity.
func NewInbox() *Inbox {
	return process.NewMessageInbox()
}

// SetInbox stores an Inbox in the FrameContext.
func SetInbox(ctx context.Context, ib *Inbox) error {
	return process.SetInbox(ctx, ib)
}

// GetInbox retrieves an Inbox from the FrameContext.
func GetInbox(ctx context.Context) *Inbox {
	inbox := process.GetInbox(ctx)
	if inbox == nil {
		return nil
	}
	if ib, ok := inbox.(*Inbox); ok {
		return ib
	}
	return nil
}
