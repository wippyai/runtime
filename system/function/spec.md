# Function Component Specification

## Purpose
The Function component provides a framework for registering, managing, and executing asynchronous functions within the Pony Runtime. It enables dynamic registration of handlers that can process tasks and return results through channels.

## Core Concepts

### Function Model
- **Function**: Asynchronous handler that receives a task and returns results through a channel
- **Task**: Unit of work to be executed, containing an identifier and input payloads
- **Result**: Outcome of execution, containing either a payload or an error
- **Registry**: Central manager for function registration and execution

## Architecture

### Registry
The Registry is the central component:
- Manages function registration and deregistration via events
- Handles function lookup and execution
- Provides context integration for accessing registry services
- Thread-safe handler storage using sync.Map

### Function Type
The core function signature:
```go
type Func func(context.Context, runtime.Task) (chan *runtime.Result, error)
```
- Takes a context and task as input
- Returns a result channel and any immediate errors
- Result channel allows for streaming multiple results

## Event-Based Management

### Registration Events
- `function.register`: Register a new function handler
- `function.delete`: Remove an existing function handler
- `function.accept`: Confirmation of successful registration/deletion
- `function.reject`: Notification of failed registration/deletion

## Message Flow

### Function Registration
1. Client sends `function.register` event with function implementation
2. Registry validates and stores the function
3. Registry sends `function.accept` or `function.reject` response

### Function Execution
1. Client calls `registry.Call()` with task and context
2. Registry looks up handler by task ID
3. Registry executes function with proper context
4. Results are streamed through the returned channel

## Context Integration

### Registry Access
- `WithFunctions(ctx, registry)`: Store registry in context
- `GetRegistry(ctx)`: Retrieve registry from context

### PID and Host Integration
During execution, the registry:
- Adds PID to context with function host ID and unique identifier
- Adds Host to context for message handling

## Thread Safety
- Concurrent handler registration and deletion is supported
- Function execution can happen concurrently
- Context propagation maintains isolation between executions

## Use Cases

### Registering a Function
```go
handler := func(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
    resultCh := make(chan *runtime.Result, 1)
    go func() {
        // Process the task
        resultCh <- &runtime.Result{
            Payload: payload.New("processed result"),
        }
        close(resultCh)
    }()
    return resultCh, nil
}

bus.Send(ctx, event.Event{
    System: function.System,
    Kind:   function.Register,
    Path:   "namespace:name",
    Data:   function.Func(handler),
})
```

### Executing a Function
```go
registry := function.GetRegistry(ctx)
resultCh, err := registry.Call(ctx, runtime.Task{
    ID:       registry.ID{NS: "namespace", Name: "name"},
    Payloads: []payload.Payload{payload.New("input data")},
})

if err != nil {
    // Handle error
}

for result := range resultCh {
    if result.Error != nil {
        // Handle execution error
    } else {
        // Process result payload
    }
}
```

This specification defines the Function component that provides a flexible framework for managing and executing asynchronous tasks within the Pony Runtime.