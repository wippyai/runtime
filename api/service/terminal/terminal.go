package terminal

import (
	"context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"io"
)

const (
	// System identifies the terminal system in the event context
	System events.System = "terminal"

	// KindTerminal identifies a terminal service component
	KindTerminal registry.Kind = "terminal.service"

	// RegisterTerminalEvent represents an event for registering a new terminal
	RegisterTerminalEvent events.Kind = "terminal.register"
	// DeleteTerminalEvent represents an event for removing a terminal
	DeleteTerminalEvent events.Kind = "terminal.delete"

	// Mouse behavior mode constants
	MouseNone string = "none"
	MouseCell string = "cell"
	MouseAll  string = "all"
)

type (
	// Terminal is the base interface that all terminal implementations must satisfy
	Terminal interface {
		Run(ctx context.Context, in io.Reader, out io.Writer) error
		Close(ctx context.Context) error
	}

	// DebugTerminal extends Terminal with debugging capabilities
	DebugTerminal interface {
		Terminal
		// Observe starts observing the terminal for debugging purposes, called before Run
		Observe(ctx context.Context, bus events.Bus) error
	}

	// StatefulTerminal extends Terminal with state management capabilities
	StatefulTerminal interface {
		Terminal
		// State returns the current terminal state
		State() payload.Payload
		// SetState attempts to restore terminal to given state, set prior to Run
		SetState(payload.Payload) error
	}
)
