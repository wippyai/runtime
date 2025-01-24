package terminal

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/api/service/terminal"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/tasks"
	"go.uber.org/zap"
)

// Factory is the default terminal factory.
type Factory struct{}

// NewFactory creates a new default terminal factory.
func NewFactory() *Factory {
	return &Factory{}
}

// MakeTerminal creates a new terminal instance.
func (f *Factory) MakeTerminal(
	log *zap.Logger,
	cfg api.TerminalConfig,
	modules api.ModuleRegistry,
	libraries api.LibraryRegistry,
) (terminal.Terminal, error) {
	opts := []engine.Option{
		// Always require the task and channel modules
		engine.WithPreloaded("tasks", tasks.NewTaskModule().Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
	}

	// Add user-configured modules
	for _, modName := range cfg.Modules {
		if modName == "tasks" || modName == "channel" {
			continue
		}

		mod, err := modules.Get(modName)
		if err != nil {
			return nil, err
		}
		opts = append(opts, engine.WithLoader(mod.Name(), mod.Loader))
	}

	// Add libraries
	for _, libName := range cfg.Libraries {
		lib, err := libraries.Get(registry.ID(libName))
		if err != nil {
			return nil, err
		}
		opts = append(opts, engine.WithLibrary(libName, lib.Source))
	}

	vm, err := engine.NewCVM(log, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create VM: %w", err)
	}

	// Import the main terminal function
	if err := vm.Import(cfg.Source, cfg.Method, cfg.Method); err != nil {
		vm.Close()
		return nil, fmt.Errorf("failed to import terminal code: %w", err)
	}

	// Create tasker with channel layer
	channels := channel.NewChannelLayer()
	tasker := tasks.NewTasker(
		log,
		vm,
		channels,
		1024, // Buffer size for task inbox
	)

	return NewLuaTerminal(log, tasker, Options{
		FuncName: cfg.Method,
		Args:     nil, // we have no state to start with, todo: support os args
	}), nil
}
