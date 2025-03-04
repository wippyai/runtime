# Supervisor Component Specification

## System Events

The Supervisor component communicates through the following event types:

- **System**: `"supervisor"` - Identifies the supervisor system in the event context
- **Register**: `"supervisor.service.register"` - Event for registering a new service
- **Remove**: `"supervisor.service.remove"` - Event for removing a service
- **Update**: `"supervisor.service.status"` - Event for updating service status
- **Start**: `"supervisor.service.start"` - Event to start a service
- **Stop**: `"supervisor.service.stop"` - Event to stop a service

## Purpose

The Supervisor component provides a robust lifecycle management system for services within the Pony Runtime. It handles service registration, orchestrated startup and shutdown, failure detection, automatic recovery, and dependency management, ensuring services are started and stopped in the correct order.

## Core Concepts

### Service Lifecycle

- **Services**: Implementations of functionality that can be started, monitored, and stopped
- **Lifecycle States**: Services transition through states including Unknown, Starting, Running, Stopping, Stopped, Failed, and Exited
- **Controllers**: Individual managers for each service that handle state transitions and recovery
- **Retry Policies**: Configurable strategies for automatic recovery from failures
- **Terminal Errors**: Special errors (`ErrTerminated`, `ErrExit`) that signal a service should not be restarted

### Dependency Management

- **Dependencies**: Services can declare dependencies on other services
- **Sequencing**: Services are started and stopped in the correct dependency order
- **Parallel Execution**: Independent services or dependency levels can be processed concurrently
- **Coordinated Shutdown**: When stopping, dependents are stopped before dependencies

### Transaction Support

- **Registry Transactions**: Service registration and removal operations are grouped into atomic transactions
- **Commit Protocol**: Changes are applied only when transactions are committed
- **Rollback**: Transactions can be discarded to maintain system stability

## Architecture

### Key Components

1. **Supervisor**
   - Central registry for service management
   - Orchestrates service operations based on dependencies
   - Processes events for registration, state changes, and lifecycle control
   - Manages transaction-based configuration changes

2. **Controller**
   - Manages the lifecycle of an individual service
   - Handles start/stop/failure states and retry logic
   - Monitors service health through a details channel
   - Implements stable threshold for determining service reliability

3. **Sequencer**
   - Determines correct dependency ordering for operations
   - Supports parallel execution of independent services
   - Ensures services are started in dependency order (dependencies first)
   - Ensures services are stopped in reverse dependency order (dependents first)

4. **Event Integration**
   - Receives events for service registration and lifecycle operations
   - Publishes service state changes through the event bus
   - Supports event-driven control of service lifecycle

## Operation Flow

### Service Registration

1. Begin a transaction through the registry system
2. Register services with their configuration and dependencies
3. Commit the transaction to apply changes
4. Auto-start services and their dependencies are launched in correct order

### Service Startup

1. Service dependencies are first identified and sorted into levels
2. Dependencies are started before dependents, level by level
3. Each level can execute services in parallel for efficient startup
4. Service controllers manage the startup process and monitor state

### Service Monitoring

1. Services communicate state changes through a details channel
2. Controllers process state changes and implement retry policies
3. Services running stably for a threshold period reset their retry counters
4. Services exceeding retry limits are marked as permanently failed

### Service Shutdown

1. Dependent services are stopped before their dependencies
2. Each level is processed in parallel for efficient shutdown
3. Services are given a timeout period to stop gracefully
4. Controllers ensure proper resource cleanup during shutdown

## Service Status

The Supervisor defines the following service status types:

- **Unknown**: `"unknown"` - Service status is currently unknown (initial state)
- **Starting**: `"starting"` - Service is currently starting up
- **Running**: `"running"` - Service is currently running and operational
- **Stopping**: `"stopping"` - Service is in the process of a graceful shutdown
- **Stopped**: `"stopped"` - Service has stopped and is no longer running
- **Exited**: `"exited"` - Service has exited on its own (terminal state)
- **Failed**: `"failed"` - Service has failed and is not running

## Service Configuration

### Lifecycle Configuration

```go
type LifecycleConfig struct {
    // AutoStart determines if the service should be started automatically
    AutoStart bool
    
    // DependsOn lists service IDs this service depends on
    DependsOn []string
    
    // StartTimeout specifies how long to wait for service to start
    StartTimeout time.Duration
    
    // StopTimeout specifies how long to wait for service to stop
    StopTimeout time.Duration
    
    // StableThreshold determines when a service is considered stable
    StableThreshold time.Duration
    
    // RetryPolicy defines how to handle service failures
    RetryPolicy RetryPolicy
}
```

### Retry Policy

```go
type RetryPolicy struct {
    // MaxAttempts limits how many times to retry a failing service
    MaxAttempts int
    
    // InitialDelay sets the base delay before first retry
    InitialDelay time.Duration
    
    // MaxDelay caps the maximum delay between retries
    MaxDelay time.Duration
    
    // Factor determines how quickly the delay increases
    Factor float64
}
```

## Service Interface

Any service managed by the supervisor must implement the `supervisor.Service` interface:

```go
// Service defines the interface that must be implemented by any service managed by the supervisor.
type Service interface {
    // Start initiates the service. Service can post current status to the returned channel.
    // The context passed into start method is primary service context, service must exit if context is done.
    // The status channel needs to stay open while the service is running and only be closed when it's fully stopped or failed.
    Start(ctx context.Context) (<-chan any, error)
    
    // Stop terminates the service. The context passed into stop method is only for graceful stop, service must return error
    // if it cannot stop within the context deadline.
    Stop(ctx context.Context) error
}
```

## Usage Patterns

### Registering a Service

```go
// Create service instance
myService := NewMyService()

// Send registration event
bus.Send(ctx, event.Event{
    System: registry.System,
    Kind:   registry.Begin,
})

bus.Send(ctx, event.Event{
    System: supervisor.System,
    Kind:   supervisor.Register,
    Path:   "my-service",
    Data: &supervisor.Entry{
        Service: myService,
        Config: supervisor.LifecycleConfig{
            AutoStart:       true,
            DependsOn:       []string{"dependency-service"},
            StartTimeout:    5 * time.Second,
            StopTimeout:     5 * time.Second,
            StableThreshold: 30 * time.Second,
            RetryPolicy: supervisor.RetryPolicy{
                MaxAttempts:  3,
                InitialDelay: 100 * time.Millisecond,
                MaxDelay:     5 * time.Second,
                Factor:       2.0,
            },
        },
    },
})

bus.Send(ctx, event.Event{
    System: registry.System,
    Kind:   registry.Commit,
})
```

### Controlling Service Lifecycle

```go
// Start a service
bus.Send(ctx, event.Event{
    System: supervisor.System,
    Kind:   supervisor.Start,
    Path:   "my-service",
})

// Stop a service
bus.Send(ctx, event.Event{
    System: supervisor.System,
    Kind:   supervisor.Stop,
    Path:   "my-service",
})

// Remove a service
bus.Send(ctx, event.Event{
    System: registry.System,
    Kind:   registry.Begin,
})
bus.Send(ctx, event.Event{
    System: supervisor.System,
    Kind:   supervisor.Remove,
    Path:   "my-service",
})
bus.Send(ctx, event.Event{
    System: registry.System,
    Kind:   registry.Commit,
})
```

### Monitoring Service Status

```go
// Get state for a specific service
state, err := supervisor.GetState("my-service")
if err != nil {
    // Handle service not found error
}

// Status could be Unknown, Starting, Running, Stopping, Stopped, Failed, or Exited
fmt.Printf("Service status: %s\n", state.Status)

// Get state for all services
states := supervisor.GetAllStates()
for serviceID, state := range states {
    fmt.Printf("Service %s status: %s\n", serviceID, state.Status)
}
```

### Implementing a Service

```go
type MyService struct {
    // service-specific fields
    statusCh chan any
}

func (s *MyService) Start(ctx context.Context) (<-chan any, error) {
    // Initialize resources
    s.statusCh = make(chan any, 10)
    
    // Start service operations in a goroutine
    go func() {
        // Report initial status
        s.statusCh <- "service started"
        
        // Perform work and report status periodically
        for {
            select {
            case <-ctx.Done():
                return
            case <-time.After(time.Minute):
                // Report health metrics or status updates
                s.statusCh <- map[string]interface{}{
                    "connections": 42,
                    "memory_mb":   128,
                }
            }
        }
    }()
    
    return s.statusCh, nil
}

func (s *MyService) Stop(ctx context.Context) error {
    // Graceful shutdown logic
    s.statusCh <- "shutting down"
    
    // Clean up resources
    close(s.statusCh)
    return nil
}
```

## Error Handling

The supervisor handles various error conditions:

- **Service Start Failures**: Retried according to the retry policy
- **Service Stop Timeouts**: Logged and forcefully terminated
- **Missing Dependencies**: Prevented from starting services with unmet dependencies
- **Terminal Errors**: Special errors that indicate a service should not be retried:
   - `ErrTerminated` - Returned when a service is terminated and should not be restarted
   - `ErrExit` - Returned when a service exits on its own and should not be restarted

## Implementation Notes

### Controller State Management

- Controllers maintain atomic state updates with proper locking
- States include the current status, desired status, and retry counts
- State updates are broadcast through the event system

### Sequencer Algorithm

- Uses a dependency graph to determine proper execution order
- Computes dependency levels for parallel execution
- Inverts dependency relationships for stop operations

### Transaction Safety

- Services are only started after a transaction is committed
- Multiple edits can be applied atomically
- Transactions can be discarded if configuration is invalid

This specification outlines the Supervisor component for the Pony Runtime, providing a comprehensive framework for service lifecycle management with dependency-aware orchestration and fault tolerance.