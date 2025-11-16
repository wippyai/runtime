# Process Options Data Flow - Visual Guide

## 1. Process Startup Sequence

```
User Code (e.g., Lua script)
    │
    ├─ Create Start config
    │  ├─ HostID: "process.local"
    │  ├─ Source: registry.ID{NS: "lua", Name: "my_process"}
    │  ├─ Input: payload.Payloads{...}
    │  ├─ Lifecycle: {Monitor: true, Parent: caller_pid}
    │  └─ Context: []context.Pair{
    │         {Key: ValuesCtx, Value: custom_values},
    │         {Key: ActorCtx, Value: "admin"},
    │         ...
    │     }
    │
    └─→ Manager.Start(ctx, start)
        │
        ├─ Get host from registry
        ├─ Allocate PID
        │
        └─→ Manager.launchOnHost()
            │
            ├─ (Managed Host Path)
            │  ├─ prototypes.Create(Source) → Process instance
            │  │
            │  └─→ Host.Launch(ctx, &Launch{
            │         PID: allocated_pid,
            │         Source: start.Source,
            │         Process: created_process,
            │         Input: start.Input,
            │         Lifecycle: start.Lifecycle,
            │         Context: start.Context  ← PASSED THROUGH
            │     })
            │
            └─ (Delegated Host Path)
               └─→ Host.Launch(ctx, pid, lifecycle, input)
```

## 2. Context Preparation in Service Layer

```
Host.Launch(ctx, launch)
    │
    └─→ prepareContext(ctx, launch)
        │
        ├─ Opens new FrameContext
        │  └─ Inherits values from parent context
        │
        ├─ Builds pair array (3 + custom)
        │  ├─ [0] FrameIDKey: launch.Source
        │  ├─ [1] FramePIDKey: launch.PID
        │  ├─ [2] FrameHostKey: host.id
        │  └─ [3..N] launch.Context pairs ← CUSTOM OPTIONS
        │
        ├─ Applies pairs via fc.SetMultiple()
        │  └─ Values now in FrameContext
        │
        ├─ AttachLifecycle(ctx, launch.Lifecycle)
        │  └─ Registers OnStart/OnComplete callbacks
        │
        └─ SetOnComplete callback
           └─ Local host-level cleanup

       Result: Fully prepared context with:
       ├─ Inherited AppContext values
       ├─ Standard frame metadata
       ├─ Custom options from Start.Context
       └─ Lifecycle callbacks attached
```

## 3. Options Visibility Across Layers

```
┌──────────────────────────────────────────────────────────────┐
│ API LAYER (api/process/process.go)                           │
│                                                              │
│ type Start struct {                                         │
│   Context []context.Pair  ← Options as untyped pairs      │
│ }                                                            │
│                                                              │
│ What's visible: NONE - just interface definition           │
└──────────────────────────────────────────────────────────────┘
                          ↓ passes Start
┌──────────────────────────────────────────────────────────────┐
│ SYSTEM LAYER (system/process/manager.go)                    │
│                                                              │
│ Manager.Start(ctx, start) {                               │
│   start.Context → launch.Context  (pass-through)           │
│ }                                                            │
│                                                              │
│ What's visible: NONE - just forwarded                      │
└──────────────────────────────────────────────────────────────┘
                          ↓ passes Launch
┌──────────────────────────────────────────────────────────────┐
│ SERVICE LAYER (service/host/host.go)                        │
│                                                              │
│ Host.Launch(ctx, launch) {                                │
│   ctx = prepareContext(ctx, launch)                        │
│   launch.Context pairs → applied to FrameContext            │
│ }                                                            │
│                                                              │
│ What's visible: Used but not validated/documented          │
└──────────────────────────────────────────────────────────────┘
                          ↓ prepared context
┌──────────────────────────────────────────────────────────────┐
│ RUNTIME LAYER (runtime/lua/component/process/process.go)   │
│                                                              │
│ LuaProcess.Start(ctx, pid, input) {                       │
│   ctx → p.state.InitContext(ctx, pid)                     │
│   Values accessed via context.Get() in Lua code            │
│ }                                                            │
│                                                              │
│ What's visible: Query via context.Get() calls              │
└──────────────────────────────────────────────────────────────┘
```

## 4. Configuration vs Runtime Options

```
┌─────────────────────────────────────────────────────────────────┐
│                     OPTIONS TYPES MATRIX                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│ BOOTSTRAP/CONFIG (immutable after host creation)               │
│ ├─ api/service/host/config.go:EntryConfig                     │
│ │  ├─ MaxProcesses: int          ← Pool sizing               │
│ │  ├─ Workers: int               ← Worker count              │
│ │  ├─ BufferSize: int            ← Queue size               │
│ │  └─ MessageWorkerCount: int     ← Message workers          │
│ │                                                             │
│ └─ Stored in service/host/Host struct                        │
│    └─ Used at startup, not changeable after                  │
│                                                              │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│ RUNTIME OPTIONS (per process/task)                            │
│ ├─ api/process/process.go:Start.Context                      │
│ │  └─ []context.Pair → untyped key-value pairs             │
│ │                                                             │
│ ├─ api/runtime/task.go:Task.Options                          │
│ │  └─ runtime.Options (alias attrs.Attributes)              │
│ │     └─ Also untyped key-value                             │
│ │                                                             │
│ └─ Used in api/process/process.go:Launch.Context            │
│    └─ Applied per-process at startup                         │
│                                                              │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│ INTERCEPTOR OPTIONS (hybrid)                                  │
│ ├─ api/service/interceptor/retry/config.go:Config           │
│ │  └─ Bootstrap config (Enabled, MaxAttempts)               │
│ │                                                             │
│ └─ api/service/interceptor/retry/config.go:Options          │
│    └─ Runtime options (MaxAttempts, BackoffMs)              │
│       └─ DUPLICATED, not inherited                          │
│                                                              │
└─────────────────────────────────────────────────────────────────┘
```

## 5. Where Options Are Accessed/Used

```
Process Lifecycle: Option Access Points

Start(ctx, pid, input)
    │
    ├─ p.state.InitContext(ctx, pid)
    │  ├─ Context available here
    │  ├─ Can extract options via context.Get()
    │  └─ Examples:
    │     ├─ Values Bag for custom data
    │     ├─ Actor for permission context
    │     ├─ Scope for isolation
    │     └─ Custom context.Pair values
    │
    ├─ OnStart callback(pid, process)
    │  ├─ Topology registration happens here
    │  └─ Options NOT directly accessible
    │
    └─ p.state.Start(input, onStartFunc)
        │
        └─ Lua code execution begins
           ├─ Access options via runtime API calls
           ├─ Example: context.get_value(key)
           └─ Falls back to context.Get(key)

Step()
    ├─ p.state.Step(false)
    └─ Executes one iteration
       └─ Options still accessible via context

OnComplete callback(pid, result)
    ├─ Topology notifications
    ├─ PID cleanup
    └─ Options NOT directly accessible
```

## 6. Options Addition Impact Map

```
To add new option "timeout" to processes:

1. API LAYER
   └─ api/process/process.go
      └─ NO CHANGE (uses untyped context.Pair)
      └─ Just document: "timeout: duration string"

2. SYSTEM LAYER
   └─ system/process/manager.go
      └─ NO CHANGE (passes through)

3. SERVICE LAYER  ← MINIMAL CHANGE
   └─ service/host/host.go:prepareContext()
      ├─ Can apply timeout immediately
      ├─ Example: ctx.WithTimeout() if key == "timeout"
      └─ OR just pass through (no validation)

4. RUNTIME LAYER  ← REQUIRED CHANGE
   └─ runtime/lua/component/process/process.go
      ├─ p.state.InitContext() or Start()
      ├─ Extract timeout from context
      ├─ Apply to Lua state/runner
      └─ Example:
         if timeout := context.Get("timeout"); timeout != nil {
             p.state.SetTimeout(timeout.(time.Duration))
         }

5. BOOTSTRAP LAYER
   └─ NO CHANGE needed if purely runtime
      └─ IF config-time: add to EntryConfig + validation

RESULT: Changes in 2 files (if runtime only) or 3+ (if config)
NO TYPE SAFETY, NO DISCOVERY, SILENT FAILURES
```

## 7. The "Pair" Array Problem

```
current Start struct:
┌────────────────────────────────────┐
│ type Start struct {                │
│   Context []context.Pair           │
│ }                                  │
│                                    │
│ // context.Pair is:                │
│ type Pair struct {                 │
│   Key   interface{}    ← Any type! │
│   Value interface{}    ← Any type! │
│ }                                  │
└────────────────────────────────────┘

Problems with this approach:

1. TYPE SAFETY
   ✗ Can't validate at compile time
   ✗ Can't auto-complete in IDE
   ✗ Can't generate docs
   ✗ Runtime errors possible

2. DISCOVERY
   ✗ No way to query "what options are valid?"
   ✗ No schema for tools to analyze
   ✗ Documentation scattered

3. VALIDATION
   ✗ No schema to validate against
   ✗ Invalid options silently ignored
   ✗ Typos cause silent failures
   │  example: {Key: "timeout", Value: "5s"} with typo "timout"
   │  result: timeout not applied, no error

4. DOCUMENTATION
   ✗ Only place is comments (easily outdated)
   ✗ Multiple layers, multiple places to document
   ✗ No canonical reference

Better approach (example):
┌────────────────────────────────────┐
│ type Start struct {                │
│   Context []context.Pair           │
│   ProcessOptions *ProcessOptions   │ ← Typed!
│ }                                  │
│                                    │
│ type ProcessOptions struct {        │
│   Timeout time.Duration           │
│   Retry  *RetryOptions            │
│   Actor  string                   │
│ }                                  │
└────────────────────────────────────┘

Benefits:
✓ Type safe
✓ IDE auto-complete
✓ Validatable
✓ Discoverable via reflection
✓ Can evolve with versions
```

## 8. Current vs Ideal Configuration Hierarchy

```
CURRENT STATE (fragmented):

api/service/host/config.go
├─ EntryConfig
│  ├─ HostConfig {MaxProcesses, Workers, BufferSize, ...}
│  └─ Lifecycle {StopTimeout}

api/service/interceptor/retry/config.go
├─ Config {Enabled, MaxAttempts}
└─ Options {MaxAttempts, BackoffMs} ← DUPLICATED

api/runtime/task.go
└─ Task.Options (untyped key-value)

api/process/process.go
├─ Start.Context (untyped pairs)
└─ Start.Lifecycle {Parent, Monitor, Link}

PROBLEMS:
- Multiple sources of truth
- Duplication (retry config vs options)
- Untyped options (Start.Context, Task.Options)
- No central registry
- No validation


IDEAL STATE (unified):

config/
├─ host.go
│  └─ HostConfig {
│       ProcessPool {MaxProcesses, Workers}
│       MessageQueue {BufferSize, WorkerCount}
│       Lifecycle {StopTimeout}
│     }
│
├─ process.go
│  └─ ProcessOptions {
│       Timeout time.Duration
│       Retry *RetryOptions
│       Metrics *MetricsOptions
│       Custom ...
│     }
│
└─ interceptors.go
   └─ InterceptorOptions {
       Retry {MaxAttempts, BackoffMs}
       OpenTelemetry {SpanName, Attributes}
     }

registry/
└─ options_registry.go
   ├─ RegisterOption(name, validator, defaults)
   ├─ ValidateOptions(opts) error
   └─ GetOptionSchema() OptionSchema

BENEFITS:
✓ Single source of truth
✓ Type safety
✓ Centralized validation
✓ Discoverable schema
✓ Easy to extend
```

---

## Summary: Key Insights for Option Handling

1. **No Schema at API Level** - `Start.Context` is untyped pairs
2. **Pass-Through in System** - Options forwarded unchanged
3. **Applied in Service** - Context pairs set in FrameContext
4. **Used in Runtime** - Via context.Get() calls
5. **No Validation** - Invalid options silently ignored
6. **Duplication** - Retry interceptor has Config AND Options
7. **Configuration Scattered** - Host config used in multiple layers
8. **Type Unsafe** - Options as interface{} key-value pairs
9. **Hard to Extend** - Changes needed in multiple files

**Recommendation:** Create typed ProcessOptions struct at API level before expanding options further.
