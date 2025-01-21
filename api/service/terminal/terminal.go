package terminal

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
	"io"
)

const (
	System        events.System = "terminal"
	RegisterEvent events.Kind   = "terminal.register"
	DeleteEvent   events.Kind   = "terminal.delete"

	MouseNone string = "none"
	MouseCell string = "cell"
	MouseAll  string = "all"
)

type (
	Registration struct {
		Terminal Terminal
		Config   Config
	}

	// Config represents terminal configuration
	Config struct {
		Meta      registry.Metadata          `json:"meta"`
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

// Validate checks if the terminal configuration is valid
func (c *Config) Validate() error {
	switch c.Options.MouseMode {
	case MouseNone, MouseCell, MouseAll, "":
		return nil
	default:
		return fmt.Errorf("invalid mouse mode: %s. Must be one of: none, cell, all", c.Options.MouseMode)
	}
}
