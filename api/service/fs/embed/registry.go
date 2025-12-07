package embed

import (
	"context"
	"io/fs"

	"github.com/wippyai/runtime/api/registry"
)

type embedRegistryKeyType struct{}

var embedRegistryKey = embedRegistryKeyType{}

// Registry provides access to embedded filesystem resources.
// Implementation is backed by pack readers but the interface abstracts this detail.
type Registry interface {
	// GetFS returns a filesystem for the given resource ID.
	// Returns fs.ErrNotExist if the resource is not found.
	GetFS(id registry.ID) (fs.ReadDirFS, error)

	// Close releases all resources held by the registry.
	Close() error
}

// WithRegistry stores the Registry in the context.
func WithRegistry(ctx context.Context, reg Registry) context.Context {
	return context.WithValue(ctx, embedRegistryKey, reg)
}

// GetRegistry retrieves the Registry from the context.
// Returns nil if not found.
func GetRegistry(ctx context.Context) Registry {
	if reg, ok := ctx.Value(embedRegistryKey).(Registry); ok {
		return reg
	}
	return nil
}
