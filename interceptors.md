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
    Handle(ctx context.Context, next func() *runtime.Result) *runtime.Result
}
```

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
- Custom attributes support
- Execution timing and duration tracking
- Error and result recording
- Context propagation

### Retry

Handles automatic retries for failed function executions.

```go
type RetryInterceptor struct {
    // No additional fields needed
}
```

Features:
- Configurable retry attempts
- Immediate retry on failure
- Context-aware cancellation
- Error propagation

### Rate Limit

Controls the rate of function executions.

```go
type RateLimitInterceptor struct {
    cache *expirable.LRU[string, *rate.Limiter]
}
```

Features:
- Per-second rate limiting
- Burst allowance
- PID-based rate limiting
- LRU cache for limiters

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

## Execution Flow

1. Function Call Initiated
2. Load Configurations
   - System defaults
   - Global config
   - Module config
   - Function config
3. Build Execution Context
   - Merge options
   - Setup interceptors
   - Prepare context
4. Execute Interceptor Chain
   - Before hooks
   - Function execution
   - After hooks
5. Return Result

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
    result := next()
    if result != nil && result.Error != nil {
        close(resultChan)
        return nil, result.Error
    }

    resultChan <- result
    return resultChan, nil
}
```

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