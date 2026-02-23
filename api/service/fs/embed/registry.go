// SPDX-License-Identifier: MPL-2.0

package embed

import (
	"context"
	"io/fs"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
)

var registryKey = &ctxapi.Key{Name: "fs.embed.registry"}

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
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(registryKey) == nil {
		ac.With(registryKey, reg)
	}
	return ctx
}

// GetRegistry retrieves the Registry from the context.
// Returns nil if not found.
func GetRegistry(ctx context.Context) Registry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if reg := ac.Get(registryKey); reg != nil {
		return reg.(Registry)
	}
	return nil
}
