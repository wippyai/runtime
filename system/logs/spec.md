# Logging System Specification

## Overview

A dynamic, event-driven logging system that provides configurable log routing, filtering, and runtime configuration management. The system supports downstream propagation and event bus streaming with dynamic level control.

## Core Components

### Manager

Central coordinator managing log configuration and event handling.

#### Key Features:
- Runtime configuration management
- Event-based configuration updates
- Thread-safe state management
- Graceful shutdown support
- Dynamic log level control

#### Usage:
```go
manager := NewManager(bus, core, logger, zapcore.InfoLevel)
err := manager.Start(ctx)
defer manager.Stop()

// Get current config
config := manager.GetConfig()
```

### Core

Log interceptor and router implementing zapcore.Core interface.

#### Key Features:
- Configurable log filtering
- Dual output streams (downstream and events)
- Thread-safe configuration changes
- Field augmentation support
- Atomic operations

#### Usage:
```go
core := NewCore(downstream, bus)
core.Configure(config)

// Check if level is enabled
if core.Enabled(zapcore.InfoLevel) {
    // Log will be processed
}
```

### ConfigSwitcher

Manages temporary logging configurations with ability to restore previous state.

#### Key Features:
- Temporary config switching
- Base config preservation
- Automatic restoration
- Thread-safe operations
- Clear state management

#### Usage:
```go
switcher := NewConfigSwitcher(bus, logger)

// Enable temporary config
err := switcher.EnableTemporaryConfig(ctx, tempConfig)

// Restore original config
switcher.RestoreBaseConfig(ctx)
```

### ConfigurationManager

Handles configuration distribution and synchronization.

#### Key Features:
- Asynchronous config updates
- Event-based communication
- Timeout handling
- Error propagation
- Operation tracking

#### Usage:
```go
cfgManager := NewConfigurationManager()

// Get current config
config, err := cfgManager.GetConfig(ctx, bus)

// Set new config
err = cfgManager.SetConfig(ctx, bus, newConfig)
```

## Configuration Model

### Log Config Structure
```go
type Config struct {
    PropagateDownstream bool
    StreamToEvents      bool
    MinLevel           zapcore.Level
}
```

### Configuration Properties:
1. PropagateDownstream
    - Controls log forwarding to downstream handlers
    - Enables/disables traditional logging paths

2. StreamToEvents
    - Toggles event bus publishing
    - Enables distributed log collection

3. MinLevel
    - Sets minimum log level threshold
    - Controls log filtering granularity

## Event Communication

### Event Types

1. Configuration Events:
    - `logs.config.set`: Update logging configuration
    - `logs.config.get`: Request current configuration
    - `logs.config.state`: Configuration state response

2. Log Events:
    - `logs.entry`: Log entry publication

### Event Flow

1. Configuration Updates:
```
Client -> SetConfig Event -> Manager -> State Event -> Client
```

2. Configuration Requests:
```
Client -> GetConfig Event -> Manager -> State Event -> Client
```

3. Log Publication:
```
Logger -> Core -> Event Bus -> Subscribers
```

## Performance Characteristics

- Atomic configuration updates
- Non-blocking event publication
- Buffered event channels
- Thread-safe operations
- Minimal lock contention

## Error Handling

### Types of Errors:
- Configuration validation errors
- Event bus communication errors
- Context cancellation
- Timeout errors
- Downstream propagation errors

### Error Propagation:
1. Immediate errors returned directly
2. Asynchronous errors logged
3. Context cancellation handled at all levels

## Best Practices

1. Configuration Management:
```go
ctx, cancel := context.WithTimeout(parentCtx, time.Second)
defer cancel()

err := manager.Start(ctx)
if err != nil {
    log.Fatal("failed to start manager", zap.Error(err))
}
```

2. Temporary Configurations:
```go
switcher := NewConfigSwitcher(bus, logger)
defer switcher.RestoreBaseConfig(ctx)

err := switcher.EnableTemporaryConfig(ctx, tempConfig)
if err != nil {
    log.Error("failed to set temp config", zap.Error(err))
}
```

3. Event Handling:
```go
sub, err := eventbus.NewSubscriber(
    ctx, 
    bus,
    api.System,
    "logs.config.*",
    handleEvent,
)
if err != nil {
    return fmt.Errorf("failed to subscribe: %w", err)
}
defer sub.Close()
```

## Limitations

- No persistent configuration storage
- No built-in authentication/authorization
- Single active configuration at a time
- No automatic config validation
- No built-in rate limiting

## Security Considerations

- No built-in access control
- Event bus security inherited
- Configuration validation responsibility
- Consider implementing middleware for security
- Sensitive data logging controls needed

## Testing

The system includes comprehensive tests covering:

- Basic functionality
- Configuration changes
- Event handling
- Error conditions
- Concurrent operations
- Timeout handling
- State management
- Core logging operations

## Implementation Guidelines

1. Custom Core Implementation:
```go
type Core struct {
    downstream zapcore.Core
    bus        events.Bus
    config     atomic.Value
}

func (c *Core) Write(ent zapcore.Entry, fields []zapcore.Field) error {
    cfg := c.config.Load().(Config)
    
    if cfg.PropagateDownstream {
        if err := c.downstream.Write(ent, fields); err != nil {
            return err
        }
    }

    if cfg.StreamToEvents {
        c.publishLogEvent(ent, fields)
    }

    return nil
}
```

2. Event Handling:
```go
func (m *Manager) handleEvent(e events.Event) {
    switch e.Kind {
    case api.SetConfigEvent:
        m.handleConfigEvent(m.ctx, e)
    case api.GetConfigEvent:
        m.handleGetConfigEvent(m.ctx, e)
    }
}
```

3. Configuration Switching:
```go
func (c *ConfigSwitcher) EnableTemporaryConfig(ctx context.Context, cfg Config) error {
    if c.baseConfig == nil {
        current, err := c.cfgManager.GetConfig(ctx, c.bus)
        if err != nil {
            return err
        }
        c.baseConfig = &current
    }
    
    return c.cfgManager.SetConfig(ctx, c.bus, cfg)
}
```