package terminal

import (
	"context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/supervisor"
	"io"
)

// System event constants for the terminal service.
// These constants define the event types and system identifiers used by the terminal system.
const (
	// System identifies the terminal system in the event context
	System events.System = "terminal"
	// RegisterTerminalEvent represents an event for registering a new terminal
	RegisterTerminalEvent events.Kind = "terminal.register"
	// DeleteTerminalEvent represents an event for removing a terminal
	DeleteTerminalEvent events.Kind = "terminal.delete"

	// Mouse behavior mode constants define different levels of mouse interaction support

	// MouseNone disables all mouse interaction support
	MouseNone string = "none"
	// MouseCell enables mouse interaction at the cell level
	MouseCell string = "cell"
	// MouseAll enables all mouse interactions
	MouseAll string = "all"
)

type (
	// Application represents a terminal application with its configuration.
	// It combines the terminal interface implementation with its options and lifecycle management.
	Application struct {
		Terminal  Terminal                   // The terminal implementation
		Options   Options                    `json:"options"`   // Terminal-specific options
		Lifecycle supervisor.LifecycleConfig `json:"lifecycle"` // Lifecycle management config
	}

	// Options contains terminal-specific settings
	Options struct {
		// UseAltScreen determines if the terminal should use the alternate screen buffer
		UseAltScreen bool `json:"alt_screen"`

		// Title sets the terminal window title
		Title string `json:"title,omitempty"`

		// MouseMode determines the type of mouse support
		MouseMode string `json:"mouse,omitempty"`

		// DisableSignals prevents handling of signals (ctrl+c, etc)
		DisableSignals bool `json:"disable_signals,omitempty"`
	}

	// Terminal is the base interface that all terminal implementations must satisfy
	Terminal interface {
		Run(ctx context.Context, in io.Reader, out io.Writer) error
	}
)
