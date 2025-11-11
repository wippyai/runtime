package boot

import (
	"context"
	"fmt"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/internal/graph"
	"github.com/ponyruntime/pony/system/eventbus"
)

// Loader orchestrates plugin loading with dependency resolution.
type Loader struct {
	plugins map[string]boot.Plugin
	graph   *graph.Graph[string, boot.Plugin]
	loaded  []boot.Plugin
}

// NewLoader creates a new plugin loader.
func NewLoader() *Loader {
	return &Loader{
		plugins: make(map[string]boot.Plugin),
		graph:   graph.New[string, boot.Plugin](),
		loaded:  make([]boot.Plugin, 0),
	}
}

// Register adds a plugin to the loader.
func (l *Loader) Register(p boot.Plugin) error {
	name := p.Name()
	if _, exists := l.plugins[name]; exists {
		return fmt.Errorf("plugin %q already registered", name)
	}

	l.plugins[name] = p
	l.graph.AddNode(name)

	for _, dep := range p.DependsOn() {
		l.graph.AddEdge(dep, name, 1, p)
	}

	return nil
}

// Load executes all plugins Load() in dependency order.
func (l *Loader) Load(ctx context.Context) (context.Context, error) {
	levels, err := l.graph.DependencyLevels()
	if err != nil {
		return ctx, fmt.Errorf("dependency resolution: %w", err)
	}

	for i := 0; i < levels.LevelCount(); i++ {
		names, _ := levels.GetLevel(i)

		for _, name := range names {
			p, ok := l.plugins[name]
			if !ok {
				return ctx, fmt.Errorf("plugin %q not found", name)
			}

			ctx, err = p.Load(ctx)
			if err != nil {
				return ctx, fmt.Errorf("plugin %q load: %w", name, err)
			}

			l.loaded = append(l.loaded, p)
		}
	}

	return ctx, nil
}

// Start activates all plugins with Start() method in dependency order.
func (l *Loader) Start(ctx context.Context) error {
	for _, p := range l.loaded {
		if starter, ok := p.(boot.Starter); ok {
			if err := starter.Start(ctx); err != nil {
				return fmt.Errorf("plugin %q start: %w", p.Name(), err)
			}
		}
	}

	return nil
}

// Shutdown stops all plugins in reverse order.
func (l *Loader) Shutdown(ctx context.Context) error {
	for i := len(l.loaded) - 1; i >= 0; i-- {
		p := l.loaded[i]

		if stopper, ok := p.(boot.Stopper); ok {
			if err := stopper.Stop(ctx); err != nil {
				return fmt.Errorf("plugin %q stop: %w", p.Name(), err)
			}
		}
	}

	return nil
}

// Handlers collects event handlers from all loaded service plugins.
func (l *Loader) Handlers() []eventbus.EventHandler {
	var handlers []eventbus.EventHandler

	for _, p := range l.loaded {
		if sp, ok := p.(ServicePlugin); ok {
			if h := sp.Handler(); h != nil {
				if handler, ok := h.(eventbus.EventHandler); ok {
					handlers = append(handlers, handler)
				}
			}
		}
	}

	return handlers
}
