# Topology Component Specification

## System Events

The Topology component uses the following event types for process communication:

- **TopicInbox**: `"@pid/inbox"` - Inbox topic for normal process messages
- **TopicEvents**: `"@pid/events"` - Events topic for process lifecycle events
- **KindCancel**: `"pid.cancel"` - Cancellation request event
- **KindExit**: `"pid.exit"` - Process exit notification
- **KindLinkDown**: `"pid.link.down"` - Notification that a linked process is down
- **KindLinkEstablished**: `"pid.link.established"` - Notification of link establishment
- **KindLinkRemoved**: `"pid.link.removed"` - Notification of link removal

## Purpose

The Topology component provides process communication and lifecycle management within the Pony Runtime. It implements Erlang-style semantics for process monitoring, linking, and naming, enabling robust distributed systems with supervision patterns, failure isolation, and coordinated process management.

## Core Concepts

### Process Identification

- **PID (Process Identifier)**: Unique identifier for runtime processes, containing Node, Host, Registry ID, and a unique identifier
- **PID Registry**: Allows naming processes with human-readable strings for easier addressing
- **Process Relationships**: Processes can monitor or link to other processes to react to their lifecycle events

### Process Monitoring

- **Monitor**: One-way relationship where a process watches another's lifecycle
- **Notification**: When a monitored process exits, all monitors receive a notification
- **Error Propagation**: Exit notifications include the result and any error that caused the exit

### Process Linking

- **Link**: Bidirectional relationship between processes
- **Error Propagation**: When a linked process exits, all linked processes receive the exit notification
- **Automatic Cleanup**: Links are automatically removed when a process exits

## Architecture

### Key Components

1. **Topology**
    - Central manager for process relationships
    - Tracks process monitors, links, and registrations
    - Handles process exit notifications and cleanup

2. **PIDRegistry**
    - Provides Erlang-style name registration for PIDs
    - Supports hierarchical registries with parent-child relationships
    - Optimized for concurrent access with thread-safe operations

3. **Context Integration**
    - Helper functions to store/retrieve Topology and PIDRegistry from context
    - Enables easy access to topology services throughout the runtime

## Interface Definitions

### Topology Interface

```go
type Topology interface {
    // Monitor capabilities
    Wait(caller, pid pubsub.PID) error
    Release(caller, pid pubsub.PID) error
    
    // Links capabilities
    Link(from, to pubsub.PID) error
    Unlink(from, to pubsub.PID) error
    GetLinks(pid pubsub.PID) []pubsub.PID
    
    // Process lifecycle management
    Register(pid pubsub.PID) error
    Notify(pid pubsub.PID, result *runtime.Result)
    Remove(pid pubsub.PID)
}
```

### PIDRegistry Interface

```go
type PIDRegistry interface {
    // Register associates a name with a PID
    Register(name string, pid pubsub.PID) error
    
    // Unregister removes a name registration
    Unregister(name string) bool
    
    // Lookup finds the PID registered with a given name
    Lookup(name string) (pubsub.PID, bool)
}
```

## Event Structures

### Base Event

```go
type Event struct {
    // At is the timestamp when the event occurred
    At time.Time `json:"at"`
    // Kind identifies the type of event
    Kind Kind `json:"kind"`
    // From identifies the source process
    From pubsub.PID `json:"from"`
}
```

### Exit Event

```go
type ExitEvent struct {
    // Event contains the base event information
    Event Event `json:"event"`
    // Result contains the exit result information
    Result *runtime.Result `json:"result"`
}
```

### Link Event

```go
type LinkEvent struct {
    // Event contains the base event information
    Event Event `json:"event"`
    // To identifies the target process of the link
    To pubsub.PID `json:"to"`
}
```

## Operation Flow

### Process Registration

1. A process registers with the Topology using its PID
2. The process may also register a friendly name via the PIDRegistry
3. Once registered, the process can be monitored or linked by other processes

### Process Monitoring

1. Process A (caller) calls `Wait(A, B)` to monitor Process B
2. The Topology verifies Process B is registered
3. The Topology adds Process A to Process B's watchers list
4. When Process B exits, all watchers (including A) receive an exit notification

### Process Linking

1. Process A calls `Link(A, B)` to establish a bidirectional link with Process B
2. The Topology verifies both processes are registered
3. The Topology creates bidirectional links between A and B
4. Both processes receive a link notification
5. When either process exits, the other receives an exit notification

### Process Exit

1. When a process exits, it calls `Notify` with its exit result
2. The Topology sends exit notifications to all linked processes and monitors
3. The Topology calls `Remove` to clean up all references to the process
4. All links are removed and processes receive unlink notifications

## Usage Patterns

### Registering a Process

```go
// Get the topology from context
topo := topology.GetTopology(ctx)

// Register process with topology
topo.Register(myPID)

// Register a friendly name (optional)
registry := topology.GetPIDRegistry(ctx)
registry.Register("worker-1", myPID)
```

### Creating a Process Monitor

```go
// Get the topology from context
topo := topology.GetTopology(ctx)

// Monitor another process
err := topo.Wait(myPID, targetPID)
if err != nil {
    // Handle error (process not registered or already monitored)
}

// Later, release the monitor if needed
topo.Release(myPID, targetPID)
```

### Creating Process Links

```go
// Get the topology from context
topo := topology.GetTopology(ctx)

// Link to another process
err := topo.Link(myPID, targetPID)
if err != nil {
    // Handle error (process not registered)
}

// Later, unlink if needed
topo.Unlink(myPID, targetPID)

// Get all linked processes
linkedPIDs := topo.GetLinks(myPID)
```

### Looking up a Named Process

```go
// Get the registry from context
registry := topology.GetPIDRegistry(ctx)

// Look up a process by name
if targetPID, found := registry.Lookup("worker-1"); found {
    // Use targetPID
} else {
    // Process not found
}
```

### Process Exit Handling

```go
// Get the topology from context
topo := topology.GetTopology(ctx)

// When process exits, notify topology with result
result := &runtime.Result{
    Payload: payload.New("completed work"),
    Error:   err, // nil or any error that caused exit
}
topo.Notify(myPID, result)

// Clean up process completely
topo.Remove(myPID)

// Unregister name if registered
registry := topology.GetPIDRegistry(ctx)
registry.Unregister("worker-1")
```

## Error Handling

The topology handles various error conditions:

- **Monitoring Unregistered Process**: Returns error when attempting to monitor an unregistered process
- **Double Monitoring**: Returns error when a process tries to monitor the same process twice
- **Linking Unregistered Process**: Returns error when trying to link to an unregistered process
- **Name Already Registered**: Returns `ErrNameAlreadyRegistered` when trying to register a name that's already taken

## Implementation Notes

### Topology Implementation

- Uses `sync.Map` for thread-safe concurrent operations
- Maintains separate maps for monitors, links, and registrations
- Avoids blocking on communication with upstream by ignoring send errors
- Ensures bidirectional link consistency by updating both sides atomically

### PIDRegistry Implementation

- Supports hierarchical registries with parent-child relationships
- Registry lookups cascade to parent registry if not found locally
- Provides thread-safe operations using `sync.Map`
- Includes logging for debugging registration operations

### Context Integration

- Provides helper functions to store and retrieve Topology and PIDRegistry from context
- Type-safe accessor functions that return nil if component is not found
- Ensures components can be accessed throughout the process lifecycle
