# PubSub Component Specification

## Purpose

The PubSub component provides a distributed message-passing system for communication between processes in the Pony
Runtime. It enables asynchronous, topic-based messaging with support for both local and remote process communication.

## Core Concepts

### Process Identification

Each process in the system is uniquely identified by a PID (Process Identifier) with the format:
`{node@host|namespace:name|uniq_id}`.

- **Node**: Identifies which node the process belongs to (optional for local processes)
- **Host**: Identifies which host environment the process runs in
- **Namespace:Name**: Registry identifier for the process type
- **UniqID**: Unique instance identifier

### Messaging Structure

- **Package**: Main container for message delivery
    - Contains destination PID (where messages should be delivered)
    - Contains one or more messages

- **Message**: Individual communication unit
    - Identified by a topic (string)
    - Contains payload data (actual content)

### Component Hierarchy

1. **Node**: Top-level component that manages multiple hosts
2. **Host**: Container that manages process message delivery
3. **Process**: End consumer that receives messages via attached channels

## Message Routing

### Local Delivery

When a process sends a message to another process on the same node:

1. Sender creates a Package with the recipient's PID
2. Package is sent to the Node
3. Node identifies the target Host based on PID.Host
4. Host uses a worker pool to deliver the Package
5. Message is delivered to the channel attached to the recipient's PID

### Remote Delivery

When a process sends a message to a process on a different node:

1. Sender creates a Package with the recipient's PID (including remote node)
2. Local Node detects non-local destination
3. Package is forwarded to upstream receiver
4. Upstream delivers to the destination Node
5. Destination Node routes to the appropriate Host
6. Host delivers to the recipient process

## Implementation Details

### Host

- Manages process message delivery within a single environment
- Uses worker pool for concurrent message processing
- Employs consistent hashing to route messages to specific workers
- Maps PIDs to receiver channels using thread-safe collection
- Handles attachment/detachment of process receiver channels

### Node

- Manages multiple hosts within a logical node
- Routes messages to appropriate hosts or upstream
- Handles host registration/deregistration
- Can forward messages to parent nodes in distributed setups

### NodeManager

- Event-driven management layer for Node
- Listens for host registration/removal events
- Processes registration requests and sends responses
- Delegates actual message operations to underlying Node

## Error Handling

The system defines several standard errors:

- `ErrAlreadyAttached`: Attempt to attach a receiver to a PID that already has one
- `ErrHostNotFound`: Requested host doesn't exist in the node
- `ErrHostAlreadyExists`: Host with the given ID is already registered

## Performance Considerations

The implementation includes several optimizations:

- Object pooling for Package instances to reduce memory allocations
- Multiple worker goroutines with dedicated queues to reduce contention
- Consistent hashing to ensure messages for the same PID go to the same worker
- Context-based cancellation for clean shutdown
- Non-blocking channel operations with configurable buffer sizes

## Integration

### Event System

The PubSub component uses an event system for management operations:

- `node.register_host`: Request to register a new host
- `node.remove_host`: Request to remove a host
- `node.accept_host`: Response for successful operation
- `node.reject_host`: Response for failed operation

### Context Integration

Helper functions to store and retrieve PubSub components in context:

- `WithPID/GetPID`: Store/retrieve PID
- `WithNode/GetNode`: Store/retrieve Node
- `WithHost/GetHost`: Store/retrieve Host

This specification outlines a flexible, performant messaging system that forms the communication backbone of the Pony
Runtime's distributed process model.