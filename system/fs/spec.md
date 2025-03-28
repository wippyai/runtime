# Filesystem Component Specification

## Purpose
The Filesystem (FS) component provides abstraction and management of filesystem resources in the Wippy Runtime. It enables registration, access, and manipulation of multiple filesystem implementations through a unified interface.

## Core Concepts

### Filesystem Interfaces
- **ReadFS**: Read-only filesystem operations (Open, Stat, ReadDir)
- **WriteFS**: Write operations (OpenFile, Remove, Mkdir)
- **FS**: Complete filesystem interface combining read and write capabilities
- **File**: Readable, writable file with seeking and synchronization

### Registry
- Central manager for filesystem implementations
- Event-based registration and deregistration
- Context integration for dependency injection

## Architecture

### Registry Component
The Registry serves as the central component:
- Manages filesystem registration through event system
- Provides lookup capabilities for registered filesystems
- Thread-safe implementation using sync.Map
- Integrates with the context system for dependency injection

### File Interface
Extends standard io/fs interfaces with write capabilities:
```go
type File interface {
    fs.File         // Read and Close operations
    io.Writer       // Write operations
    io.Seeker       // Seek operations
    Sync() error    // Synchronization
}
```

## Event-Based Management

### Registration Events
- `fs.register`: Register a new filesystem implementation
- `fs.delete`: Remove a filesystem from registry
- `fs.accept`: Confirmation of successful registration/deletion
- `fs.reject`: Notification of failed registration/deletion

## Message Flow

### Filesystem Registration
1. Client sends `fs.register` event with filesystem implementation
2. Registry validates and stores the filesystem
3. Registry sends `fs.accept` or `fs.reject` response

### Filesystem Lookup
1. Client retrieves Registry from context
2. Client calls `registry.GetFS(name)` with filesystem identifier
3. Registry returns the filesystem implementation if found

## Context Integration

### Registry Access
- `WithFSRegistry(ctx, registry)`: Store registry in context
- `GetRegistry(ctx)`: Retrieve registry from context

## Use Cases

### Registering a Filesystem
```go
// Create filesystem implementation
fs := &MyFSImplementation{}

// Register via event bus
bus.Send(ctx, event.Event{
    System: fs.System,
    Kind:   fs.Register,
    Path:   "myfs:implementation",
    Data:   fs,
})
```

### Using a Filesystem
```go
// Get registry from context
registry := fs.GetRegistry(ctx)

// Get filesystem by name
myFS, exists := registry.GetFS("myfs:implementation")
if !exists {
    // Handle filesystem not found
}

// Use the filesystem
file, err := myFS.OpenFile("path/to/file", os.O_RDWR, 0644)
if err != nil {
    // Handle error
}
defer file.Close()

// Read from file
data := make([]byte, 1024)
n, err := file.Read(data)

// Write to file
_, err = file.Write([]byte("Hello, world"))
```