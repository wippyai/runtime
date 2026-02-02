// Package boot provides application boot and component loading.
package boot

import (
	"context"
	"io/fs"

	contextapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
)

type (
	// Loader provides filesystem loading and entry extraction.
	// This is an interface to avoid circular dependencies between api and boot/loader.
	Loader interface {
		// LoadFS loads all entries from a filesystem recursively.
		LoadFS(ctx context.Context, filesystem fs.FS) ([]registry.Entry, error)

		// LoadDir loads entries from a specific directory.
		LoadDir(ctx context.Context, filesystem fs.FS, dirPath string) ([]registry.Entry, error)

		// LoadFile loads entries from a single file.
		LoadFile(ctx context.Context, filesystem fs.FS, filePath string) ([]registry.Entry, error)
	}

	// LoadFunc is the function signature for component Load.
	LoadFunc func(context.Context) (context.Context, error)

	// StartFunc is the function signature for component Start.
	StartFunc func(context.Context) error

	// StopFunc is the function signature for component Stop.
	StopFunc func(context.Context) error

	// P defines a functional component using callbacks.
	P struct {
		Load      LoadFunc
		Start     StartFunc
		Stop      StopFunc
		Name      string
		DependsOn []string
	}

	// funcComponent implements Component using function callbacks.
	funcComponent struct {
		loadFunc  LoadFunc
		startFunc StartFunc
		stopFunc  StopFunc
		name      string
		deps      []string
	}

	// loaderKey is the context key for the loader component.
	loaderKey struct{}
)

func (p *funcComponent) Name() string { return p.name }
func (p *funcComponent) DependsOn() []string {
	return p.deps
}

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
		deps:      p.DependsOn,
		loadFunc:  p.Load,
		startFunc: p.Start,
		stopFunc:  p.Stop,
	}
}

// WithLoader attaches Loader to AppContext.
func WithLoader(ctx context.Context, ldr Loader) context.Context {
	ac := contextapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(loaderKey{}) == nil {
		ac.With(loaderKey{}, ldr)
	}
	return ctx
}

// GetLoader retrieves Loader from AppContext.
// Returns nil if no Loader is found.
func GetLoader(ctx context.Context) Loader {
	ac := contextapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if ldr, ok := ac.Get(loaderKey{}).(Loader); ok {
		return ldr
	}
	return nil
}
