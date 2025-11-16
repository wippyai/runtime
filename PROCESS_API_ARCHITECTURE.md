# Process API Architecture Analysis

## Executive Summary

The process API is organized into **5 distinct architectural layers** with separate concerns:

1. **API Layer** (`api/process`) - Interface definitions
2. **System Layer** (`system/process`) - Registry and orchestration
3. **Service Layer** (`service/host`) - Host implementation with process pooling
4. **Runtime Layer** (`runtime/lua/component/process`) - Lua process execution
5. **Bootstrap Layer** (`boot/`) - Wiring and initialization

Each layer handles options/configuration differently, creating both opportunities and pain points for adding new process options.

---

## 1. Process Creation APIs & Entry Points

### 1.1 Main Entry Points

**API Level** - `/home/wolfy-j/projects/wippy/api/process/process.go`

```go
// Process interface - what must be implemented
type Process interface {
    relay.Receiver
    Start(stdcontext.Context, relay.PID, payload.Payloads) error
    Step() (StepResult, error)
    Terminate()
}

// Start configuration for launching processes
type Start struct {
    HostID      relay.HostID          // Target host
    Source      registry.ID           // Process prototype
    UniqID      string               // Optional unique ID
    Input       payload.Payloads     // Process input data
    Lifecycle   Lifecycle            // Supervision config
    Context     []context.Pair       // Context overrides
}

// Launch configuration for managed hosts
type Launch struct {
    PID         relay.PID            // Assigned PID
    Source      registry.ID          // Process prototype
    Process     Process              // Instantiated process
    Input       payload.Payloads     // Process input data
    Lifecycle   Lifecycle            // Supervision config
    Context     []context.Pair       // Context overrides
}
```

**System Level** - `/home/wolfy-j/projects/wippy/system/process/manager.go`

```go
// Manager.Start() - Main entry point for starting processes
func (m *Manager) Start(ctx context.Context, start *api.Start) (relay.PID, error) {
    host, exists := m.hosts.GetHost(start.HostID)
    if !exists {
        return relay.PID{}, fmt.Errorf("host not found: `%s`", start.HostID)
    }
    
    _, managed := host.(api.Managed)
    pid := m.preparePID(ctx, start, managed)
    return m.launchOnHost(ctx, host, pid, start)
}

// launchOnHost handles actual launch (delegates to host)
func (m *Manager) launchOnHost(ctx context.Context, host api.Host, pid relay.PID, ps *api.Start) (relay.PID, error) {
    switch h := host.(type) {
    case api.Managed:
        proc, err := m.prototypes.Create(ps.Source)  // Step 1: Create process
        newPid, err := h.Launch(ctx, &api.Launch{    // Step 2: Launch on host
            PID:       pid,
            Source:    ps.Source,
            Process:   proc,
            Input:     ps.Input,
            Lifecycle: ps.Lifecycle,
            Context:   ps.Context,
        })
    case api.Delegated:
        newPid, err := h.Launch(ctx, pid, ps.Lifecycle, ps.Input)
    }
}
```

**Service Level (Managed Host)** - `/home/wolfy-j/projects/wippy/service/host/host.go`

```go
// Host.Launch() - Managed host implementation
func (h *Host) Launch(ctx context.Context, launch *process.Launch) (relay.PID, error) {
    if h.pool.Has(launch.PID) {
        return relay.PID{}, process.ErrHostBusy
    }
    
    ctx = h.prepareContext(ctx, launch)  // Prepare context
    
    if err := launch.Process.Start(ctx, launch.PID, launch.Input); err != nil {
        return relay.PID{}, err
    }
    
    _, err := h.msgHost.Attach(launch.PID, h.getQueueForPID(launch.PID))
    if err := h.pool.Add(launch.PID, launch.Source, launch.Process); err != nil {
        launch.Process.Terminate()
        h.msgHost.Detach(launch.PID)
        return relay.PID{}, err
    }
    
    return launch.PID, nil
}
```

**Runtime Level (Lua Process)** - `/home/wolfy-j/projects/wippy/runtime/lua/component/process/process.go`

```go
// LuaProcess.Start() - Actual process initialization
func (p *LuaProcess) Start(ctx context.Context, pid relay.PID, input payload.Payloads) error {
    if err := p.state.InitContext(ctx, pid); err != nil {
        return err
    }
    
    onStart := process.GetOnStart(p.state.Ctx)
    onStartFunc := func() {
        if onStart != nil {
            onStart(pid, p)
        }
    }
    
    return p.state.Start(input, onStartFunc)
}
```

---

## 2. Options/Configuration Management

### 2.1 Configuration vs Runtime Options Pattern

The system uses **two different option types** at different levels:

#### A. Boot-Time Configuration (API Level)

Stored in `/home/wolfy-j/projects/wippy/api/service/host/config.go`:

```go
type EntryConfig struct {
    HostConfig Config                     // Host settings
    Lifecycle  supervisor.LifecycleConfig // Lifecycle settings
}

type Config struct {
    MaxProcesses       int // Maximum concurrent processes
    Workers            int // Worker goroutines
    BufferSize         int // Message queue buffer
    WorkerCount        int // Message workers
    MessageWorkerCount int // Message workers (duplicate?)
}
```

**Usage Location:**
- Loaded from config files during boot
- Passed to host factory during initialization
- Stored in `service/host/host.go` Host struct

#### B. Runtime Options (Nested in Task/Start Structures)

Defined in `/home/wolfy-j/projects/wippy/api/runtime/task.go`:

```go
type Task struct {
    ID       registry.ID      // What to run
    Payloads payload.Payloads // Input data
    Options  Options          // Runtime options ← HERE
    Context  []context.Pair   // Context overrides
}

// Options is an alias to attrs.Attributes
type Options = attrs.Attributes
```

**Problem:** `Options` is just a key-value bag with no schema at the API level.

#### C. Specific Service Options (Multiple Locations)

**Retry Interceptor** - `/home/wolfy-j/projects/wippy/api/service/interceptor/retry/config.go`:

```go
// Config - bootstrap-time config
type Config struct {
    Enabled     bool `json:"enabled"`
    MaxAttempts int  `json:"max_attempts"`
}

// Options - runtime options (duplicate of config + extra)
type Options struct {
    MaxAttempts int `json:"max_attempts"`
    BackoffMs   int `json:"backoff_ms"`
}
```

**OpenTelemetry Interceptor** - `/home/wolfy-j/projects/wippy/api/service/interceptor/otel/config.go`:

```go
type Config struct {
    Enabled bool `json:"enabled" yaml:"enabled"`
}

type Options struct {
    SpanName   string
    Attributes map[string]string
}
```

**Process Execution** - `/home/wolfy-j/projects/wippy/api/service/exec/api.go`:

```go
type ProcessOptions struct {
    WorkDir string
    Env     map[string]string
}
```

### 2.2 How Process Options Flow Through Layers

```
1. API Layer (api/process)
   ├─ Start.Context: []context.Pair ← Raw pairs, not typed
   └─ Start.Lifecycle: Lifecycle ← Typed struct

2. System Layer (system/process/manager.go)
   ├─ Copies Start → Launch unchanged
   └─ No option transformation

3. Service Layer (service/host/host.go)
   ├─ Launch.Context: []context.Pair ← Still raw
   └─ prepareContext() → Builds FrameContext
       ├─ Sets: FrameID, FramePID, FrameHost
       ├─ Applies: launch.Context pairs
       └─ Attaches: process lifecycle callbacks

4. Runtime Layer (runtime/lua/component/process)
   └─ p.state.InitContext(ctx, pid) ← Receives prepared context
       └─ Only accesses values via context.Get()
```

**Key Finding:** There is NO schema for process-level options. All options flow as untyped `context.Pair` arrays.

---

## 3. Host Types & Differentiation

### 3.1 Host Interface Hierarchy

**Base Interface** - `/home/wolfy-j/projects/wippy/api/process/process.go`:

```go
type Host interface {
    relay.Receiver          // Can receive messages
    Terminator             // Can terminate processes
}
```

**Managed Host** - Processes are instantiated by the manager:

```go
type Managed interface {
    Host
    Launch(ctx stdcontext.Context, launch *Launch) (relay.PID, error)
}
```

**Delegated Host** - Host creates its own processes:

```go
type Delegated interface {
    Host
    Launch(ctx stdcontext.Context, pid relay.PID, lf Lifecycle, input payload.Payloads) (relay.PID, error)
}
```

### 3.2 Host Type Detection & Registration

**In HostRegistry** - `/home/wolfy-j/projects/wippy/system/process/host_registry.go`:

```go
func (r *HostRegistry) registerHost(e event.Event) {
    host, ok := e.Data.(api.Host)
    
    // Determine host type
    managed := false
    switch h := host.(type) {
    case api.Managed:
        managed = true      // Store this flag
    case api.Delegated:
        // delegated host
    default:
        r.sendReject(e.Path, "host must implement either Managed or Delegated")
    }
    
    info := hostInfo{host: host, managed: managed}
    r.hosts.Store(e.Path, info)
}
```

### 3.3 Current Host Implementations

**Managed Host** - `/home/wolfy-j/projects/wippy/service/host/host.go`:

```go
type Host struct {
    id              registry.ID
    cfg             *host.EntryConfig
    log             *zap.Logger
    msgHost         relay.Host        // Message routing
    msgQueues       []chan *relay.Package
    pool            ProcessPoolAPI    // Process pool
    msgFactory      MessageHostFactory
    poolFactory     ProcessPoolFactory
}
```

Only current implementation. No delegated hosts found in the codebase.

---

## 4. Data Flow: Options Through Layers

### 4.1 Configuration Bootstrap Flow

```
boot/components/service/service/host.go
├─ Load host configuration from JSON
├─ Create HostManager via prochost.NewHostManager()
└─ Register as listener for "process.host" events

api/service/host/config.go
└─ EntryConfig {
    HostConfig: {MaxProcesses, Workers, BufferSize}
    Lifecycle: {StopTimeout}
}

service/host/factory.go
├─ DefaultHostFactory.CreateHost()
├─ Validates config
└─ Creates Host with DefaultProcessPoolFactory + DefaultMessageHostFactory

service/host/host.go
└─ Stores config in Host struct
   ├─ Used for pool sizing (Workers, MaxProcesses)
   ├─ Used for message queue sizing (BufferSize, MessageWorkerCount)
   └─ Used for shutdown timing
```

### 4.2 Runtime Options Flow

```
API: api/process/process.go - Start struct
├─ Start.Context: []context.Pair
│  └─ Actor, Scope, custom values
└─ Start.Lifecycle: Lifecycle
   ├─ Parent PID
   ├─ Monitor flag
   └─ Link flag

System: system/process/manager.go
├─ Receives Start
├─ Creates PID
└─ Passes to Host.Launch() without modification
   └─ Creates Launch struct (just copies fields)

Service: service/host/host.go
├─ Receives Launch
├─ Calls prepareContext()
│  ├─ Opens FrameContext
│  ├─ Sets standard pairs (FrameID, FramePID, FrameHost)
│  ├─ Applies launch.Context pairs
│  └─ Attaches lifecycle callbacks
└─ Calls Process.Start(ctx, pid, input)

Runtime: runtime/lua/component/process/process.go
├─ Receives prepared context
├─ Calls p.state.InitContext(ctx, pid)
└─ Values accessible via context.Get() calls
```

### 4.3 What Gets Duplicated

**Config Duplication:**

1. **Host Config** appears in:
   - `api/service/host/config.go` - EntryConfig
   - `service/host/host.go` - Stored in Host struct (not redefined, just used)
   - `service/host/pool.go` - Used to configure ProcessPool

2. **Interceptor Config** appears in:
   - `api/service/interceptor/retry/config.go` - Bootstrap config
   - `api/service/interceptor/retry/config.go` - Runtime Options (duplication!)
   - `service/interceptor/retry/interceptor.go` - Uses Options at runtime

3. **Lifecycle Config** appears in:
   - `api/service/supervisor/config.go` - LifecycleConfig
   - `api/process/process.go` - Lifecycle struct
   - `system/process/manager.go` - Used in callbacks

**Key Insight:** Retry interceptor has `Config` (bootstrap) and `Options` (runtime) that are mostly redundant but separately defined.

---

## 5. Current Pain Points

### 5.1 Adding New Process-Level Options (Problematic)

To add a new process option (e.g., timeout, retry behavior):

1. **API Layer** - No schema change needed (uses context.Pair)
   - Just pass new Pair in Start.Context
   - But: No type safety, no documentation

2. **System Layer** - No changes needed
   - Manager passes through unchanged

3. **Service Layer** - Moderate changes:
   - Host.prepareContext() just applies pairs as-is
   - But: Need to know WHERE in context layer to set it
   - Need to know WHAT it means (what format, validation?)

4. **Runtime Layer** - Must modify:
   - LuaProcess/State needs to retrieve and use the option
   - Changes needed in runtime/lua/component/process/

5. **Bootstrap Layer** - Must modify:
   - If it's config-time: add to EntryConfig, update Host bootstrap
   - If it's runtime: update wherever it's accessed

**Problem:** No consistent location for process options schema. Tight coupling between layers.

### 5.2 Interceptor Options Duplication

Current pattern requires defining options in TWO places:

```go
// api/service/interceptor/retry/config.go
type Config struct {
    MaxAttempts int
}

type Options struct {
    MaxAttempts int   // Same as Config
    BackoffMs   int   // Extra runtime-only field
}
```

**Issues:**
- Field duplication
- Validation happens twice
- No inheritance relationship
- Difficult to add new fields

### 5.3 Context Pair Validation

Currently `Start.Context` accepts ANY `context.Pair` values without validation:

```go
type Start struct {
    Context []context.Pair  // No schema, no validation
}
```

**Consequences:**
- Invalid options silently ignored
- No discovery of what options are valid
- Runtime errors instead of build-time errors

### 5.4 Host Configuration Scattered

Host configuration values are used in multiple places without centralization:

```go
service/host/host.go
├─ cfg.HostConfig.MessageWorkerCount → Message routing
├─ cfg.HostConfig.Workers           → Pool size
├─ cfg.HostConfig.MaxProcesses      → Pool limit
└─ cfg.Lifecycle.StopTimeout         → Graceful shutdown
```

Changes to host behavior require understanding usage in multiple layers.

### 5.5 Process Pool API Lacks Granularity

`ProcessPoolAPI` interface doesn't expose pool configuration:

```go
type ProcessPoolAPI interface {
    Add(pid relay.PID, source registry.ID, proc process.Process) error
    Schedule(pid relay.PID) error
    Has(pid relay.PID) bool
    // ... no way to get pool status, update limits dynamically
}
```

Hard to:
- Query current pool utilization
- Adjust max processes dynamically
- Get backpressure signals

---

## 6. Architectural Patterns

### 6.1 Event-Driven Registration

```
Hosts and Prototypes use event bus for registration:

api/process/process.go
├─ PrototypeSystem = "prototype"
│  ├─ ProtoRegister
│  ├─ ProtoDelete
│  └─ ProtoAccept/ProtoReject
└─ HostSystem = "hosts"
   ├─ HostRegister
   ├─ HostDelete
   └─ HostAccept/HostReject

system/process/prototype_registry.go
├─ Subscribes to prototype.* events
└─ Manages Create() factory

system/process/host_registry.go
├─ Subscribes to hosts.* events
└─ Manages GetHost() lookup
```

**Advantage:** Decoupled registration
**Disadvantage:** Async validation, delayed error feedback

### 6.2 Context as Options Container

```
Context Inheritance Model:
┌─────────────────┐
│  AppContext     │ (application-wide)
│  ├─ Global keys │
│  └─ Values Bag  │
└─────────────────┘
         ↓ Inherit
┌─────────────────┐
│ FrameContext    │ (call-specific)
│ ├─ Actor/Scope  │
│ ├─ Values Bag   │
│ └─ Custom pairs │
└─────────────────┘
         ↓
┌─────────────────┐
│ Process Context │ (via Start.Context pairs)
│ ├─ Inherits      │
│ ├─ Overrides     │
│ └─ New pairs     │
└─────────────────┘
```

**Usage:** Start.Context provides call-specific overrides.

### 6.3 Process Pool Abstraction

```
ProcessPoolAPI (interface)
    ↑
    │ implements
    │
ProcessPool (sync.Map-based)
├─ Concurrent process storage
├─ Worker goroutines for execution
├─ Message scheduling (awaken atomics)
└─ Cancellation coordination
```

**Design:** No options for pool behavior exposed through interface.

---

## 7. Key Findings Summary

| Aspect | Current State | Impact |
|--------|---------------|--------|
| **Options Schema** | No schema, untyped pairs | Hard to validate, discover features |
| **Config vs Runtime** | Mixed in different ways | Confusing patterns, duplication |
| **Options Location** | Scattered across layers | Hard to add new options |
| **Host Types** | 2 interfaces, 1 implementation | Delegated hosts untested |
| **Options Flow** | Via context.Pair arrays | No type safety |
| **Pool Configuration** | Fixed at creation | Can't adjust dynamically |
| **Lifecycle Integration** | Callbacks attached at launch | Difficult to compose |
| **Interceptor Options** | Duplicated Config/Options | Maintenance overhead |

---

## 8. Recommended Improvements

### Short Term (Low Risk)

1. **Document Process Options**
   - Create canonical list of supported context.Pair keys
   - Add examples to Start struct

2. **Consolidate Interceptor Options**
   - Make Options inherit from Config, not duplicate
   - Use composition/embedding

3. **Add Pool Status API**
   - GetStatus() method returning pool metrics
   - Query current load before launching

### Medium Term (Architectural)

1. **Typed Options at API Level**
   - Replace context.Pair with structured types
   - Example: `type ProcessOptions struct { Timeout, Retry, ... }`

2. **Options Inheritance Hierarchy**
   - Define base options schema
   - Interceptors extend with specific options
   - Validate once, use everywhere

3. **Process Options Registry**
   - Centralized registration of valid options
   - Validation at Start() time
   - Discovery mechanism for tooling

### Long Term (Major Refactor)

1. **Separate Config from Runtime Options**
   - Bootstrap config → immutable
   - Runtime options → per-task/process
   - Clear types for each

2. **Process Pool Configuration**
   - Make pool adjustable post-creation
   - Add dynamic scaling capabilities
   - Expose metrics/telemetry

3. **Delegated Host Implementation**
   - Implement at least one delegated host
   - Test cross-host process spawning
   - Document delegation patterns

---

## 9. File Reference Map

### Core API Definitions
- `/home/wolfy-j/projects/wippy/api/process/process.go` - Main interfaces (Start, Launch, Process, Host)
- `/home/wolfy-j/projects/wippy/api/process/context.go` - Lifecycle callbacks
- `/home/wolfy-j/projects/wippy/api/runtime/task.go` - Task structure with Options

### System Layer
- `/home/wolfy-j/projects/wippy/system/process/manager.go` - Start orchestration
- `/home/wolfy-j/projects/wippy/system/process/host_registry.go` - Host lookup
- `/home/wolfy-j/projects/wippy/system/process/prototype_registry.go` - Process creation

### Service Layer
- `/home/wolfy-j/projects/wippy/service/host/host.go` - Managed host implementation
- `/home/wolfy-j/projects/wippy/service/host/pool.go` - Process pool (150+ lines)
- `/home/wolfy-j/projects/wippy/service/host/factory.go` - Host creation
- `/home/wolfy-j/projects/wippy/api/service/host/config.go` - Host configuration

### Runtime Layer
- `/home/wolfy-j/projects/wippy/runtime/lua/component/process/process.go` - Lua process entry point
- `/home/wolfy-j/projects/wippy/runtime/lua/component/process/manager.go` - Lua manager
- `/home/wolfy-j/projects/wippy/runtime/lua/component/process/state.go` - Lua state management

### Bootstrap Layer
- `/home/wolfy-j/projects/wippy/boot/components/service/service/host.go` - Host component bootstrap
- `/home/wolfy-j/projects/wippy/boot/components/system/system/functions.go` - Function registry bootstrap
- `/home/wolfy-j/projects/wippy/boot/components/service/service/all.go` - Interceptor registration

### Configuration Examples
- `/home/wolfy-j/projects/wippy/api/service/interceptor/retry/config.go` - Config duplication example
- `/home/wolfy-j/projects/wippy/api/service/interceptor/otel/config.go` - OTEL options
- `/home/wolfy-j/projects/wippy/runtime/lua/code/build_options.go` - BuildOptions (different pattern)

