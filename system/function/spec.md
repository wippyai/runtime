# Functions System Specification

## Overview

A dynamic function registry and execution system that provides distributed task handling capabilities through an event-based architecture. The system allows runtime registration, removal, and execution of function handlers.

## Core Components

### FunctionRegistry

The central component managing function registration and execution.

#### Key Features:
- Dynamic handler registration and removal
- Event-driven communication
- Thread-safe handler storage
- Context-aware execution
- Graceful shutdown support
- Error propagation and handling

#### Usage:

```go
// Create new registry
registry := NewExecutor(bus, logger)

// Start the registry
registry.Start(ctx)

// Register a handler
bus.Send(ctx, events.Event{
    System: function.System,
    Kind:   function.RegisterFunctionHandler,
    Path:   "namespace:handler",
    Data:   handlerFunc,
})

// Execute a task
resultChan, err := registry.Call(ctx, task)
```

### Function Handler

A function that processes tasks and returns results through a channel.

#### Interface:
```go
type Func func(context.Context, runtime.Task) (chan *runtime.Result, error)
```

#### Key Features:
- Stream-based result handling
- Context-aware execution
- Error propagation
- Asynchronous processing
- Task-based input

## Event Communication

The system uses an event bus for handler registration and management.

### Event Types

1. Registration Events
    - `function.register`: Register a new handler
    - `function.remove`: Remove an existing handler
    - `function.accept`: Handler registration accepted
    - `function.reject`: Handler registration rejected

### Event Flow

1. Handler Registration:
   ```
   Client -> Register Event -> Registry -> Accept/Reject Event -> Client
   ```

2. Handler Removal:
   ```
   Client -> Remove Event -> Registry -> Accept/Reject Event -> Client
   ```

## Task Execution

### Task Structure
```go
type Task struct {
    Handler  registry.ID      // Target handler identifier
    Payloads []payload.Payload // Input data
}
```

### Result Structure
```go
type Result struct {
    Payload payload.Payload // Output data
    Error   error          // Execution error if any
}
```

### Execution Flow
1. Task submission through `Call()`
2. Handler lookup by ID
3. Context preparation and validation
4. Handler execution
5. Result streaming through channel

## Handler Management

### Registration Process
1. Client sends registration event with handler function
2. Registry validates handler
3. Handler stored in thread-safe map
4. Accept/Reject event sent back to client

### Removal Process
1. Client sends removal event with handler ID
2. Registry checks handler existence
3. Handler removed from storage
4. Accept/Reject event sent back to client

## Error Handling

### Types of Errors
- Handler not found
- Invalid handler function
- Registration failures
- Execution errors
- Context cancellation

### Error Propagation
1. Immediate errors returned directly
2. Runtime errors sent through result channel
3. Context cancellation handled at all levels

## Performance Characteristics

- Thread-safe handler storage using sync.Map
- Non-blocking event-based communication
- Asynchronous result streaming
- Concurrent handler registration and execution
- Context-based cancellation support

## Best Practices

1. Always provide context for operation control:
```go
ctx, cancel := context.WithTimeout(context.Background(), timeout)
defer cancel()
```

2. Handle result streaming properly:
```go
resultChan, err := registry.Call(ctx, task)
if err != nil {
    return err
}
for result := range resultChan {
    // Process result
}
```

3. Implement proper error handling:
```go
if err := registry.Start(ctx); err != nil {
    logger.Error("failed to start registry", zap.Error(err))
    return err
}
```

4. Use appropriate timeouts:
```go
ctx, cancel := context.WithTimeout(parentCtx, 5*time.Second)
defer cancel()
```

## Limitations

- No persistent handler storage
- No built-in retry mechanism
- No automatic handler recovery
- No distributed handler coordination
- No built-in rate limiting

## Security Considerations

- No built-in authentication/authorization
- Handler functions run with registry privileges
- Input validation responsibility lies with handlers
- No sandboxing of handler execution
- Consider implementing middleware for security checks

## Testing

The system includes comprehensive tests covering:

- Basic functionality
- Concurrent operations
- Error conditions
- Handler lifecycle
- Performance under load
- Context cancellation
- Event communication
- Result streaming

## Handler Implementation Guidelines

1. Always respect context cancellation:
```go
func handler(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
    resultChan := make(chan *runtime.Result)
    go func() {
        defer close(resultChan)
        select {
        case <-ctx.Done():
            return
        case resultChan <- &runtime.Result{
            Payload: processTask(task),
        }:
        }
    }()
    return resultChan, nil
}
```

2. Proper channel management:
```go
func handler(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
    resultChan := make(chan *runtime.Result, 1) // Buffered for non-blocking
    go func() {
        defer close(resultChan)
        // Process and send results
    }()
    return resultChan, nil
}
```

3. Error handling:
```go
func handler(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
    if err := validateTask(task); err != nil {
        return nil, err // Return immediate errors directly
    }
    resultChan := make(chan *runtime.Result)
    go func() {
        defer close(resultChan)
        if result, err := processTask(task); err != nil {
            resultChan <- &runtime.Result{Error: err} // Send runtime errors through channel
        } else {
            resultChan <- &runtime.Result{Payload: result}
        }
    }()
    return resultChan, nil
}
```