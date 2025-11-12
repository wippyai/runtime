# API Code Style Guide

This document outlines the coding conventions and patterns used in the `api/` directory. Follow these guidelines when creating new API files or modifying existing ones.

## Table of Contents

1. [File Structure and Organization](#file-structure-and-organization)
2. [Naming Conventions](#naming-conventions)
3. [Config Struct Patterns](#config-struct-patterns)
4. [Custom Type Patterns](#custom-type-patterns)
5. [Documentation Style](#documentation-style)
6. [Interface Design](#interface-design)
7. [Code Grouping and Organization](#code-grouping-and-organization)
8. [Dependency Injection](#dependency-injection)
9. [Time and Duration Handling](#time-and-duration-handling)

---

## File Structure and Organization

### Directory Layout

- **Flat package structure** with clear domain separation
- Service-specific configs in `api/service/{service-name}/`
- Core abstractions in root-level packages (`context`, `registry`, `supervisor`, `event`, `payload`)
- **Maximum 3 levels of nesting** - avoid deep hierarchies

### File Naming

- `config.go` - Configuration structs and validation
- `context.go` - Context-related types and helpers
- `api.go` or package-named file - Main interfaces and types
- `*_test.go` - Test files with table-driven tests

**Example structure:**
```
api/
├── service/
│   ├── http/
│   │   ├── config.go
│   │   └── config_test.go
│   ├── sql/
│   │   ├── config.go
│   │   └── config_test.go
├── registry/
│   ├── registry.go
│   ├── id.go
│   └── meta.go
├── types/
│   ├── duration.go
│   └── duration_test.go
```

---

## Naming Conventions

### Variables

Use **camelCase** for variables:

```go
cfg, c         // config receivers
ctx            // context
fc             // FrameContext
cleanupInterval // descriptive names
maxProcesses
tokenLength
```

### Types and Structs

Use **PascalCase**:

```go
// Config structs
type ServerConfig struct { ... }
type DBConfig struct { ... }
type PoolConfig struct { ... }

// Nested structs
type TimeoutConfig struct { ... }
type RetryPolicy struct { ... }

// Entry wrappers
type EntryConfig struct { ... }
```

### Interfaces

Use **PascalCase**, typically noun-based or with `-er` suffix:

```go
// Noun-based
type Service interface { ... }
type Registry interface { ... }
type Finder interface { ... }

// Action-based with -er suffix
type EntryReader interface { ... }
type StateWriter interface { ... }
type Unmarshaler interface { ... }
type Transcoder interface { ... }
```

### Functions and Methods

Use **PascalCase** for exported, **camelCase** for private:

```go
// Getters
func Get() { ... }
func GetString() { ... }

// Setters
func Set() { ... }
func SetMeta() { ... }

// Validators (always present on config structs)
func (c *Config) Validate() error { ... }

// Initializers (private, called within Validate)
func (c *Config) initDefaults() { ... }
```

### Constants

Use **PascalCase** or **UPPER_SNAKE** for exports:

```go
// Registry kinds
const (
    KindServer    registry.Kind = "http.service"
    KindPostgres  registry.Kind = "postgres.db"
    KindMemoryKV  registry.Kind = "memory.kv"
)

// Event types
const (
    Create EventType = "create"
    Update EventType = "update"
    Delete EventType = "delete"
)

// String constants
const (
    ServerID   = "server"
    RouterID   = "router"
    ConfigSep  = ":"
)
```

---

## Config Struct Patterns

### Standard Structure

Configs follow a consistent field ordering pattern:

```go
type Config struct {
    // 1. Metadata (if present, being phased out)
    Meta registry.Metadata `json:"meta,omitempty"`

    // 2. Required dependencies (registry.ID references)
    Database registry.ID `json:"database"`
    Store    registry.ID `json:"store"`

    // 3. Core configuration fields
    MaxSize int    `json:"max_size"`
    Host    string `json:"host"`
    Port    int    `json:"port"`

    // 4. Nested configuration structs
    Pool     PoolConfig    `json:"pool"`
    Timeouts TimeoutConfig `json:"timeouts"`

    // 5. Optional fields with omitempty
    TokenKey string `json:"token_key,omitempty"`

    // 6. Lifecycle config (for supervised services)
    Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`

    // 7. Security config (when applicable)
    Security SecurityConfig `json:"security,omitempty"`
}
```

### Common Fields

Almost all service configs include:

```go
// Lifecycle management (in 80%+ of service configs)
Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`

// Dependency references
Database registry.ID `json:"database"`
Store    registry.ID `json:"store"`
```

### Required Methods

Every config struct must implement:

#### 1. Validate()

```go
// Validate checks the configuration for errors and initializes defaults.
func (c *Config) Validate() error {
    // Always call first
    c.initDefaults()

    // Check required fields
    if c.Database.Name == "" {
        return fmt.Errorf("database ID is required")
    }

    // Validate ranges
    if c.MaxSize < 0 {
        return fmt.Errorf("max_size must be greater than or equal to 0")
    }

    // Validate nested configs
    if err := c.Lifecycle.Validate(); err != nil {
        return fmt.Errorf("lifecycle: %w", err)
    }

    return nil
}
```

#### 2. initDefaults()

```go
// Private method, called within Validate
func (c *Config) initDefaults() {
    if c.MaxSize == 0 {
        c.MaxSize = 10000
    }

    if c.CleanupInterval.IsZero() {
        c.CleanupInterval = types.Duration(5 * time.Minute)
    }

    // Chain to nested configs
    c.Lifecycle.InitDefaults()
}
```

---

## Custom Type Patterns

### Type Aliases

Use type aliases (`type X = Y`) for semantic clarity without additional behavior:

```go
type (
    // Registry types
    Kind      = string
    Namespace = string
    Name      = string

    // PubSub types
    NodeID = string
    HostID = string
    Topic  = string
)
```

**When to use:** Need type compatibility with underlying type, no custom methods required.

### Enum-Like Types

#### Integer Enums with String() Method

```go
type Phase int

const (
    PreInit Phase = iota
    Init
    PostInit
    Start
)

func (p Phase) String() string {
    switch p {
    case PreInit:
        return "PreInit"
    case Init:
        return "Init"
    case PostInit:
        return "PostInit"
    case Start:
        return "Start"
    default:
        return "Unknown"
    }
}
```

#### String Enums with Validation

```go
type Effect string

const (
    Allow Effect = "allow"
    Deny  Effect = "deny"
)

// Validate in parent config:
func (c *PolicyConfig) Validate() error {
    if c.Effect != Allow && c.Effect != Deny {
        return fmt.Errorf("invalid policy effect: %s", c.Effect)
    }
    return nil
}
```

### Structured Types with Rich Behavior

For types needing custom serialization, parsing, or helper methods:

```go
type ID struct {
    NS   Namespace `json:"ns"`
    Name Name      `json:"name"`
}

// Implement these methods as needed:
func (t ID) String() string { ... }
func (t ID) MarshalJSON() ([]byte, error) { ... }
func (t *ID) UnmarshalJSON(data []byte) error { ... }

// Package-level parser
func ParseID(s string) ID { ... }
```

**Examples from codebase:**
- `registry.ID` - Namespace and name identifier with string parsing
- `pubsub.PID` - Process ID with string caching and pooling

### Methods Typically Implemented

1. **String()** - Human-readable representation
2. **MarshalJSON() / UnmarshalJSON()** - Custom JSON serialization
3. **Parse*()** - Package-level parsing functions
4. **Helper methods** - Type-safe accessors or converters

---

## Documentation Style

### General Rules

- Use single-line `//` comments only
- **No block comments**
- **No emoji**
- **No markers like "FIXED", "ADDED", "CRITICAL"** in regular comments
- Use `// todo:` sparingly and be specific
- Every exported type, function, and method must have a comment

### Comment Format

Comments start with the item name:

```go
// ServerConfig represents the configuration for the HTTP server.
type ServerConfig struct {
    // MaxConnections is the maximum number of concurrent connections.
    // A value of 0 means unlimited.
    MaxConnections int `json:"max_connections"`
}

// Validate checks the configuration for errors.
func (c *ServerConfig) Validate() error { ... }
```

### Tone and Content

- **Technical and precise**
- Explain purpose and behavior
- Note special cases: `"(0 = unlimited)"`, `"(for pooling)"`
- Include validation context when relevant

```go
// MaxSize is the maximum number of entries in the store.
// When MaxSize is 0, the store is unlimited.
// When the store reaches MaxSize, new entries are rejected with ErrStoreFull.
MaxSize int `json:"max_size"`
```

### Const Block Documentation

```go
// Registry kind constants for HTTP service components.
// These identify different types of HTTP-related components in the registry.
const (
    // KindServer identifies an HTTP server component
    KindServer registry.Kind = "http.service"

    // KindRouter identifies an HTTP router component
    KindRouter registry.Kind = "http.router"
)
```

---

## Interface Design

### Principles

- **Small and focused** - Typically 2-7 methods
- **Single Responsibility Principle**
- Use composition via embedding

### Common Patterns

#### Reader/Writer Split

```go
type EntryReader interface {
    GetAllEntries() ([]Entry, error)
    GetEntry(ID) (Entry, error)
}

type StateWriter interface {
    Apply(context.Context, ChangeSet) (Version, error)
    ApplyVersion(context.Context, Version) error
}

// Composition
type Registry interface {
    EntryReader
    StateWriter
}
```

#### Listener Pattern

```go
type EntryListener interface {
    Add(context.Context, Entry) error
    Update(context.Context, Entry) error
    Delete(context.Context, Entry) error
}
```

### Method Documentation

Every interface method must be documented:

```go
type Service interface {
    // Start initializes and starts the service.
    // It returns an error if the service cannot be started.
    Start(context.Context) error

    // Stop gracefully shuts down the service.
    // It waits for ongoing operations to complete before returning.
    Stop(context.Context) error
}
```

---

## Code Grouping and Organization

### Import Grouping

```go
import (
    // Standard library
    "context"
    "encoding/json"
    "fmt"
    "time"

    // Internal API packages
    "github.com/ponyruntime/pony/api/registry"
    "github.com/ponyruntime/pony/api/supervisor"
    "github.com/ponyruntime/pony/api/types"
)
```

### Struct Field Ordering

```go
type frameContext struct {
    // 1. Exported fields first
    Parent FrameContext

    // 2. Private fields
    values map[any]any
    sealed bool

    // 3. Synchronization primitives last
    mu sync.RWMutex
}
```

### Method Ordering

1. Interface implementation methods (grouped together)
2. Public helper methods
3. Private helper methods

```go
// Interface implementation
func (c *Config) Validate() error { ... }

// Public helpers
func (c *Config) GetTimeout() time.Duration { ... }

// Private helpers
func (c *Config) initDefaults() { ... }
```

### Type Declaration Blocks

Group related types in a single block:

```go
type (
    // Configuration structs
    Config struct { ... }
    PoolConfig struct { ... }
    TimeoutConfig struct { ... }
)
```

---

## Dependency Injection

### Registry-Based Pattern

Services declare dependencies using `registry.ID`:

```go
type Config struct {
    // Reference to dependency
    Database registry.ID `json:"database"`
    Store    registry.ID `json:"store"`
    Set      registry.ID `json:"set"`
}
```

### Parsing Dependencies

```go
contractID := registry.ParseID(cfg.Contract)  // "ns:name" -> ID{NS: "ns", Name: "name"}
```

### Environment Variable Pattern

Support both direct values and environment variables:

```go
type Config struct {
    // Direct value
    Host string `json:"host"`
    Port int    `json:"port"`

    // Environment variable reference
    HostEnv string `json:"host_env,omitempty"`
    PortEnv string `json:"port_env,omitempty"`
}

// Validate both
func (c *Config) Validate() error {
    if c.Host == "" && c.HostEnv == "" {
        return fmt.Errorf("either host or host_env must be specified")
    }
    return nil
}
```

---

## Time and Duration Handling

### Duration Fields

**Use `types.Duration` instead of `time.Duration`** to get automatic JSON string marshaling:

```go
import "github.com/ponyruntime/pony/api/types"

type Config struct {
    // Serializes as "10s", "5m", "1h" in JSON
    CleanupInterval types.Duration `json:"cleanup_interval"`
    StartTimeout    types.Duration `json:"start_timeout"`
}
```

**Benefits:**
- No manual UnmarshalJSON/MarshalJSON boilerplate
- Consistent string format ("10s", "5m", "1h")
- Helper methods: `IsZero()`, `String()`, `Std()`

### Time Fields

For timestamps, use `time.Time` directly:

```go
type Event struct {
    // Uses standard RFC3339 JSON encoding
    At       time.Time `json:"at"`
    Deadline time.Time `json:"deadline"`
}
```

### Default Values

Set defaults in `initDefaults()`:

```go
func (c *Config) initDefaults() {
    if c.CleanupInterval.IsZero() {
        c.CleanupInterval = types.Duration(5 * time.Minute)
    }

    if c.StartTimeout.IsZero() {
        c.StartTimeout = types.Duration(10 * time.Second)
    }
}
```

### Validation

Check for valid ranges:

```go
func (c *Config) Validate() error {
    c.initDefaults()

    if c.CleanupInterval.Std() < 0 {
        return fmt.Errorf("cleanup_interval must be positive or zero")
    }

    return nil
}
```

### Converting to time.Duration

Use the `Std()` method:

```go
func (c *Config) GetCleanupInterval() time.Duration {
    return c.CleanupInterval.Std()
}

// Or use directly in time operations
ticker := time.NewTicker(c.CleanupInterval.Std())
```

---

## Summary

### Key Principles

1. **Consistency over cleverness** - Follow established patterns
2. **Explicit over implicit** - Make behavior clear
3. **Flat over nested** - Avoid deep hierarchies
4. **Simple over complex** - Prefer straightforward solutions
5. **Documentation is required** - Every export must be documented

### Common Validation Pattern

Every config struct should follow this pattern:

```go
func (c *Config) Validate() error {
    // Step 1: Initialize defaults
    c.initDefaults()

    // Step 2: Validate required fields
    if c.RequiredField == "" {
        return fmt.Errorf("required_field is required")
    }

    // Step 3: Validate ranges and constraints
    if c.MaxSize < 0 {
        return fmt.Errorf("max_size must be non-negative")
    }

    // Step 4: Validate nested configs
    if err := c.Nested.Validate(); err != nil {
        return fmt.Errorf("nested: %w", err)
    }

    return nil
}

func (c *Config) initDefaults() {
    // Set zero-value defaults
    if c.MaxSize == 0 {
        c.MaxSize = 10000
    }

    // Chain to nested configs
    c.Lifecycle.InitDefaults()
}
```

### Checklist for New API Files

- [ ] Follow naming conventions (camelCase vars, PascalCase types)
- [ ] Use `types.Duration` for duration fields
- [ ] Implement `Validate()` and `initDefaults()` on configs
- [ ] Use `registry.ID` for service dependencies
- [ ] Document all exported types and methods
- [ ] Order struct fields: dependencies → config → nested → lifecycle
- [ ] Group imports: stdlib → internal packages
- [ ] Write table-driven tests in `*_test.go`
- [ ] Keep interfaces small and focused (2-7 methods)
- [ ] Use type aliases for semantic clarity, new types for behavior

---

## Examples

### Complete Config Example

```go
package myservice

import (
    "context"
    "fmt"
    "time"

    "github.com/ponyruntime/pony/api/registry"
    "github.com/ponyruntime/pony/api/supervisor"
    "github.com/ponyruntime/pony/api/types"
)

// Config represents the configuration for MyService.
type Config struct {
    // Database is the database connection to use
    Database registry.ID `json:"database"`

    // MaxSize is the maximum number of items to cache (0 = unlimited)
    MaxSize int `json:"max_size"`

    // CleanupInterval is how often to run cleanup
    CleanupInterval types.Duration `json:"cleanup_interval"`

    // Timeouts contains timeout configuration
    Timeouts TimeoutConfig `json:"timeouts"`

    // Lifecycle contains lifecycle management configuration
    Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
}

// TimeoutConfig contains timeout settings.
type TimeoutConfig struct {
    // Read is the read timeout
    Read types.Duration `json:"read"`

    // Write is the write timeout
    Write types.Duration `json:"write"`
}

// Validate checks the configuration for errors and initializes defaults.
func (c *Config) Validate() error {
    c.initDefaults()

    if c.Database.Name == "" {
        return fmt.Errorf("database is required")
    }

    if c.MaxSize < 0 {
        return fmt.Errorf("max_size must be non-negative")
    }

    if err := c.Lifecycle.Validate(); err != nil {
        return fmt.Errorf("lifecycle: %w", err)
    }

    return nil
}

func (c *Config) initDefaults() {
    if c.MaxSize == 0 {
        c.MaxSize = 10000
    }

    if c.CleanupInterval.IsZero() {
        c.CleanupInterval = types.Duration(5 * time.Minute)
    }

    if c.Timeouts.Read.IsZero() {
        c.Timeouts.Read = types.Duration(30 * time.Second)
    }

    if c.Timeouts.Write.IsZero() {
        c.Timeouts.Write = types.Duration(30 * time.Second)
    }

    c.Lifecycle.InitDefaults()
}
```
