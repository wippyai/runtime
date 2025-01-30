package workflow

import (
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/command"
	"github.com/ponyruntime/pony/runtime/lua/engine/pubsub"
	"github.com/ponyruntime/pony/runtime/lua/modules/time"
	"go.uber.org/zap"
)

// Factory creates workflow runners
type Factory struct{}

// NewFactory creates a new workflow factory
func NewFactory() *Factory {
	return &Factory{}
}

// ForWorkflow creates a new workflow runner instance
func (f *Factory) ForWorkflow(
	log *zap.Logger,
	cfg *api.WorkflowConfig,
	modules api.ModuleRegistry,
	libraries api.LibraryRegistry,
) (func() any, error) {

	// Collect engine options
	opts := []engine.Option{
		// todo: redefine time functions!
		engine.WithPreloaded("time", time.NewTimeModule().Loader),
		engine.WithPreloaded("command", command.NewCommandModule().Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
	}

	// Track registered modules to avoid duplicates
	knownModules := map[string]struct{}{
		"command": {},
		"channel": {},
		"time":    {},
	}

	// Add libraries and their dependencies
	for _, libName := range cfg.Libraries {
		lib, err := libraries.Get(registry.ID(libName))
		if err != nil {
			return nil, err
		}

		// Add library module dependencies
		allowed := false
		for _, modID := range lib.Modules {
			module, err := modules.Get(modID)
			if err != nil {
				continue
			}

			if _, exists := knownModules[module.Name()]; !exists {
				continue
			}

			allowed = true
		}

		if allowed || len(lib.Modules) == 0 {
			opts = append(opts, engine.WithLibrary(libName, lib.Source))
		}
	}

	// todo: we can pre-compile it here

	return func() any {
		// Create VM with configured modules
		vm, err := engine.NewCVM(log, opts...)
		if err != nil {
			log.Warn("failed to create VM", zap.Error(err))
			return nil
		}

		// Import the workflow function
		if err := vm.Import(cfg.Source, cfg.Method, cfg.Method); err != nil {
			log.Warn("failed to import workflow", zap.Error(err))
			vm.Close()
			return nil
		}

		// Create required layers
		channels := channel.NewChannelLayer()
		cmdLayer := command.NewCommandLayer(channels)
		pubLayer := pubsub.NewSubscriptionLayer(channels)

		// Create engine runner with all layers
		engineRunner := engine.NewRunner(vm,
			engine.WithLayer(channels),
			engine.WithLayer(cmdLayer),
			engine.WithLayer(pubLayer),
		)

		// Create and return workflow runner
		return NewWorkflowRunner(engineRunner, cmdLayer, pubLayer)
	}, nil
}
