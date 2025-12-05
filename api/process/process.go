// Package process provides process abstractions for schedulable execution.
package process

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

// InboxKey is the context key for retrieving the process inbox from FrameContext.
var InboxKey = &ctxapi.Key{Name: "process.inbox", Inherit: false}

// GetInbox retrieves the Inbox from FrameContext.
func GetInbox(ctx context.Context) Inbox {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(InboxKey); ok {
		if inbox, ok := val.(Inbox); ok {
			return inbox
		}
	}
	return nil
}

// System identifies the process system in the event bus.
const System event.System = "process"

// Event kinds for factory operations.
const (
	FactoryRegister event.Kind = "factory.register"
	FactoryDelete   event.Kind = "factory.delete"
	FactoryAccept   event.Kind = "factory.accept"
	FactoryReject   event.Kind = "factory.reject"
)

// Registry kind for dispatcher handlers.
const KindHandler registry.Kind = "dispatcher.handler"

// Payload is an alias for payload.Payload used in process results.
type Payload = payload.Payload // todo: drop it

type (
	// Meta contains metadata about a process type.
	Meta struct {
		Method string
	}

	// Start contains the configuration needed to start a new process.
	Start struct {
		HostID  relay.HostID
		Source  registry.ID
		Input   payload.Payloads
		Context []ctxapi.Pair
		Options attrs.Attributes
	}

	// FactoryEntry is sent via event bus to register a factory.
	FactoryEntry struct {
		Factory NewFunc
		Meta    Meta
	}
)

type (
	// Process is a schedulable unit of work implemented as a state machine.
	//
	// Thread safety: Pool schedulers route messages through Executor.Send() which
	// writes directly to the Inbox. Process.Send() is only used by actor scheduler.
	Process interface {
		// Init prepares the process for execution with method and input.
		Init(ctx context.Context, method string, input payload.Payloads) error
		// Step advances the process state machine by one iteration.
		Step(results *YieldResults) (StepResult, error)
		// Close releases process resources.
		Close()
		relay.Receiver
	}

	// Inbox is a thread-safe message queue for process communication.
	Inbox interface {
		// QueueMessage adds a message to the inbox. Returns false if closed.
		QueueMessage(pkg *relay.Package) bool
		// Drain returns and clears all messages.
		Drain() []*relay.Package
	}

	// NewFunc creates new Process instances.
	NewFunc func() (Process, error)

	// Factory creates Process instances from registry IDs.
	Factory interface {
		Create(id registry.ID) (Process, *Meta, error)
	}

	// Lifecycle handles process lifecycle events for schedulers.
	Lifecycle interface {
		OnStart(ctx context.Context, pid relay.PID, proc Process)
		OnComplete(ctx context.Context, pid relay.PID, result *runtime.Result)
	}
)

const (
	// DefaultInboxCapacity is the default capacity for inbox message buffer.
	DefaultInboxCapacity = 16
	// MaxInboxCapacity limits the inbox buffer to prevent unbounded growth.
	MaxInboxCapacity = 1024
)

// MessageInbox is a bounded, thread-safe message queue for process communication.
// Can be embedded in Executor or created per-process for actor scheduler.
type MessageInbox struct {
	mu     sync.Mutex
	msgs   []*relay.Package
	closed atomic.Bool
}

// NewMessageInbox creates a MessageInbox with default capacity.
func NewMessageInbox() *MessageInbox {
	return &MessageInbox{
		msgs: make([]*relay.Package, 0, DefaultInboxCapacity),
	}
}

// Reset clears the inbox for reuse. Called between executions.
// Sets closed to false, clears messages.
func (ib *MessageInbox) Reset() {
	ib.mu.Lock()
	ib.closed.Store(false)
	// Keep capacity bounded
	if cap(ib.msgs) > MaxInboxCapacity {
		ib.msgs = make([]*relay.Package, 0, DefaultInboxCapacity)
	} else {
		ib.msgs = ib.msgs[:0]
	}
	ib.mu.Unlock()
}

// Close marks inbox as closed. QueueMessage will return false after this.
func (ib *MessageInbox) Close() {
	ib.mu.Lock()
	ib.closed.Store(true)
	ib.msgs = ib.msgs[:0]
	ib.mu.Unlock()
}

// QueueMessage adds a message to the inbox.
// Returns false if inbox is closed.
func (ib *MessageInbox) QueueMessage(pkg *relay.Package) bool {
	if ib.closed.Load() {
		return false
	}
	ib.mu.Lock()
	if ib.closed.Load() {
		ib.mu.Unlock()
		return false
	}
	ib.msgs = append(ib.msgs, pkg)
	ib.mu.Unlock()
	return true
}

// Drain returns and clears all messages.
func (ib *MessageInbox) Drain() []*relay.Package {
	ib.mu.Lock()
	if len(ib.msgs) == 0 {
		ib.mu.Unlock()
		return nil
	}
	msgs := ib.msgs
	ib.msgs = make([]*relay.Package, 0, DefaultInboxCapacity)
	ib.mu.Unlock()
	return msgs
}

// SetInbox stores an Inbox in the FrameContext.
func SetInbox(ctx context.Context, ib Inbox) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctxapi.ErrNoFrameContext
	}
	return fc.Set(InboxKey, ib)
}
