package process

import (
	"github.com/wippyai/runtime/api/dispatcher"
)

// Type aliases for backwards compatibility - use dispatcher package in new code.
type (
	CommandID      = dispatcher.CommandID
	Command        = dispatcher.Command
	ResultReceiver = dispatcher.ResultReceiver
	Handler        = dispatcher.Handler
	Dispatcher     = dispatcher.Dispatcher
	Registry       = dispatcher.Registry
	Registrar      = dispatcher.Registrar
	Freezer        = dispatcher.Freezer
	HandlerFunc    = dispatcher.HandlerFunc
)

// Re-export constants and functions for backwards compatibility.
const KindHandler = dispatcher.KindHandler

func MustRegisterCommands(module string, ids ...CommandID) {
	dispatcher.MustRegisterCommands(module, ids...)
}
