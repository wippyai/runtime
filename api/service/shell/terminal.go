package shell

import (
	"context"
	"github.com/ponyruntime/pony/api/supervisor"
	"io"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
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
)

type (
	// Terminal is the base interface that all terminal implementations must satisfy
	Terminal interface {
		Run(ctx context.Context, in io.Reader, out io.Writer) error
		Close(ctx context.Context) error
	}

	// ServiceConfig represents the configuration for a terminal service
	ServiceConfig struct {
		HideLogs  bool                       `json:"hide_logs"` // Redirect logs (all) to the event bus, releases io.Output
		Lifecycle supervisor.LifecycleConfig `json:"lifecycle"` // Lifecycle management config
	}
)

// InitDefaults initializes the ServiceConfig with default values
func (c *ServiceConfig) InitDefaults() {
	c.Lifecycle.InitDefaults()
}
