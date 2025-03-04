# Event Bus Component Specification

## System Events

The Event Bus component does not define specific system events itself, but rather provides the infrastructure for event-based communication between components in the Pony Runtime.

## Purpose

The Event Bus component provides a robust, concurrent, and pattern-matching event distribution system for the Pony Runtime. It enables loosely coupled communication between components through a publish-subscribe pattern, where publishers and subscribers are decoupled and can operate independently. The component supports hierarchical event routing with wildcard pattern matching, allowing for flexible subscription to event categories.

## Core Concepts

### Event Structure

- **Event**: Fundamental message unit containing System, Kind, Path, and Data fields
- **System**: Identifier for the system or module that events belong to
- **Kind**: Specific type of event within a system
- **Path**: Path identifier for related entity
- **Data**: Payload of the event, containing any relevant data

### Subscribers and Subscriptions

- **Subscriber**: Component that listens for specific events
- **SubscriberID**: Unique identifier for a subscription
- **Pattern Matching**: Supports advanced wildcard patterns for flexible event filtering
- **Event Channels**: Events are delivered to subscribers through Go channels

### Wildcard Patterns

- **Dot Notation**: Events and patterns use dot notation for hierarchical organization (e.g., "system.service.event")
- **Single Wildcard**: The "*" character matches exactly one segment
- **Multi Wildcard**: The "**" pattern matches zero or more segments
- **Alternation**: Parenthesized patterns with pipe separators like "(a|b|c)" match any of the options
- **Mixed Patterns**: Patterns can combine literals, wildcards, and alternations (e.g., "system.*.action.(create|update)")

### Event Routing

- **Bus**: Central event distribution system
- **EventRouter**: Handles routing of events to registered handlers
- **EventHandler**: Processes events matching specific patterns
- **Subscriber**: Helper that simplifies subscribing to and handling events

## Architecture

### Key Components

1. **Bus**
   - Core event distribution mechanism
   - Manages subscriptions and event delivery
   - Supports thread-safe concurrent operations
   - Provides pattern-based event filtering

2. **Subscriber**
   - Simplifies subscription management
   - Handles event processing in background goroutines
   - Provides automatic cleanup on context cancellation
   - Manages the lifecycle of event channels

3. **EventRouter**
   - Routes events to registered handlers
   - Supports declarative event handling with patterns
   - Manages handler registration and lifecycle
   - Provides error handling for event processing

4. **Wildcard**
   - Implements advanced pattern matching for filtering events
   - Supports hierarchical dot-notation patterns
   - Provides segment-based matching with single and multi-wildcards
   - Enables alternation patterns for matching one of multiple options

5. **Context Integration**
   - Stores and retrieves event bus instance from context
   - Ensures consistent access to the event bus throughout the system
   - Enables component composition through context propagation

## Interface Definitions

### Bus Interface

```go
type Bus interface {
    // Subscribe subscribes a channel to events from a specific system
    Subscribe(context.Context, System, chan<- Event) (SubscriberID, error)
    
    // SubscribeP subscribes a channel to events matching both system and kind patterns
    SubscribeP(context.Context, System, Kind, chan<- Event) (SubscriberID, error)
    
    // Unsubscribe removes a subscription using its SubscriberID
    Unsubscribe(context.Context, SubscriberID)
    
    // Send publishes an event to the bus
    Send(context.Context, Event)
}
```

### EventHandler Interface

```go
type EventHandler interface {
    // Pattern returns the event matching criteria for this handler
    Pattern() Pattern
    
    // Handle processes an event that matches the pattern
    Handle(context.Context, Event) error
}
```

### Pattern Structure

```go
type Pattern struct {
    // System identifies the system category of events to match
    System System
    
    // Kind identifies the specific kind of events to match
    Kind Kind
}
```

### Event Structure

```go
type Event struct {
    // System is the system or module the event originates from
    System System
    
    // Kind is the specific type of the event
    Kind Kind
    
    // Path is the path of the event
    Path Path
    
    // Data is the payload of the event
    Data any
}
```

## Operation Flow

### Event Publishing

1. Publisher creates an Event instance with appropriate System, Kind, Path, and Data fields
2. Publisher calls Bus.Send() with the event and a context
3. Bus delivers the event to all matching subscribers based on their subscription patterns
4. If a subscriber's channel is full or if context is canceled, event delivery is skipped for that subscriber

### Event Subscribing

1. Subscriber creates a channel to receive events
2. Subscriber calls Bus.Subscribe() or Bus.SubscribeP() with appropriate system and kind patterns
3. Bus returns a unique SubscriberID for the subscription
4. Subscriber receives events through the channel
5. Subscriber can unsubscribe by calling Bus.Unsubscribe() with the SubscriberID

### Event Routing

1. Router is initialized with an event bus instance
2. Handlers are registered with the router, each with a pattern to match events
3. When an event matching a handler's pattern is published, the router delivers it to the handler
4. Handler processes the event and returns any error
5. Router handles error logging and continues processing events

## Usage Patterns

### Basic Event Publishing and Subscribing

```go
// Create a new event bus
bus := eventbus.NewBus()

// Create a channel to receive events
eventCh := make(chan event.Event, 10)

// Subscribe to events from a specific system
ctx := context.Background()
subID, err := bus.Subscribe(ctx, "system-name", eventCh)
if err != nil {
    // Handle error
}

// Publish an event
event := event.Event{
    System: "system-name",
    Kind:   "event-kind",
    Path:   "entity-path",
    Data:   someData,
}
bus.Send(ctx, event)

// Process received events
go func() {
    for evt := range eventCh {
        // Process event
    }
}()

// Later, unsubscribe when done
bus.Unsubscribe(ctx, subID)
```

### Using Subscriber Helper

```go
// Create a new event bus
bus := eventbus.NewBus()

// Create a subscriber with a handler function
subscriber, err := eventbus.NewSubscriber(
    ctx,
    bus,
    "system-name",
    "event-kind.*",
    func(evt event.Event) {
        // Process event
    },
)
if err != nil {
    // Handle error
}

// Event processing happens automatically in background goroutines

// Close subscriber when done
subscriber.Close()
```

### Using EventRouter with Handlers

```go
// Create event handlers
handler1 := eventbus.NewBaseHandler(
    eventbus.Pattern{System: "system-name", Kind: "kind1.*"},
    func(ctx context.Context, evt event.Event) error {
        // Process event
        return nil
    },
)

handler2 := eventbus.NewBaseHandler(
    eventbus.Pattern{System: "system-name", Kind: "kind2.(create|update)"},
    func(ctx context.Context, evt event.Event) error {
        // Process event
        return nil
    },
)

handler3 := eventbus.NewBaseHandler(
    eventbus.Pattern{System: "system-name", Kind: "service.**.status"},
    func(ctx context.Context, evt event.Event) error {
        // Process events from any depth of service hierarchy
        // that ends with "status"
        return nil
    },
)

// Create and start router
router, err := eventbus.StartRouter(
    ctx,
    bus,
    eventbus.WithHandlers(handler1, handler2, handler3),
    eventbus.WithLogger(logger),
)
if err != nil {
    // Handle error
}

// Events are automatically routed to appropriate handlers

// Stop router when done
router.Stop()
```

### Context Integration

```go
// Store event bus in context
ctx := event.WithBus(context.Background(), bus)

// Later, retrieve event bus from context
bus := event.GetBus(ctx)
if bus != nil {
    bus.Send(ctx, evt)
}
```

## Error Handling

The Event Bus component handles various error conditions:

- **Context Cancellation**: Operations are skipped if context is canceled
- **Full Subscriber Channels**: Events are skipped for subscribers with full channels
- **Handler Errors**: EventRouter logs errors but continues processing
- **Subscription Errors**: Errors are returned for failed subscriptions
- **Closed Bus**: Operations on a closed bus return appropriate errors

## Implementation Notes

### Bus Implementation

- Uses a dedicated goroutine for processing all operations
- Employs buffered channels for handling subscription and event actions
- Maintains thread safety with sync.Map for subscriber management
- Implements graceful shutdown with proper cleanup of resources
- Uses wildcard pattern matching for flexible event filtering
- Handles concurrent operations with non-blocking message delivery

### Subscriber Implementation

- Manages subscription lifecycle automatically
- Handles event processing in background goroutines
- Implements automatic cleanup on context cancellation
- Ensures proper synchronization during shutdown

### EventRouter Implementation

- Supports dynamic handler registration
- Manages handler subscriptions throughout their lifecycle
- Provides error handling and logging for event processing
- Implements concurrent operation with proper synchronization

### Wildcard Implementation

- Implements pattern matching using recursive segment-based comparisons
- Parses patterns into segments based on dot delimiter
- Handles special wildcards with different matching semantics:
   - "*" matches exactly one segment
   - "**" matches zero or more segments (greedy matching)
- Supports alternation with (a|b|c) syntax to match one of multiple options
- Optimizes for common patterns to ensure efficient matching
- Handles edge cases like empty patterns and boundary conditions

### Wildcard Pattern Matching

- Implements a sophisticated hierarchical pattern matching system
- Supports single "*" wildcards that match exactly one segment
- Supports "**" wildcards that match zero or more segments (greedy matching)
- Supports alternation patterns with (a|b) syntax for matching one of multiple options
- Enables dot-separated hierarchical event naming (e.g., "system.service.action")
- Allows for precise control over event filtering with mixed patterns
- Employs recursive pattern matching for handling complex nested patterns