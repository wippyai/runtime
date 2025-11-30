package boot

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	contextapi "github.com/wippyai/runtime/api/context"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/internal/graph"
	"github.com/wippyai/runtime/system/eventbus"
)

// Loader orchestrates component loading with dependency resolution.
type Loader struct {
	components map[string]boot.Component
	graph      *graph.Graph[string, boot.Component]
	loaded     []boot.Component
	ctx        context.Context
}

// NewLoader creates a new component loader.
// Infrastructure (AppContext, logger, EventBus) must be initialized via NewInfrastructure before calling Load.
// If components are provided, they will be registered automatically.
func NewLoader(components ...boot.Component) (*Loader, error) {
	l := &Loader{
		components: make(map[string]boot.Component),
		graph:      graph.New[string, boot.Component](),
		loaded:     make([]boot.Component, 0),
	}

	for _, c := range components {
		if c != nil {
			if err := l.Register(c); err != nil {
				return nil, err
			}
		}
	}

	return l, nil
}

// Register adds a component to the loader.
func (l *Loader) Register(c boot.Component) error {
	name := c.Name()
	if _, exists := l.components[name]; exists {
		return fmt.Errorf("component %q already registered", name)
	}

	l.components[name] = c
	l.graph.AddNode(name)

	for _, dep := range c.DependsOn() {
		l.graph.AddEdge(dep, name, 1, c)
	}

	return nil
}

// Load executes all components Load() in dependency order.
// Infrastructure (AppContext, logger, EventBus) must already be initialized in context.
// Config should be attached to context via boot.WithConfig() before calling Load.
func (l *Loader) Load(ctx context.Context) (context.Context, error) {
	// Verify AppContext and logger are present
	if contextapi.AppFromContext(ctx) == nil {
		return ctx, fmt.Errorf("AppContext not initialized - call NewInfrastructure first")
	}
	if logapi.GetLogger(ctx) == nil {
		return ctx, fmt.Errorf("logger not initialized - call NewInfrastructure first")
	}

	levels, err := l.graph.DependencyLevels()
	if err != nil {
		return ctx, fmt.Errorf("dependency resolution: %w", err)
	}

	for i := 0; i < levels.LevelCount(); i++ {
		names, _ := levels.GetLevel(i)

		for _, name := range names {
			c, ok := l.components[name]
			if !ok {
				return ctx, fmt.Errorf("component %q not found", name)
			}

			ctx, err = c.Load(ctx)
			if err != nil {
				return ctx, fmt.Errorf("component %q load: %w", name, err)
			}

			l.loaded = append(l.loaded, c)
		}
	}

	l.ctx = ctx
	return ctx, nil
}

// Start activates runtime services and all components with Start() method in dependency order.
func (l *Loader) Start(ctx context.Context) error {
	if err := StartRuntimeServices(ctx); err != nil {
		return fmt.Errorf("start runtime services: %w", err)
	}

	// Freeze dispatcher registry for lock-free lookups
	// All handlers were registered during Load() phase
	if reg := dispatcherapi.GetRegistry(ctx); reg != nil {
		if freezer, ok := reg.(dispatcherapi.Freezer); ok {
			freezer.Freeze()
		}
	}

	for _, c := range l.loaded {
		if starter, ok := c.(boot.Starter); ok {
			if err := starter.Start(ctx); err != nil {
				return fmt.Errorf("component %q start: %w", c.Name(), err)
			}
		}
	}

	return nil
}

// Shutdown stops all components in reverse order.
func (l *Loader) Shutdown(ctx context.Context) error {
	var errors []error

	for i := len(l.loaded) - 1; i >= 0; i-- {
		c := l.loaded[i]

		if stopper, ok := c.(boot.Stopper); ok {
			if err := stopper.Stop(ctx); err != nil {
				errors = append(errors, fmt.Errorf("component %q stop: %w", c.Name(), err))
			}
		}
	}

	if len(errors) == 0 {
		return nil
	}

	if len(errors) == 1 {
		return errors[0]
	}

	return fmt.Errorf("shutdown errors (%d components): %w", len(errors), errors[0])
}

// Handlers returns all handlers registered via HandlerRegistry during component loading.
func (l *Loader) Handlers() []eventbus.EventHandler {
	if l.ctx == nil {
		return []eventbus.EventHandler{}
	}

	handlerReg := GetHandlerRegistry(l.ctx)
	if handlerReg == nil {
		return []eventbus.EventHandler{}
	}

	return handlerReg.Handlers()
}
