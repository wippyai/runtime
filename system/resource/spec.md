I'll create a specification that explains the purpose and functionality of this Resource Registry component based on the code provided.

# Resource Registry Component Specification

## Purpose
The Resource Registry is a central management system for shared resources within the Wippy Runtime. It allows components to register, access, and manage resources in a coordinated way, with support for different access modes and lifecycle management.

## Core Concepts

### Resources
- A resource is any shared object or service that needs controlled access
- Examples include SQL databases, file handles, network connections
- Each resource has a unique identifier (`registry.ID`) with namespace and name
- Resources can have metadata for additional information

### Access Modes
- **Normal Mode**: Multiple concurrent access allowed (read operations)
- **Exclusive Mode**: Locks the resource for a single accessor (write operations)

### Resource Lifecycle
1. **Registration**: Resources are registered with a Provider
2. **Acquisition**: Components request access to resources
3. **Usage**: Components use the acquired resource
4. **Release**: Resources are released when no longer needed
5. **Update/Delete**: Resources can be updated or removed from the registry

## Architecture

### Key Components

1. **Registry**
    - Central service that tracks all available resources
    - Dispatches resource acquisition requests to appropriate providers
    - Maintains resource metadata and availability state

2. **Provider**
    - Responsible for actual resource management
    - Controls access to underlying resource instances
    - Implements access control logic (normal vs. exclusive)

3. **Resource**
    - Represents a handle to an acquired resource
    - Provides methods to access the underlying resource
    - Manages proper release of resources

4. **Event Bus**
    - Enables communication about resource status changes
    - Supports events for resource registration, updates, and deletion

## Usage Patterns

### Resource Registration
```go
// Create resource entry
entry := resource.Entry{
    ID:       registry.ID{NS: "db", Name: "main"},
    Provider: dbProvider,
    Meta:     map[string]interface{}{"type": "postgres"},
}

// Register via event
bus.Send(ctx, event.Event{
    System: resource.System,
    Kind:   resource.Register,
    Path:   entry.ID.String(),
    Data:   entry,
})
```

### Resource Acquisition
```go
// Get registry from context
registry := resource.GetRegistry(ctx)

// Acquire resource with normal access
dbResource, err := registry.Acquire(ctx, 
    registry.ID{NS: "db", Name: "main"}, 
    resource.ModeNormal)
if err != nil {
    // Handle error
}
defer dbResource.Release()

// Use the resource
db, err := dbResource.Get()
// Work with db...
```

### Exclusive Access Pattern
```go
// Acquire with exclusive access for write operations
dbResource, err := registry.Acquire(ctx, 
    registry.ID{NS: "db", Name: "main"}, 
    resource.ModeExclusive)
if err != nil {
    // Handle resource locked error
}
defer dbResource.Release()

// Perform exclusive operations...
```

## Error Handling

The system defines several standard errors:
- `ErrResourceNotFound`: The requested resource doesn't exist
- `ErrResourceLocked`: Resource is currently locked for exclusive access
- `ErrResourceReleased`: Attempt to use a resource that has been released

## Integration with Context

The Registry can be stored in and retrieved from a context:
- `WithResources(ctx, registry)`: Attaches registry to context
- `GetResources(ctx)`: Retrieves registry from context

## Event-Based Communication

Resources use an event system for registration and lifecycle events:
- `resource.Register`: New resource registration
- `resource.Update`: Resource information update
- `resource.Delete`: Resource removal

## Security and Resource Protection

- Resources can be locked for exclusive access
- Context cancellation automatically propagates to resource acquisition
- Proper release of resources is enforced through the Resource interface

## Implementation Notes

- Uses Go's sync.Map for thread-safe resource tracking
- Subscribes to resource events for registry updates
- Supports context cancellation for resource acquisition

This component provides a robust foundation for managing shared resources in the Wippy Runtime, ensuring safe concurrent access while supporting both exclusive and shared access patterns.