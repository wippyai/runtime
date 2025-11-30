# Function Pool Hook Architecture Problem

## The Issue

**Current funcpool hooks are called at the wrong lifecycle stage.**

### Current (Wrong) Behavior

```go
// Hooks called at PROCESS lifetime (once per pool process)
type Hooks struct {
    OnStart OnStart  // Called when process CREATED in pool
    OnStop  OnStop   // Called when process DESTROYED from pool
}

// Called ONCE per process:
factory := WrapFactoryWithHooks(baseFactory, Hooks{
    OnStart: func(proc Process) { ... },  // ❌ No context, no PID
    OnStop:  func(proc Process) { ... },   // ❌ No context, no result
})
```

**Problem**: A pooled process lives for **many executions**. These hooks are called once when the process is created/destroyed, NOT per execution.

### What We Need (Correct) Behavior

```go
// Hooks called at EXECUTION lifetime (once per Call())
type ExecutionHooks struct {
    OnStart  func(ctx context.Context, proc Process)              // Per execution
    OnComplete func(ctx context.Context, result *runtime.Result)  // Per execution
}

// Called for EVERY execution:
pool.Call(ctx, method, input)
    ↓
OnStart(ctx, proc)           // ← With execution context!
    ↓
proc.Execute(ctx, ...)
    ↓
proc.Step() loop...
    ↓
OnComplete(ctx, result)      // ← With execution context & result!
```

---

## Why This Matters for Topology

### Topology Registration Pattern

From `system/process/mutator_topology.go`:

```go
// OnStart hook (PER EXECUTION)
topologyOnStart := func(_ context.Context, pid relay.PID, _ api.Process) {
    topo.Register(pid)              // Register THIS execution's PID
    if monitor {
        topo.Wait(parentPID, pid)   // Set up monitoring
    }
}

// OnComplete hook (PER EXECUTION)
topologyOnComplete := func(_ context.Context, pid relay.PID, result *runtime.Result) {
    topo.Notify(pid, result)        // Notify watchers
    pidReg.Remove(pid)              // Cleanup
    topo.Remove(pid)                // Remove from topology
}
```

**This MUST happen per execution**, not per process creation!

### Why Functions Need This

1. **Monitoring**: Another process monitors a function execution
2. **Linking**: Parent process links to function execution
3. **Cleanup**: When function completes, watchers need notification
4. **PID Management**: Each execution has its own PID

Without execution-level hooks, **functions cannot participate in topology**.

---

## Execution Lifecycle Comparison

### ✅ Actor Scheduler (Correct - host2)

```
Process.Start() call
    ↓
Create process (NOT pooled)
    ↓
scheduler.Submit(ctx, pid, process, method, input)
    ↓
OnStart hook with ctx, pid, process ← TOPOLOGY REGISTRATION
    ↓
process.Execute(ctx, method, input)
    ↓
Worker: process.Step() loop
    ↓
scheduler.completeProcessor()
    ↓
OnComplete hook with ctx, pid, result ← TOPOLOGY CLEANUP
    ↓
Process destroyed
```

**Key**: OnStart/OnComplete called **per execution** with **execution context**.

### ❌ Funcpool (Wrong - current)

```
Pool creation (ONCE)
    ↓
factory() creates process
    ↓
WrapFactoryWithHooks
    ↓
OnStart(proc) ← Called ONCE, no context ❌
    ↓
Process stored in pool
    ↓

    For Each Call():
        pool.Call(ctx, method, input)
        ↓
        (NO HOOKS CALLED) ❌
        ↓
        executor.Run(ctx, proc, method, input)
        ↓
        proc.Execute(ctx, method, input)
        ↓
        proc.Step() loop
        ↓
        Return result
        ↓
        (NO HOOKS CALLED) ❌

    (Process reused many times...)

Pool shutdown (ONCE)
    ↓
OnStop(proc) ← Called ONCE, no context ❌
    ↓
Process destroyed
```

**Problem**: Hooks called at pool lifetime, not execution lifetime. No access to execution context, PID, or result.

---

## Context Flow in Funcpool

Each execution has its own context:

```
pool.Call(ctx context.Context, method, input)  ← Caller's context
    ↓
request{ctx, method, input, resultCh}
    ↓
Worker receives request
    ↓
executor.Run(ctx, proc, method, input)  ← Context flows through
    ↓
proc.Execute(ctx, method, input)  ← Context attached to process
    ↓
p.ctx = ctx                       ← Stored for THIS execution
p.state.SetContext(ctx)
fc := FrameFromContext(ctx)       ← Access execution frame
```

**Each execution context is separate** - but there's no hook infrastructure to use it!

---

## The Fix: Add Execution-Level Hooks

### Option 1: Add to Executor (Minimal Change)

Modify `system/scheduler/pool/pool.go`:

```go
type Executor struct {
    dispatcher Dispatcher
    hooks      ExecutionHooks  // NEW
}

type ExecutionHooks struct {
    OnStart    func(ctx context.Context, proc process2.Process)
    OnComplete func(ctx context.Context, result *runtime.Result)
}

func (e *Executor) Run(ctx, proc, method, input) *runtime.Result {
    // Call OnStart hook
    if e.hooks.OnStart != nil {
        e.hooks.OnStart(ctx, proc)  // ← WITH CONTEXT!
    }

    if err := proc.Execute(ctx, method, input); err != nil {
        return &runtime.Result{Error: err}
    }

    // ... step loop ...

    result := ... // get result

    // Call OnComplete hook
    if e.hooks.OnComplete != nil {
        e.hooks.OnComplete(ctx, result)  // ← WITH CONTEXT & RESULT!
    }

    return result
}
```

### Option 2: Add to Pool Interface (More Flexible)

```go
type Pool interface {
    Call(ctx, method, input) (*runtime.Result, error)
    SetExecutionHooks(hooks ExecutionHooks)  // NEW
    Start()
    Stop()
}
```

Then each pool implementation calls hooks in its execution loop.

### Option 3: Use Middleware Pattern

```go
type ExecutionMiddleware func(next ExecutionFunc) ExecutionFunc

type ExecutionFunc func(ctx, method, input) (*runtime.Result, error)

// Example:
func TopologyMiddleware(topo Topology) ExecutionMiddleware {
    return func(next ExecutionFunc) ExecutionFunc {
        return func(ctx, method, input) (*runtime.Result, error) {
            pid := getPIDFromContext(ctx)
            topo.Register(pid)                    // OnStart

            result, err := next(ctx, method, input)

            topo.Notify(pid, result)              // OnComplete
            topo.Remove(pid)

            return result, err
        }
    }
}
```

---

## Why Actor Scheduler Works

The actor scheduler doesn't pool processes - each execution gets a fresh process:

**Location**: `system/scheduler/actor/scheduler.go:204-229`

```go
func (s *Scheduler) Submit(ctx, pid, p Process, method, input) (*Processor, error) {
    if err := p.Execute(ctx, method, input); err != nil {
        return nil, err
    }

    // Create processor for THIS execution
    proc := acquireProcessor()
    proc.ID = s.nextID.Add(1)
    proc.PID = pid
    proc.Process = p
    proc.ctx = ctx  // ← Execution context

    s.byID.Store(proc.ID, p)
    s.byPID.Store(pid, proc.ID)

    s.global.Push(proc)
    s.wakeWorker()

    // OnStart called PER EXECUTION
    if s.onStart != nil {
        s.onStart(ctx, pid, p)  // ← WITH CONTEXT AND PID!
    }

    return proc, nil
}
```

And at completion:

**Location**: `system/scheduler/actor/scheduler.go:293-323`

```go
func (s *Scheduler) completeProcessor(proc *Processor, yields, err) {
    result := &runtime.Result{Error: err}

    if len(yields) > 0 {
        if p, ok := yields[0].(payload.Payload); ok {
            result.Value = p
        }
    }

    // OnComplete called PER EXECUTION
    if s.onComplete != nil {
        s.onComplete(proc.ctx, proc.PID, result)  // ← WITH CONTEXT, PID, RESULT!
    }

    // Cleanup...
}
```

**Perfect for topology**: Each execution has context, PID, and result available.

---

## Impact Analysis

### What Breaks Without This

1. ❌ **Function monitoring**: Cannot monitor function executions
2. ❌ **Function linking**: Cannot link to function executions
3. ❌ **Topology cleanup**: Function PIDs leak in topology
4. ❌ **Relay registration**: Functions cannot receive messages mid-execution
5. ❌ **Resource tracking**: Cannot track per-execution resources in topology

### What Works Now

✅ **Process spawning via host2**: Uses actor scheduler, has correct hooks
✅ **Workflow spawning**: Temporal manages lifecycle differently
✅ **Terminal execution**: Has its own lifecycle management

### Migration Path

**Phase 1** (Now):
- Document the issue (this file)
- Use host2 for process.lua (when we create process2 component)
- Functions continue without topology support

**Phase 2** (Future):
- Add ExecutionHooks to Executor
- Update pool implementations to call hooks
- Enable topology support for functions

**Phase 3** (Future):
- Deprecate process-lifetime hooks
- Migrate all code to execution-lifetime hooks

---

## References

**Correct Implementation**:
- `service/host2/host.go:74-78` - OnStart hook execution per launch
- `service/host2/manager.go:53-60` - OnComplete hook in scheduler callback
- `system/process/mutator_topology.go:39-86` - Topology hook pattern

**Current (Wrong) Implementation**:
- `system/scheduler/pool/pool.go:70-103` - Process-lifetime hooks
- `system/scheduler/pool/static.go:43-66` - Pool creation with hooks

**Core Execution Loop**:
- `system/scheduler/pool/pool.go:140-184` - Executor.Run (where hooks SHOULD be)
- `runtime/lua/engine2/process.go:279-364` - Process.Execute (context attachment)

---

## Recommendation

**For now**: Accept that functions don't participate in topology. This is documented and understood.

**For later**: Add execution-level hooks to funcpool when we need function monitoring/linking.

The architecture is sound - we just need to add the hook points at the right lifecycle stage.
