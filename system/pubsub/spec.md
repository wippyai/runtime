# PubSub Component Overview

The PubSub component implements a local publish–subscribe system that enables asynchronous message delivery between
services on a node. It is composed of several interrelated parts that work together to provide robust messaging with
configurable delivery and retry semantics.

## Core Parts

### Host

The **Host** is the central building block for message delivery on a single node. Its key responsibilities include:

- **Configuration and Setup:**  
  The Host is initialized with a `HostConfig` that defines parameters like the internal job channel buffer size, number
  of worker goroutines, retry and delivery timeouts, and a logger for operational events. If specific timeouts or a
  logger are not provided, sensible defaults are used. citeturn1file0

- **Receiver Attachment:**  
  The Host allows receivers to attach via an `Attach` method (or `AttachWithPID` for consumers that need both the
  sender’s PID and the batch payload). Only one receiver may be attached per PID; duplicate attachments return an error.
  Once attached, a receiver can later be detached either explicitly or via a cancel function returned during attachment.

- **Asynchronous Message Sending:**  
  When sending a message via the `Send` method, the Host enqueues a send job that encapsulates the destination PID,
  message batch, and a context. It attempts an immediate send into an internal job channel. If the channel is full, the
  Host will retry sending for a configurable duration before timing out. citeturn1file0

- **Worker Processing and Delivery:**  
  Worker goroutines are spawned upon initialization. Each worker continuously processes queued send jobs. Based on the
  type of receiver channel attached (either a plain batch channel or a PIDBatch channel), the worker delivers the
  message either immediately or with a timeout-based fallback. Delivery timeouts ensure that messages are dropped if the
  receiver does not accept them within a set period.

### Node

The **Node** aggregates one or more Hosts and acts as a routing layer for messages. Its primary functions include:

- **Local Message Routing:**  
  When a message is sent, the Node checks if the destination is local (i.e., the PID’s node is empty or matches the
  Node’s identifier). If local, it retrieves the proper Host and delegates the send operation. If the target Host is not
  found or is of an invalid type, the Node returns an error. citeturn1file4

- **Upstream Delegation:**  
  For non-local messages, the Node can forward messages to an upstream component (if configured). If no upstream is
  available, the Node returns an error indicating that the message cannot be delivered.

- **Receiver Attachment Delegation:**  
  Similarly, attachment requests for local messages are delegated to the appropriate Host. Non-local attachment requests
  are rejected if no upstream exists.

### Node Manager

The **NodeManager** adds an event-driven management layer over a Node. Its responsibilities include:

- **Event Handling for Host Registration:**  
  The NodeManager listens to host-related events (for example, registering or removing a host) from an event bus. When a
  host registration event is received, the NodeManager validates the payload, registers the host with the underlying
  Node, and sends an accept or reject response event. This ensures that the node’s host registry remains consistent.

- **Delegating Message Operations:**  
  The NodeManager provides methods such as `Send` and `Attach` that directly delegate to the underlying Node. This
  abstraction allows other parts of the system to interact with a NodeManager without worrying about the internal
  routing details.

- **Graceful Shutdown:**  
  The NodeManager can start and stop its event subscriptions cleanly, ensuring that all host registration events are
  properly handled and that the system can be shutdown without leaving dangling subscriptions. citeturn1file2

## Component Interactions

1. **Message Flow:**
    - A client calls the NodeManager’s `Send` method.
    - The NodeManager delegates the call to the Node, which checks if the destination is local.
    - For local messages, the Node retrieves the appropriate Host and calls its `Send` method.
    - The Host enqueues a send job that is eventually processed by a worker, delivering the message to the attached
      receiver.

2. **Receiver Setup:**
    - Services attach receiver channels to the Host via `Attach` or `AttachWithPID`.
    - The Host stores these channels in a concurrent map keyed by the PID.
    - When a message is delivered, the Host determines the receiver type and attempts to send the message accordingly.

3. **Event-Driven Management:**
    - The NodeManager subscribes to registration and removal events on the event bus.
    - Upon receiving an event, it validates the payload and updates the Node’s registry by calling `RegisterHost` or
      `UnregisterHost`.
    - After processing, the NodeManager sends an acceptance or rejection event back on the bus.

## Example Usage Flow

Consider a scenario where a service on a node wants to send messages to a specific process:

- **Setup:**  
  A Host is created with a defined buffer size and worker count. A service attaches a receiver channel to a PID using
  the Host’s `Attach` method.

- **Sending a Message:**  
  When the service calls `Send` on the NodeManager with a message batch, the NodeManager forwards the request to the
  Node. The Node routes the message to the appropriate Host, where a worker processes the send job. The message is then
  delivered to the receiver’s channel, ensuring asynchronous and reliable communication.

- **Management:**  
  Simultaneously, if a new host needs to be registered or an existing host removed, events are published on the event
  bus. The NodeManager listens to these events, updates the Node’s host registry, and responds accordingly.