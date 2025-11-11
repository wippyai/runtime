package boot

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
)

// ServicePlugin is implemented by plugins that provide event handlers.
// Event handlers are collected after Load() and passed to the event router.
type ServicePlugin interface {
	// Handler returns the event handler for this service.
	// Called after Load() completes successfully.
	// Returns interface{} to avoid circular dependency with eventbus package.
	Handler() interface{}
}

// ServiceP defines a service plugin that provides an event handler.
type ServiceP struct {
	Name      string
	Phase     boot.Phase
	DependsOn []string
	Load      func(ctx context.Context) (context.Context, interface{}, error) // Returns (ctx, handler, error)
	Start     boot.StartFunc
	Stop      boot.StopFunc
}

type servicePlugin struct {
	name      string
	phase     boot.Phase
	deps      []string
	loadFunc  func(context.Context) (context.Context, interface{}, error)
	startFunc boot.StartFunc
	stopFunc  boot.StopFunc
	handler   interface{}
}

func (p *servicePlugin) Name() string         { return p.name }
func (p *servicePlugin) Phase() boot.Phase    { return p.phase }
func (p *servicePlugin) DependsOn() []string  { return p.deps }
func (p *servicePlugin) Handler() interface{} { return p.handler }

func (p *servicePlugin) Load(ctx context.Context) (context.Context, error) {
	if p.loadFunc == nil {
		return ctx, nil
	}
	newCtx, handler, err := p.loadFunc(ctx)
	if err != nil {
		return ctx, err
	}
	p.handler = handler
	return newCtx, nil
}

func (p *servicePlugin) Start(ctx context.Context) error {
	if p.startFunc == nil {
		return nil
	}
	return p.startFunc(ctx)
}

func (p *servicePlugin) Stop(ctx context.Context) error {
	if p.stopFunc == nil {
		return nil
	}
	return p.stopFunc(ctx)
}

// NewService creates a service plugin with event handler support.
func NewService(p ServiceP) boot.Plugin {
	return &servicePlugin{
		name:      p.Name,
		phase:     p.Phase,
		deps:      p.DependsOn,
		loadFunc:  p.Load,
		startFunc: p.Start,
		stopFunc:  p.Stop,
	}
}
