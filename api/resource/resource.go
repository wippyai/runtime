package resource

import (
	"context"
	"errors"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
)

// System constants define event types and identifiers for the resource system
const (
	System   events.System = "resources"
	Register events.Kind   = "resources.register"
	Update   events.Kind   = "resources.update"
	Remove   events.Kind   = "resources.remove"
)

// Common errors returned by the resource system
var (
	ErrResourceNotFound  = errors.New("resource not found")
	ErrResourceLocked    = errors.New("resource is locked")
	ErrInvalidAccessMode = errors.New("invalid access mode")
	ErrResourceReleased  = errors.New("resource has been released")
)

// AccessMode defines the type of access requested for a resource
type AccessMode uint8

const (
	ReadOnly  AccessMode             = 1 << iota // Resource can only be read
	WriteOnly                                    // Resource can only be written
	ReadWrite = ReadOnly | WriteOnly             // Resource can be read and written
	Exclusive                                    // Resource is locked for exclusive access
)

// IsValid checks if the access mode combination is valid
func (m AccessMode) IsValid() bool {
	// Exclusive can't be combined with other modes
	if m&Exclusive != 0 {
		return m == Exclusive
	}
	// Must have at least one access type
	return m != 0 && m <= ReadWrite
}

type (
	// Entry represents a registered resource with its metadata
	Entry struct {
		ID       registry.ID       // Unique identifier for the resource
		Meta     registry.Metadata // Associated metadata
		Resource any               // The actual resource instance
	}

	// ManagedResource provides controlled access to a resource instance
	ManagedResource[T any] interface {
		// Get returns the managed resource instance.
		// The returned resource is valid until Release is called.
		// Panics if called after Release.
		Get() T

		// Release frees the resource and invalidates the access grant.
		// After Release is called, subsequent Get() calls will panic.
		// It's safe to call Release multiple times.
		Release()

		// AccessMode returns the current access mode granted for this resource
		AccessMode() AccessMode
	}

	// Registry manages resources.
	Registry interface {
		// Acquire attempts to acquire a resource with the specified access mode.
		// Returns:
		// - ErrResourceNotFound if the resource doesn't exist
		// - ErrResourceLocked if the resource is exclusively locked
		// - ErrInvalidAccessMode if the requested mode is invalid
		// - ErrRegistryUnavailable if the registry cannot be accessed
		// The context can be used to cancel long-running acquire operations.
		Acquire(ctx context.Context, id registry.ID, mode AccessMode) (ManagedResource[any], error)

		// List returns all registered resource IDs.
		// Returns ErrRegistryUnavailable if the registry cannot be accessed.
		List() ([]registry.ID, error)

		// Exists checks if a resource is registered without acquiring it.
		Exists(id registry.ID) bool
	}
)

// GetResources retrieves the ResourceRegistry instance from the context
// Panics if the ResourceRegistry is not found in the context
func GetResources(ctx context.Context) Registry {
	reg, ok := ctx.Value(contextapi.ResourcesCtx).(Registry)
	if !ok {
		panic("resource registry not found in context")
	}
	return reg
}
