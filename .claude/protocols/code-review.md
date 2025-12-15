# Code Review Protocol v2

## Purpose

This protocol defines how agents review Go code in this codebase. Reviews identify issues and report to the parent agent - they do NOT make changes.

**Philosophy:** Less is more. Prefer simple, idiomatic Go. Detect unnecessary complexity and AI-generated bloat.

---

## Project Structure

### Directory Organization

```
api/                    # Contracts: interfaces, types, errors, commands
  ├── <domain>/         # Core domain packages
  │   ├── <domain>.go   # Interfaces and core types
  │   ├── errors.go     # Structured errors
  │   ├── context.go    # Context keys and With*/Get* helpers
  │   └── command.go    # Dispatcher commands (if applicable)
  └── service/<domain>/ # Service-specific contracts
      ├── config.go     # Configuration structs
      ├── errors.go     # Service errors
      └── <variant>/    # Implementation variants (memory/, sql/, etc.)

system/                 # Core infrastructure implementations
  └── <domain>/
      ├── registry.go   # State storage
      ├── dispatcher.go # Command handlers
      └── manager.go    # Lifecycle coordination

service/                # Business logic implementations
  └── <domain>/
      ├── manager.go    # Service lifecycle
      ├── factory.go    # Object creation
      └── <variant>/    # Implementation variants

internal/               # Shared internal utilities
runtime/                # Runtime-specific code (Lua, etc.)
boot/                   # Application bootstrapping
```

### What Goes Where

| Type | Location | Example |
|------|----------|---------|
| Interfaces | `api/<domain>/` | `api/store/store.go` |
| Errors | `api/<domain>/errors.go` | `api/fs/errors.go` |
| Config structs | `api/service/<domain>/config.go` | `api/service/http/config.go` |
| Commands | `api/<domain>/command.go` | `api/clock/command.go` |
| Context keys | `api/<domain>/context.go` | `api/fs/context.go` |
| Implementations | `system/` or `service/` | `service/store/memory/` |

---

## Core Patterns

### 1. Interfaces in api/, implementations elsewhere

```go
// api/store/store.go - INTERFACE
type Store interface {
    Get(ctx context.Context, key registry.ID) (payload.Payload, error)
    Set(ctx context.Context, entry Entry) error
}

// service/store/memory.go - IMPLEMENTATION
type memoryStore struct { ... }
var _ store.Store = (*memoryStore)(nil)  // compile-time check
```

### 2. Errors defined in api/ packages

```go
// api/store/errors.go - structured errors
type Error struct {
    kind      apierror.Kind
    message   string
    retryable apierror.Ternary
    details   attrs.Attributes
    cause     error
}

// Sentinel errors
var (
    ErrNotFound = errors.New("not found")
    ErrClosed   = errors.New("closed")
)

// Constructors
func NewNotFoundError(id string) *Error { ... }
```

```go
// service/store/memory.go - USE DIRECTLY (no aliases)
return nil, store.ErrNotFound  // Good
return nil, errNotFound        // Bad - don't alias
```

### 3. Context keys and helpers

```go
// api/<domain>/context.go
var registryKey = &ctxapi.Key{Name: "fs.registry"}  // private, Key suffix

func WithRegistry(ctx context.Context, reg Registry) context.Context {
    ac := ctxapi.AppFromContext(ctx)
    if ac == nil {
        return ctx
    }
    ac.With(registryKey, reg)
    return ctx
}

func GetRegistry(ctx context.Context) Registry {
    ac := ctxapi.AppFromContext(ctx)
    if ac == nil {
        return nil
    }
    if v := ac.Get(registryKey); v != nil {
        return v.(Registry)
    }
    return nil
}
```

### 4. Dispatcher pattern

```go
type Dispatcher struct { ... }

func NewDispatcher() *Dispatcher { ... }
func (d *Dispatcher) Start(ctx context.Context) error { ... }
func (d *Dispatcher) Stop(ctx context.Context) error { ... }
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
    register(api.CmdFoo, dispatcher.HandlerFunc(d.handleFoo))
}
```

### 5. Manager pattern

```go
type Manager struct { ... }

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error { ... }
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error { ... }
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error { ... }
```

### 6. Functional options

```go
type Option func(*Config)

func New(opts ...Option) *Thing {
    cfg := &Config{timeout: 30 * time.Second}  // defaults
    for _, opt := range opts {
        opt(cfg)
    }
    return &Thing{cfg: cfg}
}

func WithTimeout(d time.Duration) Option {
    return func(c *Config) { c.timeout = d }
}

func WithLogger(log *zap.Logger) Option {
    return func(c *Config) { c.logger = log }
}
```

### 7. Configuration structs

```go
// api/service/<domain>/config.go
type Config struct {
    Name    string `json:"name"`              // required
    Timeout string `json:"timeout"`           // optional, parsed

    parsedTimeout time.Duration               // private parsed field
}

func (c *Config) Validate() error {
    if c.Name == "" {
        return errors.New("name is required")
    }
    if c.Timeout != "" {
        d, err := time.ParseDuration(c.Timeout)
        if err != nil {
            return err
        }
        c.parsedTimeout = d
    }
    return nil
}

func (c *Config) GetTimeout() time.Duration {  // Get prefix when field exists
    if c.parsedTimeout == 0 {
        return 30 * time.Second
    }
    return c.parsedTimeout
}
```

### 8. Resource cleanup

```go
// Mutex: always defer unlock
func (r *registry) Get(id uint64) *entry {
    r.mu.RLock()
    defer r.mu.RUnlock()
    return r.items[id]
}

// Context cancellation
select {
case <-ctx.Done():
    return ctx.Err()
case result := <-ch:
    return result
}

// Stop methods
func (s *Service) Stop() {
    close(s.done)
    s.wg.Wait()
}
```

---

## Naming Conventions

### Context Keys

| Pattern | Example |
|---------|---------|
| Variable | `var registryKey = &ctxapi.Key{...}` |
| Key name | `Name: "fs.registry"` |
| Setter | `func WithRegistry(ctx, reg)` |
| Getter | `func GetRegistry(ctx)` |

**Rules:**
- Private variable with `Key` suffix: `registryKey`, `managerKey`
- NOT `CtxKey` suffix, NOT `Ctx` suffix
- NOT exported (use getter function if public access needed)

```go
// Good
var registryKey = &ctxapi.Key{Name: "fs.registry"}

// Bad
var registryCtxKey = ...   // wrong suffix
var registryCtx = ...      // wrong suffix
var RegistryKey = ...      // don't export
```

### Event Constants

```go
const System event.System = "fs"

const (
    KindRegister event.Kind = "fs.register"  // Kind prefix
    KindDelete   event.Kind = "fs.delete"
    KindAccept   event.Kind = "fs.accept"
)
```

**Rule:** Event kind constants use `Kind` prefix.

### Errors

```go
// Sentinel errors - Err prefix
var (
    ErrNotFound = errors.New("not found")
    ErrClosed   = errors.New("closed")
)

// Constructors - New prefix
func NewNotFoundError(id string) *Error { ... }
func NewTimeoutError(d time.Duration) *Error { ... }
```

### Interfaces

- No `I` prefix
- `-er` suffix for actors: `Handler`, `Dispatcher`, `Finder`
- Domain nouns: `Store`, `Bus`, `Registry`

```go
// Good
type Handler interface { ... }
type Store interface { ... }

// Bad
type IHandler interface { ... }
type StoreInterface interface { ... }
```

### Structs

- PascalCase domain nouns
- `Config` suffix for configuration
- `Entry` for registry items
- `Cmd` suffix for commands

```go
type ServerConfig struct { ... }
type StoreEntry struct { ... }
type ReadCmd struct { ... }
```

### Constants

- PascalCase (no ALL_CAPS)
- Grouped by domain

| Category | Prefix | Example |
|----------|--------|---------|
| Commands | `Cmd` | `CmdRead`, `CmdWrite` |
| Event kinds | `Kind` | `KindRegister`, `KindDelete` |
| Headers | `Header` | `HeaderContentType` |

### Getters

- No `Get` prefix normally: `func (c *Config) Timeout() time.Duration`
- Use `Get` prefix when field name conflicts: `func (c *Config) GetMode() int` (if `Mode` field exists)

### Functional Options

- Type: `Option` (singular, not `Options`)
- Functions: `With<Field>` prefix

```go
type Option func(*Config)           // Good
type Options func(*Config)          // Bad - plural

func WithTimeout(d) Option          // Good
func SetTimeout(d) Option           // Bad - wrong prefix
```

### Files

```
<domain>.go      # Core types/interfaces
errors.go        # Error definitions
context.go       # Context keys/helpers
command.go       # Dispatcher commands
config.go        # Configuration
manager.go       # Lifecycle coordination
registry.go      # State storage
dispatcher.go    # Command handlers
factory.go       # Object creation
*_test.go        # Tests
```

---

## Review Checklist

### 1. Structure

- [ ] Interfaces in `api/`, implementations in `system/` or `service/`
- [ ] Errors defined in `api/` packages
- [ ] Context keys in `api/<domain>/context.go`
- [ ] Config structs in `api/service/<domain>/config.go`
- [ ] Implementation variants in subdirectories

### 2. Naming

- [ ] Context keys use `Key` suffix (not `CtxKey` or `Ctx`)
- [ ] Event constants use `Kind` prefix
- [ ] Errors use `Err` prefix
- [ ] Options type is singular `Option`
- [ ] No `I` prefix on interfaces
- [ ] PascalCase constants (no ALL_CAPS)

### 3. Patterns

- [ ] Functional options for configurable constructors
- [ ] Config structs have `Validate()` method
- [ ] Getters only use `Get` prefix when field conflicts
- [ ] Implementations verify interface with `var _ Interface = (*Type)(nil)`

### 4. Linting

- [ ] Would pass `go vet`
- [ ] Would pass `gofmt`
- [ ] No unused imports/variables
- [ ] No shadowed variables

### 5. Idiomatic Go

- [ ] `if err != nil { return }` pattern
- [ ] Context as first parameter
- [ ] Consistent receivers
- [ ] No stuttering (`user.UserID` → `user.ID`)

### 6. Resource Cleanup

- [ ] Mutex: `defer unlock` after lock
- [ ] Goroutines have exit conditions
- [ ] Channels closed when done
- [ ] Stop() waits for goroutines

### 7. Error Handling

- [ ] Errors from `api/` used directly (no aliases)
- [ ] Proper wrapping with `%w`
- [ ] Constructors for dynamic context

### 8. Simplicity

- [ ] No unnecessary abstractions
- [ ] No wrapper functions that just call another function
- [ ] No "future-proofing" for requirements that don't exist
- [ ] No commented-out code

---

## Multi-Agent Review Structure

### Agent 1: Structure Review
- Package organization
- File size (>800 lines = flag)
- Public surface
- Interface/error locations

### Agent 2: Correctness Review
- Error handling
- Resource cleanup
- Concurrency safety
- Nil checks

### Agent 3: Simplicity Review
- Unnecessary code
- Over-engineering
- AI bloat
- Idiomatic patterns

---

## Protected Areas (Review Only)

Do NOT suggest changes to:

- `system/scheduler/actor/worker.go`
- `system/scheduler/actor/scheduler.go`
- `runtime/lua/engine/process.go`
- `system/relay/mailbox.go`
- `system/eventbus/bus.go`

Only report clear bugs or resource leaks.

---

## Output Format

```
## Code Review: {package_path}

### Summary
- Files reviewed: N
- Issues: N (critical: N, warning: N, info: N)

### Critical Issues
{must fix - bugs, leaks}

### Warnings
{should fix - naming, structure, location}

### Info
{suggestions}
```

### Issue Format

```
[SEVERITY] file.go:LINE - CATEGORY
Description
Current: what it does
Expected: what it should do
```

---

## Severity Levels

| Level | Meaning |
|-------|---------|
| CRITICAL | Bug, leak, missing cleanup |
| WARNING | Naming, structure, wrong location |
| INFO | Suggestion |
| HUMAN | Requires human decision |

---

## Categories

| Category | Covers |
|----------|--------|
| STRUCTURE | Wrong package location |
| NAMING | Violates naming conventions |
| CLEANUP | Missing resource cleanup |
| ERROR_LOCATION | Error in wrong package |
| CONTEXT_KEY | Wrong context key pattern |
| UNNECESSARY | Code that can be removed |
| AI_BLOAT | Over-engineered abstractions |
