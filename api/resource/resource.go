// Package resource provides a system for managing and accessing shared resources.
package resource

import (
	"context"
	"errors"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/registry"
)

// System constants define event types and identifiers for the resource system
const (
	// System identifies the resource management system in the event context
	System event.System = "resource"

	// Register is emitted when a new resource is registered
	Register event.Kind = "resource.register"
	// Update is emitted when a resource is updated
	Update event.Kind = "resource.update"
	// Delete is emitted when a resource is removed
	Delete event.Kind = "resource.delete"
)

// Common errors returned by the resource system
var (
	// ErrResourceNotFound indicates the requested resource doesn't exist
	ErrResourceNotFound = errors.New("resource not found")
	// ErrResourceLocked indicates the resource is currently locked for exclusive access
	ErrResourceLocked = errors.New("resource is locked")
	// ErrResourceReleased indicates an attempt to use a resource that has been released
	ErrResourceReleased = errors.New("resource has been released")
)

// AccessMode defines the type of access requested for a resource
type AccessMode uint8

// AccessMode constants define different resource access patterns
const (
	// ModeNormal indicates the resource can only be read
	ModeNormal AccessMode = 1 << iota
	// ModeExclusive indicates the resource is locked for exclusive access
	ModeExclusive
)

type (
	// Entry represents a registered resource with its metadata
	Entry struct {
		// ID uniquely identifies the resource
		ID registry.ID
		// Meta contains additional resource metadata
		Meta registry.Metadata
		// Provider is responsible for managing access to the resource
		Provider Provider
	}

	// Resource provides controlled access to a resource instance
	Resource[T any] interface {
		// Get returns the managed resource instance.
		// The returned resource is valid until Release is called.
		Get() (T, error)

		// Release frees the resource and invalidates the access grant.
		// After Release is called, subsequent Get() calls will fail.
		// It's safe to call Release multiple times.
		Release()
	}

	// Provider defines an interface for components that can provide access to resources.
	// Implementations are responsible for managing resource lifecycle and access control.
	Provider interface {
		// Acquire attempts to obtain access to a resource identified by id with the specified access mode.
		//
		// The context can be used to cancel long-running acquire operations or pass deadlines.
		// The id uniquely identifies the resource being requested.
		// The mode specifies the type of access being requested (normal, exclusive).
		//
		// Returns a Resource providing controlled access to the resource or an error.
		// Common errors include:
		// - ErrResourceNotFound if the requested resource doesn't exist
		// - ErrResourceLocked if the resource is currently locked for exclusive access
		Acquire(ctx context.Context, id registry.ID, mode AccessMode) (Resource[any], error)
	}

	// Registry manages resources and provides a centralized access point for resource acquisition.
	Registry interface {
		// Acquire attempts to acquire a resource with the specified access mode.
		// Returns a Resource providing controlled access or an error.
		Acquire(ctx context.Context, id registry.ID, mode AccessMode) (Resource[any], error)

		// List returns all registered resource IDs.
		List() ([]registry.ID, error)

		// Exists checks if a resource is registered without acquiring it.
		Exists(id registry.ID) bool
	}
)
