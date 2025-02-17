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
		ID       registry.ID
		Meta     registry.Metadata
		Provider Provider
	}

	// Resource provides controlled access to a resource instance
	Resource[T any] interface {
		// Get returns the managed resource instance.
		// The returned resource is valid until Release is called.
		Get() (T, error)

		// Release frees the resource and invalidates the access grant.
		// After Release is called, subsequent Get() calls will panic.
		// It's safe to call Release multiple times.
		Release() error
	}

	// Provider defines an interface for components that can provide access to resources.
	// Implementations are responsible for managing resource lifecycle and access control.
	//
	// The provider acts as a factory/manager for resources, handling:
	// - Resource instantiation and initialization
	// - Access mode validation and enforcement
	// - Resource state management
	// - Cleanup and release of resources
	//
	// Providers must ensure thread-safety when handling concurrent access requests.
	// They should also properly handle context cancellation for long-running operations.
	Provider interface {
		// Acquire attempts to obtain access to a resource identified by id with the specified access mode.
		//
		// The context can be used to cancel long-running acquire operations or pass deadlines.
		// The id uniquely identifies the resource being requested.
		// The mode specifies the type of access being requested (read, write, exclusive).
		//
		// Returns:
		// - A Resource providing controlled access to the resource
		// - ErrResourceNotFound if the requested resource doesn't exist
		// - ErrResourceLocked if the resource is currently locked for exclusive access
		// - ErrInvalidAccessMode if the requested access mode is invalid
		// - Other implementation-specific errors that may occur during acquisition
		//
		// The returned Resource must be released when no longer needed by calling Release().
		Acquire(ctx context.Context, id registry.ID, mode AccessMode) (Resource[any], error)
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
		Acquire(ctx context.Context, id registry.ID, mode AccessMode) (Resource[any], error)

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
