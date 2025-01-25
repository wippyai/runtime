package terminal

import (
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/api/service/terminal"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/async"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/lua/modules/upstream"
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
	up := make(chan any, 1024)
	opts := []engine.Option{
		engine.WithPreloaded("upstream", upstream.NewUpstreamModule(up).Loader),
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
		opts = append(opts, engine.WithPreloaded(mod.Name(), mod.Loader))
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

	// Create channel layer
	channels := channel.NewChannelLayer()

	// async layer, example: time.after():receive()
	asyncLayer := async.NewAsyncLayer(channels, 4096)

	// coroutine executor, example: time.sleep
	coroutineLayer := coroutine.NewCoroutineLayer()

	// Create runner with all layers
	// Order: coroutine -> async -> channel -> base VM
	runner := tasks.NewTaskRunner(
		log, vm, channels, 1024,
		engine.WithLayer(coroutineLayer),
		engine.WithLayer(asyncLayer),
	)

	return NewLuaTerminal(log, runner, Options{
		FuncName: cfg.Method,
		Args:     nil, // we have no state to start with, todo: support os args
	}, up), nil
}
