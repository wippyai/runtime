# Process API - Tight Coupling Analysis

## Critical Coupling Points

### 1. Manager ↔ Host Interface Coupling

**Location:** `wippy/system/process/manager.go`

```go
// Problem: Manager knows about Managed vs Delegated distinction
func (m *Manager) launchOnHost(ctx context.Context, host api.Host, 
    pid relay.PID, ps *api.Start) (relay.PID, error) {
    
    switch h := host.(type) {
    case api.Managed:
        // Manager must instantiate process BEFORE passing to host
        proc, err := m.prototypes.Create(ps.Source)  // ← Manager owns creation
        newPid, err := h.Launch(ctx, &api.Launch{
            PID:       pid,
            Source:    ps.Source,
            Process:   proc,  // ← Must pass already-created instance
            Input:     ps.Input,
            Lifecycle: ps.Lifecycle,
            Context:   ps.Context,
        })
        
    case api.Delegated:
        // Delegated host creates its own - different interface!
        newPid, err := h.Launch(ctx, pid, ps.Lifecycle, ps.Input)
        // ← No Process passed, no Source passed!
    }
}
```

**Coupling Problem:**
- Manager has different launch paths for different host types
- Managed: Manager creates, Host launches
- Delegated: Host creates its own (but no way to pass Source!)
- Delegated.Launch() signature is incompatible with passing registry.ID

**Impact:**
- Can't add feature that affects Delegated hosts without changing Manager
- Delegated host type untested (interface mismatch)
- Hard to support new host archetypes

---

### 2. Host ↔ ProcessPool Configuration Coupling

**Location:** `wippy/service/host/host.go`

```go
// Host creation stores config
func NewMultiProcessHost(
    id registry.ID,
    config *host.EntryConfig,  // ← Immutable after creation
    log *zap.Logger,
    msgFactory MessageHostFactory,
    poolFactory ProcessPoolFactory,
) *Host {
    msgQueues := make([]chan *relay.Package, config.HostConfig.MessageWorkerCount)
    for i := 0; i < config.HostConfig.MessageWorkerCount; i++ {
        msgQueues[i] = make(chan *relay.Package, config.HostConfig.BufferSize)
    }
    
    return &Host{
        id:          id,
        cfg:         config,  // ← Stored, but not configurable later
        // ...
    }
}

// Start() creates pool with fixed config
func (h *Host) Start(ctx context.Context) (<-chan any, error) {
    h.pool, err = h.poolFactory.CreateProcessPool(
        ctx,
        h.cfg.HostConfig.Workers,      // ← Fixed from config
        h.cfg.HostConfig.MaxProcesses, // ← Fixed from config
        h.log,
    )
}
```

**Coupling Problem:**
- Pool configuration set at Host creation time
- ProcessPool interface has no way to adjust limits later
- Can't query current pool utilization
- Can't add backpressure handling

**Impact:**
- Can't implement dynamic scaling
- Can't diagnose pool exhaustion without accessing internal state
- Host config must be known upfront, no runtime tuning

---

### 3. FrameContext ↔ Custom Options Coupling

**Location:** `wippy/service/host/host.go`

```go
func (h *Host) prepareContext(ctx context.Context, launch *process.Launch) context.Context {
    // Open FrameContext
    pCtx, fc := ctxapi.OpenFrameContext(ctx)
    
    // Build pairs manually
    pairsLen := 3 + len(launch.Context)  // ← Hard to extend!
    pairs := make([]ctxapi.Pair, pairsLen)
    pairs[0] = ctxapi.Pair{Key: runtime.FrameIDKey, Value: launch.Source}
    pairs[1] = ctxapi.Pair{Key: runtime.FramePIDKey, Value: launch.PID}
    pairs[2] = ctxapi.Pair{Key: runtime.FrameHostKey, Value: h.id}
    
    // Copy custom pairs as-is (no validation!)
    if len(launch.Context) > 0 {
        copy(pairs[3:], launch.Context)  // ← BLIND COPY
    }
    
    if err := fc.SetMultiple(pairs...); err != nil {
        h.log.Error("failed to set frame context", zap.Error(err))
    }
}
```

**Coupling Problem:**
- Host doesn't know what context pairs mean
- No validation of pairs
- Pairs silently ignored if invalid
- Hard to debug missing options

**Impact:**
- Silent failures when options misnamed
- No way to validate options at launch time
- Must rely on runtime behavior to detect errors
- Difficult to add option validation later

---

### 4. LuaProcess ↔ State Coupling

**Location:** `wippy/runtime/lua/component/process/process.go`

```go
type LuaProcess struct {
    state *State  // ← Direct dependency on State internals
}

func NewLuaProcess(log *zap.Logger, runner *engine.Runner, 
    funcName string) (process.Process, error) {
    state, err := NewState(log, runner, funcName)  // ← State creation buried here
    // ...
    return &LuaProcess{state: state}, nil
}

func (p *LuaProcess) Start(ctx context.Context, pid relay.PID, 
    input payload.Payloads) error {
    
    // Access state internals directly
    if err := p.state.InitContext(ctx, pid); err != nil {  // ← Init in state
        return err
    }
    
    onStart := process.GetOnStart(p.state.Ctx)  // ← Access internal Ctx
    return p.state.Start(input, onStartFunc)    // ← Start in state
}
```

**Coupling Problem:**
- Process interface doesn't expose where options go
- LuaProcess must know about State internals
- State creation happens in Process factory
- Hard to inject different option handling

**Impact:**
- Can't reuse LuaProcess with different state implementations
- Options must be handled inside State.InitContext()
- No clear contract about option application timing

---

### 5. Interceptor Config ↔ Options Duplication

**Location:** `wippy/api/service/interceptor/retry/config.go`

```go
// Bootstrap config
type Config struct {
    Enabled     bool `json:"enabled" yaml:"enabled"`
    MaxAttempts int  `json:"max_attempts" yaml:"max_attempts"`
}

// Runtime options
type Options struct {
    MaxAttempts int `json:"max_attempts"`  // ← DUPLICATED
    BackoffMs   int `json:"backoff_ms"`    // ← Extra field
}
```

**Coupling Problem:**
- Two separate types with overlapping fields
- No inheritance relationship
- Both must be updated when adding MaxTimeout or similar
- Validation logic duplicated

**Current Implementations:**

```go
// In service/interceptor/retry/interceptor.go
// Config loaded at boot time
var defaultConfig = Config{
    Enabled:     true,
    MaxAttempts: 3,
}

// At runtime
func NewRetryInterceptor(config Config) *Interceptor {
    return &Interceptor{
        config: config,  // Convert to Options internally
        // ...
    }
}

// In Handle() function
func (i *Interceptor) Handle(ctx context.Context, task runtime.Task, 
    next func(context.Context, runtime.Task) (*runtime.Result, error)) (*runtime.Result, error) {
    
    // Extract options from task (untyped!)
    opts := task.Options  // ← runtime.Options = attrs.Attributes
    
    maxAttempts := i.config.MaxAttempts  // From Config
    // But task.Options might override! How? Unclear.
}
```

**Impact:**
- Adding new field requires changes in 2+ locations
- Unclear how task.Options overrides config
- No clear contract about precedence
- Difficult to version/evolve

---

### 6. ProcessPool ↔ Worker Execution Coupling

**Location:** `wippy/service/host/pool.go`

```go
type ProcessPool struct {
    workers      int
    numProcesses atomic.Int32
    maxProcesses int
    log          *zap.Logger
    processes    sync.Map  // map[relay.Target]*processEntry
    workCh       chan relay.PID
    wg           sync.WaitGroup
    processWG    sync.WaitGroup
    ctx          context.Context
    cancel       context.CancelFunc
}

type processEntry struct {
    process process.Process  // ← Tightly coupled to process interface
    source  registry.ID
    running atomic.Bool
    awaken  atomic.Bool
}
```

**Coupling Problem:**
- ProcessPool directly stores process instances
- No abstraction layer for process execution
- Worker goroutines hard-coded to call p.Step()
- Can't support different execution models (async, streaming, etc.)

**Impact:**
- Must implement full Process interface to be poolable
- Can't add features like priority scheduling without changing pool
- Hard to add metrics/observability hooks
- Worker count fixed at creation time

---

## Dependency Direction Analysis

```
Current Dependencies:

System Layer ─→ API Layer
(system/process/manager.go uses api/process interfaces)
    │
    └─→ Knows about Host types (Managed vs Delegated)

Service Layer ─→ API Layer
(service/host/* uses api/process and api/runtime)
    │
    └─→ Knows about Start/Launch/Process interfaces
    └─→ Tightly couples to ProcessPool interface

Runtime Layer ─→ API Layer
(runtime/lua/* uses api/process)
    │
    └─→ Extends process.Process interface
    └─→ Must handle all context options

Bootstrap Layer ─→ All Layers
(boot/* knows about System, Service, Runtime)
    │
    └─→ Assembles everything together

Problem: Circular conceptual dependencies
├─ Manager needs to know host types
├─ Host needs to know about context options
├─ Runtime needs to know what options are available
└─ Bootstrap needs to orchestrate all of above
```

---

## Adding New Options: Coupling Impact

### Scenario: Add "timeout" option to processes

```
1. API Layer (api/process/process.go)
   └─ NO CHANGE
      └─ Uses untyped context.Pair, so nothing to change
      
2. System Layer (system/process/manager.go)
   └─ NO CHANGE
      └─ Just passes through Start.Context
      
3. Service Layer (service/host/host.go)
   ├─ MINIMAL CHANGE: prepareContext()
   └─ Could apply timeout at this stage
       └─ Example: if val, ok := pairs[i].Key.(string); ok && val == "timeout"
       
4. Runtime Layer (runtime/lua/component/process)
   ├─ REQUIRED CHANGE: p.state.InitContext() or Start()
   ├─ Extract timeout from context
   └─ Apply to Lua runner/environment
   
5. Bootstrap Layer
   └─ CONDITIONAL CHANGE:
      └─ If config-time option: modify EntryConfig
      └─ If runtime: modify where options are documented

RESULT: Changes scattered across 2-3 layers, no schema validation
```

### Scenario: Add "maxRetries" as host config option

```
1. API Layer
   └─ CHANGE: api/service/host/config.go
      └─ Add field to Config struct
      
2. System Layer
   └─ NO DIRECT CHANGE
      └─ But HostManager might validate config
      
3. Service Layer
   ├─ CHANGE: service/host/host.go
   └─ CHANGE: service/host/factory.go
      └─ Pass config to retry interceptor
      
4. Bootstrap Layer
   ├─ CHANGE: boot/components/service/service/host.go
   └─ CHANGE: boot/components/service/service/interceptor_retry.go
   
5. Interceptor Layer (if used at runtime)
   ├─ CHANGE: service/interceptor/retry/interceptor.go
   └─ CHANGE: api/service/interceptor/retry/config.go
      └─ If Options struct needs different MaxRetries

RESULT: Changes in 5+ files, multiple layers affected
```

---

## Recommendations to Reduce Coupling

### Short Term (Low Risk)

1. **Document implicit contracts**
   - Create OPTION_SCHEMA.md listing all valid context.Pair keys
   - Document where each option is used (System, Service, Runtime layer)
   - Add deprecation warnings for planned changes

2. **Add basic validation in Service layer**
   ```go
   // In prepareContext()
   func validateContextPairs(pairs []context.Pair) error {
       for _, p := range pairs {
           key, ok := p.Key.(string)
           if !ok {
               return fmt.Errorf("context key must be string, got %T", p.Key)
           }
           if !isKnownOption(key) {
               return fmt.Errorf("unknown option: %s", key)
           }
       }
       return nil
   }
   ```

### Medium Term

1. **Typed ProcessOptions struct**
   ```go
   type Start struct {
       HostID   relay.HostID
       Source   registry.ID
       Input    payload.Payloads
       Lifecycle Lifecycle
       Context  []context.Pair              // Keep for backwards compat
       Options  *ProcessOptions             // Add typed options
   }
   
   type ProcessOptions struct {
       Timeout     time.Duration
       MaxRetries  int
       Actor       string
       Custom      map[string]interface{}   // Extensibility
   }
   ```

2. **OptionsRegistry**
   ```go
   type OptionsRegistry interface {
       Register(name string, opts OptionSpec)
       Validate(opts ProcessOptions) error
       Schema() map[string]OptionSpec
   }
   
   type OptionSpec struct {
       Name        string
       Type        string
       Default     interface{}
       Validator   func(interface{}) error
       AppliedAt   Layer  // API, System, Service, Runtime
   }
   ```

3. **Delegated Host Source passing**
   - Change Delegated.Launch() signature to include Source
   - Or use Launch context to pass Source

### Long Term

1. **Process execution abstraction**
   - Introduce ProcessExecutor interface above ProcessPool
   - Support different execution models
   - Enable priority queues, batching, etc.

2. **Options inheritance model**
   - Host-level defaults
   - Task-level overrides
   - Runtime-level application
   - Clear precedence rules

3. **Configuration validation**
   - Compile-time validation of option combinations
   - Runtime validation with clear error messages
   - Discovery mechanism for tools/IDEs

