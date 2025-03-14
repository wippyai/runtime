package terminal

import (
	"context"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/service/terminal"
	"github.com/ponyruntime/pony/system/logs"
	"go.uber.org/zap"
)

// RunnerFactory defines an interface for creating terminal runners
type RunnerFactory interface {
	// CreateRunner creates a new runner based on the given context and launch parameters
	CreateRunner(ctx context.Context, cfg *RunnerConfig, launch *process.Launch) (*Runner, error)
}

// DefaultRunnerFactory is the standard implementation of RunnerFactory
type DefaultRunnerFactory struct{}

// CreateRunner implements the RunnerFactory interface
func (f *DefaultRunnerFactory) CreateRunner(ctx context.Context, cfg *RunnerConfig, launch *process.Launch) (*Runner, error) {
	return NewTerminalRunner(ctx, cfg, launch)
}

// ServiceFactory is an interface for creating terminal service instances
type ServiceFactory interface {
	// CreateTerminal creates a new terminal service instance with the given configuration
	CreateTerminal(id registry.ID, config *api.HostConfig) *Terminal
}

// DefaultServiceFactory is the standard implementation of ServiceFactory
// that creates Terminal instances with default behavior
type DefaultServiceFactory struct {
	bus           event.Bus
	log           *zap.Logger
	runnerFactory RunnerFactory
}

// NewDefaultServiceFactory creates a new DefaultServiceFactory with default runner factory
func NewDefaultServiceFactory(bus event.Bus, log *zap.Logger) *DefaultServiceFactory {
	return &DefaultServiceFactory{
		bus:           bus,
		log:           log,
		runnerFactory: &DefaultRunnerFactory{},
	}
}

// NewDefaultServiceFactoryWithRunnerFactory creates a new DefaultServiceFactory with custom runner factory
func NewDefaultServiceFactoryWithRunnerFactory(bus event.Bus, log *zap.Logger, runnerFactory RunnerFactory) *DefaultServiceFactory {
	return &DefaultServiceFactory{
		bus:           bus,
		log:           log,
		runnerFactory: runnerFactory,
	}
}

// CreateTerminal implements ServiceFactory interface
// Creates a terminal instance with the provided configuration
func (f *DefaultServiceFactory) CreateTerminal(id registry.ID, config *api.HostConfig) *Terminal {
	// Create log switcher internally
	logSwitcher := logs.NewConfigSwitcher(f.bus, f.log)

	// Create terminal with the created log switcher and pass the runner factory
	return NewTerminalHost(
		id,
		config,
		logSwitcher,
		f.log.With(zap.String("terminalID", id.String())),
		f.runnerFactory,
	)
}
