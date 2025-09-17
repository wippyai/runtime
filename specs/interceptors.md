# Function Execution Interceptors

## Overview

The interceptor system provides a way to add cross-cutting concerns to function execution in a modular and configurable way. It allows for adding functionality like tracing, retries, rate limiting, and more without modifying the core function code.

## Architecture

### Core Components

```
Function Execution System
├── Options Layer
│   ├── Common Options Interface
│   ├── Options Validator
│   └── Context Management
│
├── Interceptor System
│   ├── Interceptor Interface
│   ├── Interceptor Chain
│   └── Interceptor Registry
│
├── Configuration System
│   ├── Module Configuration (_index.yaml)
│   ├── Global Configuration
│   └── Runtime Configuration
│
└── Execution Engine
    ├── Function Executor
    ├── Context Manager
    └── Error Handler
```

## Core Interfaces

### Interceptor Interface

```go
type Interceptor interface {
    // Handle processes the execution and calls next() to continue the chain
    Handle(ctx context.Context, next func(context.Context) (*runtime.Result, context.Context)) (*runtime.Result, context.Context)
}
```

The interceptor interface is designed to:
- Process function execution in a chain
- Maintain context throughout the execution
- Return both the result and updated context
- Support cancellation and timeout propagation
- Enable cross-cutting concerns like tracing, retries, and rate limiting

### Options Interface

```go
type Options struct {
    Retry     RetryOptions     `json:"retry,omitempty"`
    RateLimit RateLimitOptions `json:"ratelimit,omitempty"`
    Timeout   TimeoutOptions   `json:"timeout,omitempty"`
}

type RetryOptions struct {
    MaxAttempts int `json:"attempts"`
}

type RateLimitOptions struct {
    RequestsPerSecond int `json:"rps"`
    Burst            int `json:"burst"`
}

type TimeoutOptions struct {
    Timeout Duration `json:"timeout"`
}
```

## Configuration

Interceptors can be configured at multiple levels, following a hierarchical structure:

1. System Defaults
2. Global Configuration
3. Module Configuration (_index.yaml)
4. Function Configuration
5. Runtime Options

### Example Configuration

```yaml
# _index.yaml
version: "1.0"
namespace: app.interceptor.demo

meta:
  depends_on: [ "ns:system" ]
  comment: "Interceptor Demo Application"

entries:
  - name: interceptor_demo_otel
    kind: function.lua
    meta:
      comment: "interceptor demo otel"
    source: file://otel.lua
    method: handler
    modules: [ http ]
    pool:
      size: 2
      workers: 4

  - name: interceptor_demo_retry
    kind: function.lua
    meta:
      comment: "interceptor demo retry"
      options:
        retry:
          attempts: 10
    source: file://retry.lua
    method: handler
    modules: [ http ]
    pool:
      size: 2
      workers: 4

  - name: interceptor_demo_ratelimit
    kind: function.lua
    meta:
      comment: "interceptor demo ratelimit"
      options:
        ratelimit:
          rps: 1
          burst: 1
    source: file://ratelimit.lua
    method: handler
    modules: [ http ]
    pool:
      size: 2
      workers: 4

  - name: interceptor_demo_timeout
    kind: function.lua
    meta:
      comment: "interceptor demo timeout"
      options:
        timeout:
          timeout: 200ms
    source: file://timeout.lua
    method: handler
    modules: [ http, time ]
    pool:
      size: 2
      workers: 4
```

## Built-in Interceptors

### OpenTelemetry (OTEL)

Provides distributed tracing and metrics collection.

```go
type OTelInterceptor struct {
    tracer trace.Tracer
}
```

Features:
- Automatic span creation for function execution
- Support for both new spans and sub-spans
- PID-based span naming and attributes
- Error recording and status tracking
- Context propagation
- Span kind specification (Server/Internal)

#### Environment Variables

The OpenTelemetry interceptor can be configured using the following environment variables:

- `OTEL_ENDPOINT`: The endpoint for the OpenTelemetry collector (e.g., "localhost:4317")
- `OTEL_SERVICE_NAME`: The name of the service for tracing (default: "wippy-runtime")
- `OTEL_SERVICE_VERSION`: The version of the service (default: "1.0.0")

Example:
```bash
export OTEL_ENDPOINT="localhost:4317"
export OTEL_SERVICE_NAME="wippy"
export OTEL_SERVICE_VERSION="1.0.0"
```

If `OTEL_ENDPOINT` is not set, the interceptor will use a no-op tracer and tracing will be disabled.

### Retry

Handles automatic retries for failed function executions.

```go
type RetryInterceptor struct {
    // No additional fields needed
}
```

Features:
- Configurable retry attempts via options
- Immediate retry on failure
- Context-aware cancellation
- Error propagation
- Skip retry when max attempts is 0
- Support for timeout and cancellation

### Rate Limit

Controls the rate of function executions.

```go
type RateLimitInterceptor struct {
    cache *expirable.LRU[string, *rate.Limiter]
    mu    sync.Mutex
}
```

Features:
- Per-second rate limiting
- Burst allowance
- PID and actor-based rate limiting
- LRU cache for limiters
- Thread-safe limiter creation
- Skip rate limiting when RPS is 0
- Context-aware cancellation

### Timeout

Enforces execution time limits.

```go
type TimeoutInterceptor struct {
    // No additional fields needed
}
```

Features:
- Configurable timeout duration
- Context-based cancellation
- Graceful timeout handling
- Error propagation
- Skip timeout when duration is 0
- Support for both timeout and context cancellation
- Goroutine-based execution with result channels

### NOP (No Operation)

A no-operation interceptor that does nothing but pass through the execution.

```go
type NopInterceptor struct{}
```

Features:
- Zero-overhead pass-through
- Context preservation
- Result propagation
- Useful for testing and development

## Execution Flow

1. Function Call Initiated
   - Task creation with function ID
   - Context preparation
   - PID generation and context enrichment

2. Load Configurations
   - System defaults
   - Global config
   - Module config (_index.yaml)
   - Function config
   - Runtime options

3. Build Execution Context
   - Merge options from all levels
   - Setup interceptors from registry
   - Prepare context with options
   - Setup cancellation context
   - Add interceptor registry to context

4. Execute Interceptor Chain
   - Create result channel
   - Build interceptor chain
   - Execute each interceptor in sequence
   - Handle context propagation
   - Process results and errors
   - Support cancellation at any point

5. Return Result
   - Send result through channel
   - Handle errors and timeouts
   - Clean up resources
   - Propagate context changes

## Interceptor Chain

The interceptor chain manages the execution of multiple interceptors in sequence:

```go
type Chain struct {
    interceptors []Interceptor
}

func (c Chain) Execute(ctx context.Context, f function.Func, task runtime.Task) (chan *runtime.Result, error) {
    // Create a result channel
    resultChan := make(chan *runtime.Result, 1)

    // Create a next function that will be passed to each interceptor
    next := c.getNext(ctx, resultChan, 0, f, task)
    result, newCtx := next(ctx)
    if result != nil && result.Error != nil {
        close(resultChan)
        return nil, result.Error
    }

    resultChan <- result
    return resultChan, nil
}

func (c Chain) getNext(ctx context.Context, resultChan chan *runtime.Result, index int, f function.Func, task runtime.Task) func(context.Context) (*runtime.Result, context.Context) {
    if index >= len(c.interceptors) {
        return func(ctx context.Context) (*runtime.Result, context.Context) {
            // All interceptors have been executed, now run the actual function
            ch, err := f(ctx, task)
            if err != nil {
                return &runtime.Result{Error: err}, ctx
            }

            // Forward the result from the function's channel to our result channel
            result := <-ch
            if result != nil && result.Error != nil {
                return result, ctx
            }

            return result, ctx
        }
    }

    interceptor := c.interceptors[index]
    return func(ctx context.Context) (*runtime.Result, context.Context) {
        // Get the next function in the chain, always passing the latest context
        nextFn := c.getNext(ctx, resultChan, index+1, f, task)

        // Execute the current interceptor with the next function
        result, newCtx := interceptor.Handle(ctx, nextFn)

        return result, newCtx
    }
}
```

The chain execution:
- Creates a buffered result channel
- Builds a chain of next functions
- Executes interceptors in sequence
- Propagates context through the chain
- Handles errors at each step
- Supports cancellation
- Manages function execution
- Returns results through channels

## Registry

The registry manages available interceptors:

```go
type Registry struct {
    ctx          context.Context
    logger       *zap.Logger
    bus          event.Bus
    interceptors []Interceptor
    mu           sync.RWMutex
    subscriber   *eventbus.Subscriber
}
```

Features:
- Thread-safe interceptor registration
- Event-based interceptor management
- Dynamic interceptor updates
- List available interceptors
- Context-aware operation
- Logging support
- Event subscription handling

### Event System

The registry uses an event-based system for interceptor management:

```go
const (
    // System identifies the interceptor system in the event bus
    System event.System = "interceptor"

    // Event kinds
    Register event.Kind = "interceptor.register"
    Update   event.Kind = "interceptor.update"
    Delete   event.Kind = "interceptor.delete"
    Accept   event.Kind = "interceptor.accept"
    Reject   event.Kind = "interceptor.reject"
)
```

Event-based features:
- Registration events for new interceptors
- Update events for existing interceptors
- Delete events for removing interceptors
- Accept/Reject events for operation results
- Event subscription management
- Event handling with context
- Error reporting through events

### Registry Operations

The registry provides several operations:

```go
// Core operations
func (r *Registry) Start(ctx context.Context) error
func (r *Registry) Stop() error
func (r *Registry) Register(name string, interceptor Interceptor) error
func (r *Registry) Unregister(name string) error
func (r *Registry) Get(name string) (Interceptor, error)
func (r *Registry) List() []string
func (r *Registry) GetChain() Chain

// Event handling
func (r *Registry) handleEvent(e event.Event)
func (r *Registry) registerInterceptor(e event.Event)
func (r *Registry) updateInterceptor(e event.Event)
func (r *Registry) deleteInterceptor(e event.Event)
func (r *Registry) sendAccept(path event.Path)
func (r *Registry) sendReject(path event.Path, reason string)
```

Operation features:
- Lifecycle management (Start/Stop)
- Thread-safe registration
- Event-based updates
- Chain retrieval
- Error handling
- Event processing
- Status reporting

## Usage Example

```lua
-- Configure interceptors in _index.yaml
-- Then use in code:

executor:with_options({
    timeout = "10s",
    max_retries = 3,
    retry_delay = "1s",
    workflow_id = "my-workflow"
})
```

## Error Handling

The interceptor system provides standardized error handling:

- Error propagation through interceptor chain
- Error transformation and enrichment
- Error reporting and monitoring
- Standardized error types

## Performance Considerations

- Interceptor chain optimization
- Configuration caching
- Option validation caching
- Minimal overhead for disabled interceptors
- LRU cache for rate limiters

## Security

- Option validation
- Interceptor permissions
- Configuration validation
- Access control
- PID-based rate limiting

## Monitoring and Observability

- Interceptor execution metrics
- Option usage statistics
- Error rates and types
- Performance metrics
- OpenTelemetry integration

## Extension Points

The system can be extended through:

- Custom interceptors
- Custom options
- Custom validators
- Custom error handlers
- Event-based interceptor management

## Best Practices

1. **Configuration**
   - Keep global configurations minimal
   - Use function-specific configurations when needed
   - Document all custom interceptors
   - Validate configuration values

2. **Performance**
   - Disable unused interceptors
   - Use appropriate retry policies
   - Monitor interceptor performance
   - Configure appropriate rate limits

3. **Error Handling**
   - Implement proper error handling in custom interceptors
   - Use appropriate error types
   - Log relevant information
   - Handle context cancellation

4. **Security**
   - Validate all configurations
   - Implement proper access control
   - Monitor for security issues
   - Use PID-based rate limiting

## Implementation Status

Currently implemented:
- Core interceptor interfaces
- Interceptor chain
- Registry system
- Configuration loader
- OpenTelemetry interceptor
- Retry interceptor
- Rate limit interceptor
- Timeout interceptor
- Event-based management
- Context propagation

In progress:
- Enhanced monitoring capabilities
- Performance optimizations
- Additional security features
- Documentation improvements

## Future Considerations

- Dynamic interceptor configuration
- More built-in interceptors
- Enhanced monitoring capabilities
- Performance optimizations
- Additional security features
- Improved error handling
- Better context management

### Context Management

The interceptor system provides several context utilities:

```go
// Context keys for interceptor system
type RegistryContextKey struct{}
type OptionsContextKey struct{}
type CancelContextKey struct{}

// Context utilities
func WithInterceptor(ctx context.Context, registry Registry) context.Context
func GetInterceptors(ctx context.Context) Registry
func GetOptionsFromContext(ctx context.Context) Options
func WithOptions(ctx context.Context, options Options) context.Context
func WithCancel(ctx context.Context, cancel context.CancelFunc) context.Context
func GetCancelFromContext(ctx context.Context) context.CancelFunc
```

These utilities enable:
- Interceptor registry access
- Options configuration
- Cancellation control
- Context propagation through the chain 