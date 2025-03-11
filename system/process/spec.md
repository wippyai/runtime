# Process Management Component Specification

## System Events

The Process Management component uses the following event types for communication:

- **PrototypeSystem**: `"prototype"` - System identifier for process prototype events
- **ProtoRegister**: `"prototype.register"` - Event for registering a process prototype
- **ProtoDelete**: `"prototype.delete"` - Event for removing a process prototype
- **ProtoAccept**: `"prototype.accept"` - Event for successful prototype registration
- **ProtoReject**: `"prototype.reject"` - Event for failed prototype registration
- **HostSystem**: `"hosts"` - System identifier for process host events
- **HostRegister**: `"hosts.register"` - Event for registering a process host
- **HostDelete**: `"hosts.delete"` - Event for removing a process host
- **HostAccept**: `"hosts.accept"` - Event for successful host registration
- **HostReject**: `"hosts.reject"` - Event for failed host registration

## Purpose

The Process Management component provides a comprehensive framework for managing distributed processes within the Pony Runtime. It handles process creation, lifecycle management, host registration, and inter-process communication. The component enables the creation of dynamic, supervised processes that can communicate across different hosts and nodes in a distributed system.

## Core Concepts

### Process Identification

- **PID (Process Identifier)**: Uniquely identifies processes with Node, Host, Registry ID, and UniqID
- **Host**: Execution environment for processes, either Managed or Delegated
- **Prototype**: Template for creating process instances, registered in the system

### Process Lifecycle

- **Creation**: Processes are instantiated from registered prototypes
- **Execution**: Processes run on hosts and can communicate via messages
- **Supervision**: Processes can monitor or link to other processes
- **Termination**: Processes can be gracefully cancelled or forcefully terminated

### Host Types

- **Managed Host**: Receives fully instantiated process instances from the manager
- **Delegated Host**: Receives process identifiers and creates processes itself

## Architecture

### Key Components

1. **HostRegistry**
   - Tracks available process hosts
   - Handles host registration events
   - Validates host implementations
   - Provides lookup capabilities for process hosts

2. **PrototypeRegistry**
   - Manages process prototype registration
   - Creates process instances from registered prototypes
   - Validates prototype implementations
   - Handles prototype lifecycle events

3. **Manager**
   - Orchestrates process launches across hosts
   - Handles process lifecycle events
   - Manages process monitoring and linking
   - Provides context integration for process lifecycle

4. **Context Integration**
   - Stores process lifecycle callbacks in context
   - Enables composable process lifecycle management
   - Provides access to process manager throughout the system

## Interface Definitions

### Manager Interface

```go
type Manager interface {
    // Start launches a new process according to the provided configuration
    Start(ctx context.Context, start *Start) (pubsub.PID, error)
    
    // Terminate forcefully stops a running process
    Terminate(ctx context.Context, pid pubsub.PID) error
    
    // Cancel sends a cancellation signal to a process
    Cancel(ctx context.Context, from, pid pubsub.PID, deadline time.Time) error
    
    // AttachLifecycle enhances a context with process lifecycle management
    AttachLifecycle(context.Context, Lifecycle) context.Context
}
```

### Host Interface

```go
type Host interface {
    // Send dispatches a message to the host
    Send(*pubsub.Package) error
    
    // Terminate forcefully stops a running process
    Terminate(ctx context.Context, pid pubsub.PID) error
}
```

### Managed Host Interface

```go
type Managed interface {
    Host
    
    // Launch starts a process with the provided configuration
    Launch(ctx context.Context, launch *Launch) (pubsub.PID, error)
}
```

### Delegated Host Interface

```go
type Delegated interface {
    Host
    
    // Launch starts a process with the provided PID, lifecycle, and input
    Launch(ctx context.Context, pid pubsub.PID, lf Lifecycle, input payload.Payloads) (pubsub.PID, error)
}
```

### Process Interface

```go
type Process interface {
    // Send dispatches a message to the process
    Send(*pubsub.Package) error
    
    // Start initializes the process
    Start(context.Context, pubsub.PID, payload.Payloads) error
    
    // Step advances process state by one iteration
    Step() error
    
    // Ready returns the size of the runner's queue
    Ready() int
}
```

## Operation Flow

### Host Registration

1. A host implementation sends a `HostRegister` event with its implementation
2. The `HostRegistry` validates the host implementation (Managed or Delegated)
3. If valid, the host is stored in the registry and an `HostAccept` event is sent
4. If invalid, an `HostReject` event is sent with the reason

### Prototype Registration

1. A process prototype sends a `ProtoRegister` event with its factory function
2. The `PrototypeRegistry` validates the prototype implementation
3. If valid, the prototype is stored in the registry and a `ProtoAccept` event is sent
4. If invalid, a `ProtoReject` event is sent with the reason

### Process Launch

1. Client code calls `Manager.Start()` with process configuration
2. Manager looks up the host and prepares a PID
3. For Managed hosts:
   - Manager creates a Process instance from the registered prototype
   - Manager passes the Process, PID, Input, and Lifecycle to the host's Launch method
4. For Delegated hosts:
   - Manager passes the PID, Lifecycle, and Input to the host's Launch method
5. The host starts the process and returns the PID
6. Topology integration (monitoring, linking) is handled via lifecycle callbacks

### Process Termination

1. Client code calls `Manager.Terminate()` with a PID
2. Manager looks up the host for the PID
3. Manager calls the host's Terminate method with the PID
4. The host forcefully stops the process
5. Process completion callbacks are triggered for cleanup

### Process Cancellation

1. Client code calls `Manager.Cancel()` with source PID, target PID, and deadline
2. Manager looks up the host for the target PID
3. Manager sends a cancellation package to the target process
4. The process receives the cancellation and handles graceful shutdown
5. If the process doesn't terminate by the deadline, it may be forcefully terminated

## Usage Patterns

### Registering a Process Host

```go
// Create a host implementation
host := NewMyManagedHost()

// Send host registration event
bus.Send(ctx, event.Event{
System: process.HostSystem,
Kind:   process.HostRegister,
Path:   "my-host",
Data:   host,
})
```

### Registering a Process Prototype

```go
// Define process prototype factory function
protoFunc := func() (process.Process, error) {
return &MyProcess{}, nil
}

// Send prototype registration event
bus.Send(ctx, event.Event{
System: process.PrototypeSystem,
Kind:   process.ProtoRegister,
Path:   "my-namespace:my-process",
Data:   process.Prototype(protoFunc),
})
```

### Starting a Process

```go
// Get process manager from context
manager := process.GetProcesses(ctx)

// Configure process start
start := &process.Start{
HostID: "my-host",
Source: registry.ID{NS: "my-namespace", Name: "my-process"},
Input:  payload.New("input data"),
Lifecycle: process.Lifecycle{
Parent:  parentPID,
Monitor: true,
Link:    false,
},
}

// Launch the process
pid, err := manager.Start(ctx, start)
if err != nil {
// Handle error
}

// Use the returned PID
```

### Implementing a Process

```go
type MyProcess struct {
// Process state
}

func (p *MyProcess) Start(ctx context.Context, pid pubsub.PID, input payload.Payloads) error {
// Initialize process with input
return nil
}

func (p *MyProcess) Step() error {
// Advance process state
return nil
}

func (p *MyProcess) Send(pkg *pubsub.Package) error {
// Handle incoming messages
return nil
}

func (p *MyProcess) Ready() int {
// Return queue size
return 0
}
```

### Implementing a Managed Host

```go
type MyManagedHost struct {
// Host state
}

func (h *MyManagedHost) Launch(ctx context.Context, launch *process.Launch) (pubsub.PID, error) {
// Start the process
err := launch.Process.Start(ctx, launch.PID, launch.Input)
if err != nil {
return pubsub.PID{}, err
}

// Invoke onStart callback if present
if onStart := process.GetOnStart(ctx); onStart != nil {
onStart(launch.PID, launch.Process)
}

// Run process in background
go h.runProcess(ctx, launch.PID, launch.Process)

return launch.PID, nil
}

func (h *MyManagedHost) Terminate(ctx context.Context, pid pubsub.PID) error {
// Stop the process
return nil
}

func (h *MyManagedHost) Send(pkg *pubsub.Package) error {
// Forward message to process
return nil
}
```

## Error Handling

The system defines several standard errors:

- `ErrNoProcess`: Indicates that no process is currently running
- `ErrHostBusy`: Indicates that the process host is already running at capacity
- `ErrMaxProcesses`: Indicates that the maximum number of processes has been reached
- `ErrHostDead`: Indicates that the process host is no longer available
- `ErrHostNotFound`: Indicates that the requested host could not be found
- `ErrAlreadyAttached`: Indicates that a receiver is already attached to the specified PID

## Implementation Notes

### HostRegistry Implementation

- Uses `sync.Map` for thread-safe concurrent operations
- Validates host implementations to ensure they conform to either Managed or Delegated interfaces
- Publishes acceptance/rejection events for registration operations
- Provides host lookup for process managers

### PrototypeRegistry Implementation

- Uses `sync.Map` for thread-safe concurrent operations
- Provides factory methods for creating processes from registered prototypes
- Validates prototype implementations
- Publishes acceptance/rejection events for registration operations

### Manager Implementation

- Handles both Managed and Delegated host types with appropriate launch logic
- Generates unique IDs for processes when not provided
- Integrates with topology for process monitoring and linking
- Provides lifecycle callback integration through context

### Context Integration

- Provides composable lifecycle callbacks that can be aggregated
- Enables process lifecycle management without direct coupling
- Supports both OnStart and OnComplete callbacks for full lifecycle coverage

This specification outlines the Process Management component for the Pony Runtime, providing a comprehensive framework for managing distributed processes with support for process creation, lifecycle management, and fault tolerance.