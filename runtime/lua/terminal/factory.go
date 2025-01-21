package terminal

import (
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/api/service/terminal"
	terminal2 "github.com/ponyruntime/pony/service/terminal"
)

// DefaultFactory is the default terminal factory.
type DefaultFactory struct {
}

// NewDefaultFactory creates a new default terminal factory.
func NewDefaultFactory() *DefaultFactory {
	return &DefaultFactory{}
}

// MakeTerminal creates a new terminal instance.
func (f *DefaultFactory) MakeTerminal(
	cfg api.TerminalConfig,
	modules api.ModuleRegistry,
	libraries api.LibraryRegistry,
) (terminal.Terminal, error) {
	return terminal2.NewEchoTerminal("p"), nil
}
