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

// EntryResolver is an optional Registry capability that resolves a filesystem
// for a specific registry entry, honoring the provenance of the operation that
// carries it. It lets consumers pick the correct pack when more than one module
// version exposes the same resource ID — for example, while a module update has
// both the old and new packs staged. A nil or host-authored provenance resolves
// by entry ID, as does a module-owned entry whose provenance names no version
// and whose module exposes no matching pack.
type EntryResolver interface {
	GetFSForEntry(entry registry.Entry, prov *registry.EntryProvenance) (fs.ReadDirFS, error)
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
