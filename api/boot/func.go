package boot

import "context"

// LoadFunc is the function signature for plugin Load.
type LoadFunc func(context.Context) (context.Context, error)

// StartFunc is the function signature for plugin Start.
type StartFunc func(context.Context) error

// StopFunc is the function signature for plugin Stop.
type StopFunc func(context.Context) error

// P defines a functional plugin using callbacks.
type P struct {
	Name      string
	Phase     Phase
	DependsOn []string
	Load      LoadFunc
	Start     StartFunc
	Stop      StopFunc
}

// funcPlugin implements Plugin using function callbacks.
type funcPlugin struct {
	name      string
	phase     Phase
	deps      []string
	loadFunc  LoadFunc
	startFunc StartFunc
	stopFunc  StopFunc
}

func (p *funcPlugin) Name() string        { return p.name }
func (p *funcPlugin) Phase() Phase        { return p.phase }
func (p *funcPlugin) DependsOn() []string { return p.deps }

func (p *funcPlugin) Load(ctx context.Context) (context.Context, error) {
	if p.loadFunc == nil {
		return ctx, nil
	}
	return p.loadFunc(ctx)
}

func (p *funcPlugin) Start(ctx context.Context) error {
	if p.startFunc == nil {
		return nil
	}
	return p.startFunc(ctx)
}

func (p *funcPlugin) Stop(ctx context.Context) error {
	if p.stopFunc == nil {
		return nil
	}
	return p.stopFunc(ctx)
}

// New creates a functional plugin.
func New(p P) Plugin {
	return &funcPlugin{
		name:      p.Name,
		phase:     p.Phase,
		deps:      p.DependsOn,
		loadFunc:  p.Load,
		startFunc: p.Start,
		stopFunc:  p.Stop,
	}
}
