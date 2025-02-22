# Unit of Work Usage Specification

## Overview

The Unit of Work (UoW) pattern provides a thread-safe mechanism for managing resource lifecycles, cleanup operations, and shared state. Each UoW has its own internal context for lifecycle management, independent of the parent context.

## Core Concepts

### 1. Context Management
- Each UoW has its own internal context, independent of the parent context
- The internal context is canceled when UoW is closed
- UoW.Context() can be used for operations that should be bound to UoW lifecycle
- UoW.Done() is tied to the internal context's cancellation

### 2. Resource Lifecycle Management
- Cleanup functions are executed in LIFO order (last added = first cleaned)
- All cleanup operations should be registered immediately after resource creation
- Resource cleanup follows a stack-like pattern
- Context cancellation triggers cleanup process

### 3. Thread-Safe Shared State
- Thread-safe state operations via sync.Map
- Access methods:
   - `Get(key any) (any, bool)`
   - `GetOrStore(key any, value any) (any, bool)`
   - `Set(key any, value any)`
   - `Delete(key any)`
   - `Range(f func(key, value any) bool)`
   - `CompareAndSwap(key any, old any, new any) bool`
   - `CompareAndDelete(key any, old any) bool`

## Usage Guidelines

### Context Management Pattern

```go
// Creating a new UoW context
ctx, uw := uow.WithContext(parentCtx)
defer func() {
    if err := uw.Close(); err != nil {
        log.Error("failed to close unit of work", zap.Error(err))
    }
}()

// Using UoW's internal context for operations
go func() {
    select {
    case <-uw.Context().Done():
        // UoW-specific cleanup
    case <-parentCtx.Done():
        // Parent context cleanup
    }
}()
```

### Resource Management Pattern

```go
func manageResource(uw *UnitOfWork) error {
    // Create resource using UoW context
    resource, err := NewResource(uw.Context())
    if err != nil {
        return err
    }

    // Register cleanup
    uw.Add(func() error {
        return resource.Close()
    })

    // Start background operation with UoW context
    go func() {
        select {
        case <-uw.Context().Done():
            // Resource-specific cleanup
            return
        case data := <-resource.Data():
            // Process data
        }
    }()

    return nil
}
```

## Lua Module Integration

### 1. Context-Aware Module Operations

```go
func luaOperation(L *lua.LState) int {
    uw := uow.FromContext(L.Context())
    if uw == nil {
        L.RaiseError("unit of work missing")
        return 0
    }

    // Use UoW's internal context for operations
    go func() {
        ctx := uw.Context()
        select {
        case <-ctx.Done():
            return
        case result := <-processData(ctx):
            handleResult(result)
        }
    }()

    return 0
}
```

### 2. Resource Management in Modules

```go
func luaResourceOperation(L *lua.LState) int {
    uw := uow.FromContext(L.Context())
    if uw == nil {
        L.RaiseError("unit of work missing")
        return 0
    }

    // Create resources using UoW context
    resource := createResource(uw.Context())
    uw.Add(func() error {
        return resource.Close()
    })

    return 0
}
```

## Stream Operations Example

```go
func handleStream(uw *UnitOfWork, reader io.ReadCloser) (*stream.Stream, error) {
    // Create stream with UoW context
    str, err := stream.NewStream(uw.Context(), reader, nil)
    if err != nil {
        return nil, err
    }

    // Register cleanup
    uw.Add(func() error {
        return str.Close()
    })

    // Process stream using UoW context
    go func() {
        for {
            select {
            case <-uw.Context().Done():
                return
            default:
                chunk, err := str.ReadChunk()
                if err != nil {
                    if err == io.EOF {
                        return
                    }
                    // Handle error
                    return
                }
                processChunk(chunk)
            }
        }
    }()

    return str, nil
}
```

## Critical Rules

1. **Context Usage**
   - Use UoW.Context() for operations that should be bound to UoW lifecycle
   - Always check for context cancellation in long-running operations
   - Parent context cancellation should be handled separately if needed

2. **Cleanup Order**
   - Resources are cleaned up in LIFO order
   - Context cancellation triggers cleanup process
   - Register cleanup functions immediately after resource creation

3. **Goroutine Management**
   - Use UoW.Context() for goroutine cancellation
   - Always handle both UoW context and parent context if needed
   - Clean up resources properly when context is canceled

## Best Practices

### 1. Context Management
```go
func operationWithContexts(parentCtx context.Context) error {
    ctx, uw := uow.WithContext(parentCtx)
    
    // Start operation with both contexts
    go func() {
        select {
        case <-parentCtx.Done():
            // Handle parent cancellation
        case <-uw.Context().Done():
            // Handle UoW cleanup
        case result := <-operation(uw.Context()):
            // Process result
        }
    }()

    return nil
}
```

### 2. Resource Cleanup
```go
func manageMultipleResources(uw *UnitOfWork) error {
    // Resources are created and registered in order
    resource1 := createResource1(uw.Context())
    uw.Add(resource1.Close)

    resource2 := createResource2(uw.Context(), resource1)
    uw.Add(resource2.Close)

    // Start operation with proper context
    go operate(uw.Context(), resource1, resource2)

    return nil
}
```

### 3. Module State Management
```go
type ModuleState struct {
    ctx    context.Context
    cancel context.CancelFunc
    data   atomic.Value
}

func initModuleState(uw *UnitOfWork) *ModuleState {
    // Create state with UoW context
    ctx, cancel := context.WithCancel(uw.Context())
    state := &ModuleState{
        ctx:    ctx,
        cancel: cancel,
    }
    
    // Register cleanup
    uw.Add(func() error {
        cancel()
        return nil
    })

    return state
}
```

## Testing

### 1. Context Cancellation Testing
```go
func TestContextCancellation(t *testing.T) {
    ctx, uw := uow.WithContext(context.Background())
    
    done := make(chan struct{})
    go func() {
        <-uw.Context().Done()
        close(done)
    }()
    
    uw.Close()
    
    select {
    case <-done:
        // Test passed
    case <-time.After(time.Second):
        t.Error("context not canceled")
    }
}
```

### 2. Resource Cleanup Testing
```go
func TestResourceCleanup(t *testing.T) {
    ctx, uw := uow.WithContext(context.Background())
    
    cleanup1Called := false
    cleanup2Called := false
    
    uw.Add(func() error {
        cleanup1Called = true
        return nil
    })
    uw.Add(func() error {
        cleanup2Called = true
        return nil
    })
    
    uw.Close()
    
    assert.True(t, cleanup2Called) // Should be called first
    assert.True(t, cleanup1Called) // Should be called second
}
```

## Migration Guide

When migrating existing code to use the UoW with internal context:

1. Replace direct context usage with UoW.Context() where appropriate:
   ```go
   // Old
   operation(parentCtx)
   
   // New
   operation(uw.Context())
   ```

2. Update goroutine management:
   ```go
   // Old
   select {
   case <-parentCtx.Done():
       return
   }
   
   // New
   select {
   case <-uw.Context().Done():
       return
   }
   ```

3. Review resource cleanup registration:
   - Ensure cleanup functions are registered immediately
   - Verify LIFO order maintains proper dependency handling
   - Update cleanup functions to handle context cancellation

4. Update tests to verify:
   - Context cancellation behavior
   - Cleanup order
   - Resource management
   - Goroutine lifecycle