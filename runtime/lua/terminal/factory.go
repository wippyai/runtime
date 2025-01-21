package terminal

import (
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/api/service/terminal"
	terminal2 "github.com/ponyruntime/pony/service/terminal"
)

type defaultFactory struct {
}

func NewDefaultFactory() *defaultFactory {
	return &defaultFactory{}
}

func (f *defaultFactory) MakeTerminal(
	cfg api.TerminalConfig,
	modules api.ModuleRegistry,
	libraries api.LibraryRegistry,
) (terminal.Terminal, error) {
	return terminal2.NewEchoTerminal("p"), nil
}
