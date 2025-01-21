package terminal

import (
	"context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/supervisor"
	"io"
)

const (
	System                events.System = "terminal"
	RegisterTerminalEvent events.Kind   = "terminal.register"
	DeleteTerminalEvent   events.Kind   = "terminal.delete"

	// Mouse behavior modes
	MouseNone string = "none"
	MouseCell string = "cell"
	MouseAll  string = "all"
)

type (
	RegisterApplication struct {
		Terminal  Terminal
		Options   Options                    `json:"options"`
		Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
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
