# Process Component Specification

## Purpose

The Process component provides a framework for managing distributed process lifecycles within the Pony Runtime. It
handles process creation, execution, supervision, and communication, supporting a fault-tolerant and isolated process
model.

## Core Concepts

### Process Model

- **Process**: An executable unit with its own lifecycle and isolated state
- **PID** (Process Identifier): Uniquely identifies a process with format `{node@host|namespace:name|uniq_id}`
- **Prototype**: Factory function that creates process instances
- **Host**: Execution environment for processes
- **Lifecycle**: Defines supervision relationships between processes

### Host Types

- **Managed Host**: Receives process instances from manager, runs locally
- **Delegated Host**: Creates processes itself based on instructions

### Process Lifecycle

1. **Creation**: Process is instantiated from a prototype
2. **Launch**: Process is initialized and started on a host
3. **Execution**: Process runs step by step
4. **Termination**: Process completes or fails, notifications sent

## Architecture Components

### PrototypeRegistry

- Manages process prototypes
- Handles registration/deregistration via events
- Creates process instances on demand

### HostRegistry

- Manages available host environments
- Tracks both managed and delegated hosts
- Handles host registration/deregistration via events

### ProcessManager

- Orchestrates process launches
- Selects appropriate hosts
- Handles lifecycle events
- Propagates supervision information

## Supervision Model

### Monitoring

- Parent receives notification when child terminates
- Parent continues running independently

### Linking

- Bidirectional dependency between processes
- If either process terminates with error, the other is also terminated

## Event-Based Communication

### Prototype Events

- `prototype.register`: Register new process prototype
- `prototype.delete`: Remove existing prototype
- `prototype.accept`/`prototype.reject`: Registration responses

### Host Events

- `hosts.register`: Register new process host
- `hosts.delete`: Remove existing host
- `hosts.accept`/`hosts.reject`: Registration responses

## Message Flow

### Process Launch

1. Client requests process start with `Start` configuration
2. Manager selects host and prepares PID
3. For managed hosts:
    - Manager creates process instance from prototype
    - Host receives process instance and Launch configuration
4. For delegated hosts:
    - Host creates and starts process itself
    - Host returns assigned PID

### Process Termination

1. Manager receives termination request
2. Manager locates host for target process
3. Host forcefully terminates process
4. Termination events propagate via topology system

### Process Cancellation

1. Manager receives cancellation request with deadline
2. Cancellation message sent to process
3. Process can clean up before deadline
4. If deadline passes, process is forcefully terminated

## Context Integration

### Process Manager Access

- `WithProcesses(ctx, manager)`: Store manager in context
- `GetProcesses(ctx)`: Retrieve manager from context

### Lifecycle Callbacks

- `OnStart`: Called when a process begins execution
- `OnComplete`: Called when a process finishes execution
- Callbacks are chainable via `WithAddedOnStart`/`WithAddedOnComplete`

## Error Handling

- `ErrNoProcess`: No process is currently running
- `ErrHostBusy`: Process host is at capacity
- `ErrMaxProcesses`: Maximum process limit reached
- `ErrHostDead`: Host is no longer available

This specification defines the Process component that provides a robust foundation for distributed process management in
the Pony Runtime, enabling fault-tolerant and isolated execution of concurrent processes.