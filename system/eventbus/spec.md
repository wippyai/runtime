# Event Bus System Specification

## Overview

A high-performance, thread-safe event bus implementation in Go that provides publish/subscribe messaging patterns with
advanced filtering capabilities.

## Core Components

### Bus

The central message distribution system implementing the publisher/subscriber pattern.

#### Key Features:

- Thread-safe operations for subscribe, unsubscribe, and send
- Support for wildcard pattern matching on system and kind fields
- Buffered channels for efficient event handling
- Graceful shutdown with proper cleanup
- Context-aware operations for timeout and cancellation support

#### Usage:

```go
bus := NewBus()
defer bus.Stop()

// Subscribe to events
ch := make(chan events.Event)
subID, err := bus.Subscribe(ctx, "system-name", ch)

// Subscribe with pattern matching
subID, err := bus.SubscribeP(ctx, "system-name", "event.*", ch)

// Send events
bus.Send(ctx, events.Event{...})
```

### Router

A higher-level routing layer that provides pattern-based event distribution.

#### Key Features:

- Pattern-based event routing to specific handlers
- Support for multiple concurrent handlers
- Built-in error handling and logging
- Middleware-style event processing
- Graceful shutdown coordination

#### Usage:

```go
router, err := StartRouter(ctx, bus,
WithHandlers(handler1, handler2),
WithLogger(logger))
defer router.Stop()
```

### Subscriber

A helper component that simplifies event subscription and handling.

#### Key Features:

- Simplified event subscription interface
- Automatic cleanup on context cancellation
- Goroutine-safe event processing
- Support for both simple and pattern-based subscriptions

#### Usage:

```go
subscriber, err := NewSubscriber(ctx, bus, "system", "kind.*",
func (evt Event) {
// Handle event
})
defer subscriber.Close()
```

## Event Pattern Matching

The system implements a sophisticated wildcard pattern matching system for event routing that supports hierarchical
dot-notation patterns.

### Pattern Types

1. Single Wildcard (`*`)
    - Matches exactly one segment
    - Example: `a.*` matches `a.b` but not `a.b.c`

2. Double Wildcard (`**`)
    - Matches zero or more segments
    - Can appear anywhere in the pattern
    - Example: `a.**` matches `a`, `a.b`, `a.b.c`, etc.

3. Exact Matches
    - Direct string comparison
    - Example: `a.b.c` only matches `a.b.c`

4. Alternations (`(a|b)`)
    - Matches any one of the provided options
    - Example: `(a|b).state.*` matches both `a.state.x` and `b.state.x`

### Pattern Rules

1. Segment Boundaries
    - Patterns are split on dots (`.`)
    - Each segment is matched independently
    - Example: `a.*.c` has three segments

2. Wildcard Behavior
    - `*` consumes exactly one segment
    - `**` is greedy but backtracks to find matches
    - `**` at pattern end matches all remaining segments

### Examples

```
Pattern: "*.state.*"
✓ Matches: "a.state.x"
✗ No Match: "b.state.y.z"   (too many segments)
✗ No Match: "b.event.y"     (middle segment must be "state")

Pattern: "a.**"
✓ Matches: "a"
✓ Matches: "a.b"
✓ Matches: "a.b.c.d"

Pattern: "(a|b).*.state"
✓ Matches: "a.x.state"
✓ Matches: "b.y.state"
✗ No Match: "c.x.state"     (first segment must be "a" or "b")
```

### Performance Characteristics

- Pattern compilation is done once at subscription time
- Matching is performed using recursive matching with backtracking
- Alternations use simple array lookups
- Memory efficient with minimal allocations during matching

## Performance Characteristics

- Uses buffered channels for non-blocking event distribution
- Concurrent event processing with proper synchronization
- Efficient pattern matching for event routing
- Graceful handling of slow consumers
- Built-in backpressure handling

## Error Handling

- Context-based cancellation and timeout support
- Proper error propagation
- Graceful shutdown on errors
- Error logging with configurable logger

## Thread Safety

The implementation is fully thread-safe and handles:

- Concurrent subscriptions/unsubscriptions
- Concurrent event publishing
- Safe shutdown with multiple active subscribers
- Race-free event distribution

## Best Practices

1. Always use context for operation control:

```go
ctx, cancel := context.WithTimeout(context.Background(), timeout)
defer cancel()
```

2. Implement proper shutdown:

```go
defer bus.Stop()
defer subscriber.Close()
```

3. Handle backpressure with buffered channels:

```go
ch := make(chan events.Event, bufferSize)
```

4. Use pattern matching effectively:

```go
// Specific events
"users.created"
// All user events
"users.*"
// All system events
"system.**"
```

## Limitations

- Events are delivered at-least-once (no exactly-once guarantee)
- No persistent event storage
- No built-in event ordering guarantees
- No automatic message retry mechanism

## Security Considerations

- No built-in authentication/authorization
- No encryption of event payloads
- Should be used within trusted boundaries
- Consider implementing middleware for security checks

## Testing

The system includes comprehensive tests covering:

- Basic functionality
- Concurrent operations
- Error conditions
- Pattern matching
- Performance under load
- Graceful shutdown
- Context cancellation