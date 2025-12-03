// Package resource provides a system for managing and accessing shared resources.
package resource

import (
	"context"
	"sync"
	"sync/atomic"

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
		ID       registry.ID // Unique resource identifier.
		Meta     attrs.Bag   // Additional metadata.
		Provider Provider    // Manages access to the resource.
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
		// Acquire obtains access to a resource with the specified mode.
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
	released  atomic.Bool
}

var trackedResourcePool = sync.Pool{
	New: func() any {
		return &TrackedResource{}
	},
}

// NewTrackedResource creates a tracked wrapper around a resource.
func NewTrackedResource(inner Resource[any], onRelease func()) *TrackedResource {
	tr := trackedResourcePool.Get().(*TrackedResource)
	tr.inner = inner
	tr.onRelease = onRelease
	tr.released.Store(false)
	return tr
}

// Get returns the managed resource instance.
func (t *TrackedResource) Get() (any, error) {
	if t.released.Load() {
		return nil, ErrReleased
	}
	return t.inner.Get()
}

// Release frees the resource and invalidates access.
func (t *TrackedResource) Release() {
	if t.released.CompareAndSwap(false, true) {
		t.inner.Release()
		if t.onRelease != nil {
			t.onRelease()
		}
		t.inner = nil
		t.onRelease = nil
		trackedResourcePool.Put(t)
	}
}
