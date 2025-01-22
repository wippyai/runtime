package terminal

import (
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/api/service/terminal"
	"go.uber.org/zap"
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
	log *zap.Logger,
	cfg api.TerminalConfig,
	modules api.ModuleRegistry,
	libraries api.LibraryRegistry,
) (terminal.Terminal, error) {
	return NewLuaTerminal(log), nil
}
