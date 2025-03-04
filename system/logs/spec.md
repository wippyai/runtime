# Logging Component Specification

## Purpose
The Logging component provides a structured, configurable logging system for the Pony Runtime with support for dynamic configuration and event integration. It allows for centralized logging control, context-based logger retrieval, and flexible routing of log entries.

## Core Concepts

### Logging Configuration
- **PropagateDownstream**: Controls whether logs are sent to underlying logger implementations
- **StreamToEvents**: Controls whether logs are published as events on the event bus
- **MinLevel**: Defines the minimum level threshold for log processing

### Core Components
- **Core**: Zapcore implementation that intercepts and routes log entries based on configuration
- **Manager**: Central service that manages logging configuration and handles config events
- **ConfigSwitcher**: Utility for temporarily changing and restoring logging configurations

## Architecture

### Core
The Core component serves as the central logging interceptor:
- Implements Zap's zapcore.Core interface for integration with Zap logger
- Routes log entries based on current configuration
- Supports both traditional logging and event-based publishing
- Thread-safe configuration handling using atomic storage

### Manager
The Manager component provides centralized logging control:
- Handles configuration events from the event bus
- Applies configuration changes to the Core
- Responds to configuration queries
- Provides current configuration state

### ConfigSwitcher
The ConfigSwitcher provides a pattern for temporary configuration changes:
- Stores the original configuration for later restoration
- Applies temporary configurations without losing original settings
- Restores previous configuration when operation completes

### Context Integration
The context integration provides logger access throughout the system:
- `WithLogger`: Stores a logger in a context
- `GetLogger`: Retrieves a logger from a context (with fallback to no-op)
- Allows propagation of logger instances without direct coupling

## Message Flow

### Configuration Changes
1. Client sends `logs.config.set` event with new configuration
2. Manager receives event and validates configuration
3. Manager applies configuration to Core
4. Manager sends `logs.config.state` event confirming the change

### Configuration Queries
1. Client sends `logs.config.get` event
2. Manager receives event and prepares current configuration
3. Manager sends `logs.config.state` event with current configuration

### Log Entry Processing
1. Application code logs message via Zap logger
2. Core's Write method intercepts log entry
3. If PropagateDownstream is true, entry is sent to underlying logger
4. If StreamToEvents is true, entry is published as event on bus

## Integration Points

### Event System
The Logging component uses events for management operations:
- `logs.config.set`: Request to update logging configuration
- `logs.config.get`: Request for current configuration
- `logs.config.state`: Response with configuration state
- `logs.entry`: Log entry published as an event

### Context System
- Logger instances are stored and retrieved from context
- Provides a standard pattern for accessing loggers throughout the system

## Utility Helpers

### ConfigurationManager
- Provides high-level methods for interacting with logging configuration
- Handles subscription to configuration events
- Supports request/response pattern for configuration queries
- Provides timeouts and error handling

## Common Usage Patterns

### Temporary Logging Configuration
```go
// Store original config and apply temporary one
switcher := logs.NewConfigSwitcher(bus, logger)
tempConfig := api.Config{
    PropagateDownstream: true,
    StreamToEvents: true,
    MinLevel: zapcore.DebugLevel,
}
switcher.EnableTemporaryConfig(ctx, tempConfig)

// Do work with temporary config...

// Restore original config
switcher.RestoreBaseConfig(ctx)
```

### Context-based Logger Retrieval
```go
// Store logger in context
ctx = logs.WithLogger(ctx, logger)

// Later, retrieve logger from context
logger := logs.GetLogger(ctx)
logger.Info("Operation completed", zap.Int("count", count))
```

This specification defines the Logging component that provides flexible, structured logging for the Pony Runtime with support for dynamic configuration and integration with the event system.