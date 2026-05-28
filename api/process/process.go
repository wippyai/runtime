// SPDX-License-Identifier: MPL-2.0

// Package process provides process abstractions for schedulable execution.
package process

import (
	"context"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
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

// Process option keys for process supervision.
const (
	ProcessParentKey  = "process.parent"
	ProcessMonitorKey = "process.monitor"
	ProcessLinkKey    = "process.link"
	ProcessNameKey    = "process.name"
)

type (
	// Meta contains metadata about a process type.
	Meta struct {
		Method string
	}

	// Start contains the configuration needed to start a new process.
	Start struct {
		HostID   pid.HostID
		Source   registry.ID
		Input    payload.Payloads
		Context  []ctxapi.Pair
		Options  attrs.Attributes
		Messages []*relay.Message // optional: initial messages for spawn-or-signal
	}

	// FactoryEntry is sent via event bus to register a factory.
	FactoryEntry struct {
		Factory FactoryFunc
		Meta    Meta
	}
)

type (
	// PIDGenerator creates PIDs for a host.
	PIDGenerator interface {
		Generate(host pid.HostID) pid.PID
	}

	// Process is a schedulable unit of work implemented as a state machine.
	// Scheduler passes events (yield completions + messages), process writes to output.
	// Ownership: scheduler owns queue and output, process is borrowed.
	//
	// Message delivery is handled by the scheduler/executor that owns the process,
	// not by the process itself. Process receives messages via Step(events).
	Process interface {
		// Init prepares the process for execution with method and input.
		Init(ctx context.Context, method string, input payload.Payloads) error
		// Step advances the process state machine with events that arrived.
		// events slice is owned by scheduler, process must not retain it.
		// out is scheduler-owned buffer, process writes yields and done status.
		Step(events []Event, out *StepOutput) error
		// Close releases process resources.
		Close()
	}

	// FactoryFunc creates new Process instances.
	FactoryFunc func() (Process, error)

	// Factory creates Process instances from registry IDs.
	Factory interface {
		Create(id registry.ID) (Process, *Meta, error)
	}

	// Lifecycle handles process lifecycle events for schedulers.
	Lifecycle interface {
		OnStart(ctx context.Context, p pid.PID, proc Process) error
		OnComplete(ctx context.Context, p pid.PID, result *runtime.Result)
	}

	// LifecycleRegistry manages multiple lifecycle handlers that are called
	// by schedulers during process lifecycle events.
	LifecycleRegistry interface {
		Lifecycle
		Register(name string, lc Lifecycle)
		Unregister(name string)
	}

	// Host is a unified interface for process execution environments.
	Host interface {
		relay.Receiver
		Run(ctx context.Context, start *Start) (pid.PID, error)
		Terminate(ctx context.Context, p pid.PID) error
	}

	// Manager defines the interface for process lifecycle management.
	Manager interface {
		Start(ctx context.Context, start *Start) (pid.PID, error)
		Cancel(ctx context.Context, from, target pid.PID, reason string) error
		Terminate(ctx context.Context, p pid.PID) error
	}

	// StatsProvider can be implemented by Process or Host to expose runtime statistics.
	StatsProvider interface {
		Stats() attrs.Attributes
	}
)
