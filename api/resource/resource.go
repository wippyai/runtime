// SPDX-License-Identifier: MPL-2.0

// Package resource provides a system for managing and accessing shared resources.
package resource

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
)

// System identifies the resource system in the event bus.
const System event.System = "resource"

// Event kinds for resource operations.
const (
	Register event.Kind = "resource.register"
	Update   event.Kind = "resource.update"
	Delete   event.Kind = "resource.delete"
)

// AccessMode constants.
const (
	ModeNormal    AccessMode = 1 << iota // Normal read access.
	ModeExclusive                        // Exclusive locked access.
)

type (
	// AccessMode defines the type of access requested for a resource.
	AccessMode uint8

	// Entry represents a registered resource with its metadata.
	Entry struct {
		Provider Provider
		Meta     attrs.Bag
		ID       registry.ID
	}

	// Resource provides controlled access to a resource instance.
	Resource[T any] interface {
		// Get returns the managed resource instance.
		Get() (T, error)

		// Release frees the resource and invalidates access.
		Release()
	}

	// Provider manages resource lifecycle and access control.
	Provider interface {
		// Acquire obtains access to a resource with the specified mode. A
		// successful Resource remains owned by the caller and valid until Release,
		// even if the provider is concurrently removed or replaced in a Registry.
		// Implementations own that lifetime guarantee; the Registry only routes
		// new acquisitions and tracks generations.
		Acquire(ctx context.Context, id registry.ID, mode AccessMode) (Resource[any], error)
	}

	// Registry manages resources and provides centralized access.
	Registry interface {
		// Acquire obtains a resource with the specified access mode.
		Acquire(ctx context.Context, id registry.ID, mode AccessMode) (Resource[any], error)

		// List returns all registered resource IDs.
		List() ([]registry.ID, error)

		// Exists checks if a resource is registered.
		Exists(id registry.ID) bool
	}
)

// TrackedResource wraps a Resource with borrow tracking.
type TrackedResource struct {
	inner     Resource[any]
	onRelease func()
	mu        sync.RWMutex
	released  bool
}

// NewTrackedResource creates a tracked wrapper around a resource.
func NewTrackedResource(inner Resource[any], onRelease func()) *TrackedResource {
	return &TrackedResource{inner: inner, onRelease: onRelease}
}

// Get returns the managed resource instance.
func (t *TrackedResource) Get() (any, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.released {
		return nil, ErrReleased
	}
	return t.inner.Get()
}

// Release frees the resource and invalidates access.
func (t *TrackedResource) Release() {
	t.mu.Lock()
	if t.released {
		t.mu.Unlock()
		return
	}
	t.released = true
	inner := t.inner
	onRelease := t.onRelease
	t.inner = nil
	t.onRelease = nil
	t.mu.Unlock()

	if onRelease != nil {
		defer onRelease()
	}
	inner.Release()
}
