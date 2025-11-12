package boot

import "context"

// LoadFunc is the function signature for component Load.
type LoadFunc func(context.Context) (context.Context, error)

// StartFunc is the function signature for component Start.
type StartFunc func(context.Context) error

// StopFunc is the function signature for component Stop.
type StopFunc func(context.Context) error

// P defines a functional component using callbacks.
type P struct {
	Name      string
	Phase     Phase
	DependsOn []string
	Load      LoadFunc
	Start     StartFunc
	Stop      StopFunc
}

// funcComponent implements Component using function callbacks.
type funcComponent struct {
	name      string
	phase     Phase
	deps      []string
	loadFunc  LoadFunc
	startFunc StartFunc
	stopFunc  StopFunc
}

func (p *funcComponent) Name() string        { return p.name }
func (p *funcComponent) Phase() Phase        { return p.phase }
func (p *funcComponent) DependsOn() []string { return p.deps }

func (p *funcComponent) Load(ctx context.Context) (context.Context, error) {
	if p.loadFunc == nil {
		return ctx, nil
	}
	return p.loadFunc(ctx)
}

func (p *funcComponent) Start(ctx context.Context) error {
	if p.startFunc == nil {
		return nil
	}
	return p.startFunc(ctx)
}

func (p *funcComponent) Stop(ctx context.Context) error {
	if p.stopFunc == nil {
		return nil
	}
	return p.stopFunc(ctx)
}

// New creates a functional component.
func New(p P) Component {
	return &funcComponent{
		name:      p.Name,
		phase:     p.Phase,
		deps:      p.DependsOn,
		loadFunc:  p.Load,
		startFunc: p.Start,
		stopFunc:  p.Stop,
	}
}
