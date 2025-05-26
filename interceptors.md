# Function Execution Interceptors

## Overview

The interceptor system provides a way to add cross-cutting concerns to function execution in a modular and configurable way. It allows for adding functionality like tracing, retries, rate limiting, and more without modifying the core function code.

## Architecture

### Core Components

```
Function Execution System
├── Options Layer
│   ├── Common Options Interface
│   ├── Temporal Compatibility Layer
│   └── Options Validator
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

### Execution Context

```go
type Execution struct {
    FunctionID   string
    Options      map[string]interface{}
    Context      context.Context
    Interceptors []Interceptor
    Result       interface{}
    Error        error
    StartTime    time.Time
    EndTime      time.Time
}
```

### Interceptor Interface

```go
type Interceptor interface {
    Before(ctx context.Context, execution *Execution) error
    After(ctx context.Context, execution *Execution, result interface{}, err error) error
}
```

### Option Interface

```go
type Option interface {
    Validate() error
    Apply(execution *Execution) error
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
meta:
  interceptors:
    global:
      otel:
        enabled: true
        service_name: "module_name"
        custom_attributes:
          environment: "production"
      retry:
        enabled: true
        max_attempts: 3
        initial_interval: "1s"
        max_interval: "10s"
        multiplier: 2
      rate_limit:
        enabled: true
        requests_per_second: 100
        burst: 50

  functions:
    my_function:
      interceptors:
        otel:
          enabled: true
          custom_attributes:
            function_type: "critical"
        retry:
          enabled: true
          max_attempts: 5
```

## Built-in Interceptors

### OpenTelemetry (OTEL)

Provides distributed tracing and metrics collection.

```go
type Config struct {
    Enabled          bool
    ServiceName      string
    CustomAttributes map[string]string
}

type Interceptor struct {
    config Config
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
type RetryPolicy struct {
    MaxAttempts     int
    InitialInterval time.Duration
    MaxInterval     time.Duration
    Multiplier      float64
}
```

Features:
- Configurable retry attempts
- Exponential backoff
- Maximum interval limits
- Custom retry conditions

### Rate Limit

Controls the rate of function executions.

```go
type RateLimit struct {
    RequestsPerSecond int
    Burst            int
}
```

Features:
- Per-second rate limiting
- Burst allowance
- Configurable limits
- Queue management

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

func (c *Chain) Execute(ctx context.Context, execution *Execution, fn func(context.Context, *Execution) (interface{}, error)) (interface{}, error) {
    // Execute before hooks
    for _, interceptor := range c.interceptors {
        if err := interceptor.Before(ctx, execution); err != nil {
            return nil, fmt.Errorf("interceptor before hook failed: %w", err)
        }
    }

    // Execute the function
    result, err := fn(ctx, execution)
    execution.Result = result
    execution.Error = err

    // Execute after hooks in reverse order
    for i := len(c.interceptors) - 1; i >= 0; i-- {
        interceptor := c.interceptors[i]
        if err := interceptor.After(ctx, execution, result, err); err != nil {
            return nil, fmt.Errorf("interceptor after hook failed: %w", err)
        }
    }

    return result, err
}
```

## Registry

The registry manages available interceptors:

```go
type Registry struct {
    interceptors sync.Map // map[string]Interceptor
}
```

Features:
- Thread-safe interceptor registration
- Name-based interceptor lookup
- Dynamic interceptor management
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

## Security

- Option validation
- Interceptor permissions
- Configuration validation
- Access control

## Monitoring and Observability

- Interceptor execution metrics
- Option usage statistics
- Error rates and types
- Performance metrics

## Extension Points

The system can be extended through:

- Custom interceptors
- Custom options
- Custom validators
- Custom error handlers

## Best Practices

1. **Configuration**
   - Keep global configurations minimal
   - Use function-specific configurations when needed
   - Document all custom interceptors

2. **Performance**
   - Disable unused interceptors
   - Use appropriate retry policies
   - Monitor interceptor performance

3. **Error Handling**
   - Implement proper error handling in custom interceptors
   - Use appropriate error types
   - Log relevant information

4. **Security**
   - Validate all configurations
   - Implement proper access control
   - Monitor for security issues

## Implementation Status

Currently implemented:
- Core interceptor interfaces
- Interceptor chain
- Registry system
- Configuration loader
- OpenTelemetry interceptor

In progress:
- Retry interceptor
- Rate limit interceptor
- Function executor integration
- Tests and documentation

## Future Considerations

- Dynamic interceptor configuration
- More built-in interceptors
- Enhanced monitoring capabilities
- Performance optimizations
- Additional security features 