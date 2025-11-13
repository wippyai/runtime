// Package boot provides application boot and component loading.
package boot

import (
	"context"
	"io/fs"

	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/registry"
)

// Loader provides filesystem loading and entry extraction.
// This is an interface to avoid circular dependencies between api and boot/loader.
type Loader interface {
	// LoadFS loads all entries from a filesystem recursively.
	LoadFS(ctx context.Context, filesystem fs.FS) ([]registry.Entry, error)

	// LoadDir loads entries from a specific directory.
	LoadDir(ctx context.Context, filesystem fs.FS, dirPath string) ([]registry.Entry, error)

	// LoadFile loads entries from a single file.
	LoadFile(ctx context.Context, filesystem fs.FS, filePath string) ([]registry.Entry, error)
}

// loaderKey is the context key for the loader component.
type loaderKey struct{}

// WithLoader attaches Loader to AppContext.
func WithLoader(ctx context.Context, ldr Loader) {
	ac := contextapi.AppFromContext(ctx)
	if ac != nil {
		ac.With(loaderKey{}, ldr)
	}
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
