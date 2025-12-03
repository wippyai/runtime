// Package process provides process abstractions for schedulable execution.
package process

import (
	"context"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

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
type Payload = payload.Payload

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
	Process interface {
		// Init prepares the process for execution with method and input.
		Init(ctx context.Context, method string, input payload.Payloads) error
		// Step advances the process state machine by one iteration.
		Step(results *YieldResults) (StepResult, error)
		// Close releases process resources.
		Close()
		relay.Receiver
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
