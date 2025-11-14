# PubSub Component Specification

## System Events

The PubSub component uses the following event types for communication:

- **System**: `"node"` - Identifies the node management system in the event context
- **HostRegister**: `"node.register_host"` - Event for requesting host registration
- **HostDelete**: `"node.remove_host"` - Event for requesting host removal
- **HostAccept**: `"node.accept_host"` - Event for successful host registration
- **HostReject**: `"node.reject_host"` - Event for failed host registration

## Purpose

The PubSub component provides a distributed message routing infrastructure for the Wippy Runtime. It enables efficient and reliable communication between processes across multiple hosts and nodes in a distributed system. The component supports hierarchical routing, process addressing through PIDs (Process Identifiers), and message delivery with customizable delivery guarantees.

## Core Concepts

### Hierarchical Structure

- **Node**: Top-level routing entity that manages multiple hosts and routes messages between them
- **Host**: Process execution environment that manages a set of processes and routes messages to them
- **PID (Process Identifier)**: Unique identifier for addressing processes across the distributed system

### Message Routing

- **Topics**: Named channels for message delivery that processes can subscribe to
- **Messages**: Units of communication containing topic and payload information
- **Packages**: Collections of related messages sent to a specific PID

### Distribution

- **Local Routing**: Direct delivery of messages to processes within the same host
- **Cross-Host Routing**: Delivery of messages between processes on different hosts within the same node
- **Cross-Node Routing**: Forwarding messages to processes on different nodes through upstream connections

## Architecture

### Key Components

1. **Node**
  - Top-level messaging router
  - Manages multiple hosts and routes messages between them
  - Forwards messages to upstream nodes when necessary
  - Maintains host registry for local message routing

2. **Host**
  - Process container and local message router
  - Manages process message queues
  - Implements asynchronous message delivery with worker pools
  - Provides consistent message delivery through PID-based routing

3. **NodeManager**
  - Event-driven management interface for nodes
  - Handles host registration and removal events
  - Provides administrative control over the node's configuration
  - Acts as a bridge between the event system and the Node implementation

## Interface Definitions

### Node Interface

```go
type Node interface {
    Host
    // ID returns the unique identifier for this node
    ID() NodeID
    // RegisterHost adds a host to this node with the specified ID
    RegisterHost(HostID, Host) error
    // UnregisterHost removes a host from this node
    UnregisterHost(HostID)
}
```

### Host Interface

```go
type Host interface {
    Receiver
    // Attach connects a process (identified by PID) to a message channel
    // Returns a cancel function to detach and any error that occurred
    Attach(PID, chan *Package) (context.CancelFunc, error)
    // Detach disconnects a process (identified by PID) from the host
    Detach(PID)
}
```

### Receiver Interface

```go
type Receiver interface {
    // Send dispatches a package to the upstream receiver
    Send(*Package) error
}
```

### Message Structure

```go
type Message struct {
    // Topic identifies the message category
    Topic Topic
    // Payloads contains the actual message data
    Payloads payload.Payloads
}

type Package struct {
    // PID identifies the destination process
    PID PID
    // Messages contains the actual message data
    Messages []*Message
}
```

### PID Structure

```go
type PID struct {
    // Node identifies which node the process belongs to
    Node NodeID
    // Host identifies which host the process belongs to
    Host HostID
    // ID contains the process's registry identifier
    ID registry.ID
    // UniqID contains a unique instance identifier
    UniqID string
}
```

## Operation Flow

### Message Routing

1. A sender calls `Send(package)` on a Receiver (Node or Host)
2. The Receiver examines the package's PID to determine the routing path:
  - If PID.Node matches the local Node or is empty, route within the node
  - If PID.Node is different and an upstream is configured, forward to upstream
3. For local routing, the Node looks up the Host based on PID.Host
4. The Host uses a worker pool to deliver the message to the appropriate receiver channel
5. The Host uses consistent hashing based on PID.UniqID to ensure messages for the same PID are processed by the same worker

### Host Registration

1. Client sends a HostRegister event with a Host implementation
2. NodeManager receives the event and calls RegisterHost on the underlying Node
3. Node stores the Host in its registry under the specified HostID
4. NodeManager sends a HostAccept event if successful, or HostReject if failed
5. The Host is now available for message routing and delivery

### Process Attachment

1. Client calls Attach(pid, channel) on a Node
2. Node determines the appropriate Host based on pid.Host
3. Node delegates to the Host's Attach method
4. Host registers the channel for receiving messages addressed to the PID
5. Host returns a cancel function that can be used to detach the receiver

## Usage Patterns

### Creating a Node and Host

```go
// Create a Host
ctx := context.Background()
hostConfig := HostConfig{
    BufferSize:  100,
    WorkerCount: 4,
    Logger:      logger,
}
host := NewHost(ctx, hostConfig)

// Create a Node
nodeID := "node1"
node := NewNode(nodeID, nil)

// Register the Host with the Node
err := node.RegisterHost("host1", host)
if err != nil {
    // Handle error
}
```

### Registering a Host via Events

```go
// Create a NodeManager
node := NewNode("node1", nil)
bus := eventBus
logger := logger
manager := NewNodeManager(node, bus, logger)
manager.Start(ctx)

// Send host registration event
bus.Send(ctx, event.Event{
    System: api.System,
    Kind:   api.HostRegister,
    Path:   "host1",
    Data:   host,
})
```

### Attaching a Process Receiver

```go
// Create a channel to receive packages
ch := make(chan *api.Package, 10)

// Create a PID
pid := api.PID{
    Node:   "node1",
    Host:   "host1",
    ID:     registry.ID{NS: "ns1", Name: "proc1"},
    UniqID: "uniq1",
}

// Attach the channel to the PID
cancel, err := node.Attach(pid, ch)
if err != nil {
    // Handle error
}
defer cancel() // Detach when done

// Read packages from the channel
for pkg := range ch {
    // Process the package
}
```

### Sending Messages

```go
// Create a Package
pkg := &api.Package{
    PID: pid,
    Messages: []*api.Message{
        {
            Topic: "my.topic",
            Payloads: payload.New("Hello, world!"),
        },
    },
}

// Send the Package
err := node.Send(pkg)
if err != nil {
    // Handle error
}
```

## Error Handling

The system defines several standard errors:

- **ErrAlreadyAttached**: Returned when attempting to attach a receiver to a PID that already has one
- **ErrHostNotFound**: Returned when attempting to route a message to a non-existent host
- **ErrHostAlreadyExists**: Returned when attempting to register a host with an ID that is already in use

Additional error conditions include:

- Invalid host types in the Node registry
- Non-local routing with no upstream configured
- Context cancellation during message sending
- Channel blocking during message delivery (message is dropped)

## Implementation Notes

### Host Implementation

- Uses a worker pool pattern with multiple goroutines for processing message delivery
- Implements a consistent hashing algorithm based on PID.UniqID to ensure all messages for a PID are handled by the same worker
- Uses non-blocking channel sends to prevent deadlocks, with messages dropped if receiver channels are full
- Closes worker goroutines when context is cancelled

### Node Implementation

- Uses sync.Map for thread-safe host registry access
- Implements a three-tiered routing strategy (local, cross-host, cross-node)
- Supports attaching an upstream receiver for hierarchical message routing
- Handles special cases for empty Node IDs in PIDs (treated as local)

### NodeManager Implementation

- Acts as an adapter between the event system and the Node implementation
- Provides administrative control over host registration and removal
- Implements event-based communication for host lifecycle management
- Forwards actual messaging operations to the underlying Node

This specification outlines the PubSub component for the Wippy Runtime, providing a robust distributed messaging infrastructure with support for hierarchical routing, process addressing, and flexible delivery semantics.