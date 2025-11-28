# Low Engine v2 - Yield-Based Scheduler Design

## Problems Solved

### 1. Coroutine Layer Overhead
**Before:** Every async call (even simple SQL) spawns goroutine via `uw.Run()`.
**After:** Clean yields go to external dispatcher, no goroutine per call.

### 2. Upstream Hack
**Before:** `upstream.NewRequest()` creates channel, `SendAndYield()` couples scheduler to internal channels.
**After:** Channels never leave process boundary. External yields are pure data.

### 3. Callback Coupling
**Before:** Callbacks/channels passed with yields, anyone could invoke them (race conditions).
**After:** Callbacks registered in Frame Context, invoked only during Process.Step().

### 4. Unified Model
**Before:** Different code paths for processes vs workflows vs Temporal.
**After:** Same yield protocol for all hosts. Only dispatcher implementation differs.

## Architecture Layers

```
┌─────────────────────────────────────────────────────────────┐
│ Frame Context (per-execution sandbox)                       │
│                                                             │
│ - pendingYields map[YieldID]func(data, err)                │
│ - resources []CleanupFunc                                   │
│ - values map[string]any                                     │
│                                                             │
│ RegisterYield(id, callback)   // called by Lua modules     │
│ GetCallback(id) callback      // called during Step()      │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│ Process (pure state machine)                                │
│                                                             │
│ Start(ctx, input) error                                     │
│ Step(results *YieldResults) (StepResult, error)            │
│ Send(pkg *Package) error                                    │
│                                                             │
│ Internal layer during Step():                               │
│   - receives YieldResults from scheduler                    │
│   - looks up callbacks in frame context                     │
│   - invokes callbacks (routes to channels internally)       │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼ yields []Command (clean, no callbacks)
┌─────────────────────────────────────────────────────────────┐
│ Scheduler (work-stealing, zero-alloc hot path)              │
│                                                             │
│ - workers []*Worker (each owns local Deque)                 │
│ - global *ConcurrentQueue (for cross-worker distribution)   │
│ - registry *Registry (CommandID -> Handler, O(1) lookup)    │
│                                                             │
│ Submit(ctx, process, input) → schedules process             │
│ Processor.Complete(data, err) → stores result, wakes proc   │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│ Handler (per-command dispatcher)                            │
│                                                             │
│ Handle(cmd Command, proc *Processor)                        │
│   - executes command (sync or async)                        │
│   - calls proc.Complete(data, err) when done               │
│   - NO knowledge of frame context or callbacks              │
└─────────────────────────────────────────────────────────────┘
```

## Yield Flow (Corrected)

```
1. Lua module (e.g., funcs.async):
   └─> creates internal channel
   └─> frame.RegisterYield(yieldID, func(d,e) { channel.Send(d) })
   └─> returns channel to Lua code

2. Process.Step() returns:
   └─> []Command with clean data (yieldID, params)
   └─> NO callbacks in yields

3. Scheduler dispatches Command to Handler:
   └─> Handler executes (sync or spawns goroutine for async)
   └─> Handler calls proc.Complete(data, err)
   └─> Handler has NO context/frame awareness

4. Scheduler receives completion:
   └─> stores (data, err) in processor's YieldResults
   └─> marks processor ready
   └─> pushes to queue

5. Worker picks up processor, calls Process.Step(yieldResults):
   └─> Process INTERNAL layer receives results
   └─> Looks up callback in frame context by yieldID
   └─> Invokes callback (routes to channel)
   └─> Lua code can now receive from channel
```

## Key Invariants

1. **External yields are pure data** - Command + YieldID, no callbacks/channels
2. **Callbacks live in Frame Context** - registered by Lua modules
3. **Callbacks invoked during Step()** - inside process boundary only
4. **Handlers are context-unaware** - just execute and call Complete()
5. **Scheduler is routing-unaware** - just delivers results to process

## Yield Types

### Blocking Yield (sync pattern)
```lua
local result = sql.query("SELECT 1")  -- blocks until result
```
- Result stored in YieldResults
- Process wakes when yield completes
- No channel involved

### Background Yield (async pattern)
```lua
local ch = funcs.async(myFunc)  -- returns immediately
timer.sleep(5)                   -- THIS is the blocking yield
local result = ch:receive()      -- channel may already have data
```
- Channel created internally by funcs module
- Callback registered in frame context
- When async completes, callback routes to channel
- Only timer.sleep (blocking yield) controls wake

## Data Structures

### Chase-Lev Deque (per-worker local queue)
- Owner pushes/pops from bottom (LIFO, cache-friendly)
- Thieves steal from top (FIFO, older/bigger tasks)
- Lock-free for owner operations
- CAS for steal operations

### ConcurrentQueue (global queue)
- Thread-safe ring buffer with mutex
- Used for: new submissions, cross-worker transfers
- Grows on demand (rare, only at startup)

### Registry (command dispatch)
- Fixed array[256] for O(1) lookup by CommandID
- CommandID is uint8 for efficiency

## What Gets Removed

- `uw.Run()` - goroutine spawning per async call
- `upstream.SendAndYield()` - channel coupling
- `upstream.NewRequest()` as external concept
- Coroutine layer's `Wrap()` pattern
- Complex Task abstractions in engine
- TaskScheduler in UnitOfWork

## What Stays

- Channel API in Lua (internal to process)
- `channel:select()` unchanged
- Frame Context for per-execution state
- VM pooling for state reuse
- Resource cleanup handlers

## VM State Separation

```
┌─────────────────────────────────────┐
│ VM (pooled, reused across requests) │
│ - Lua state                         │
│ - Loaded modules                    │
│ - Compiled bytecode                 │
│ - Global tables                     │
└─────────────────────────────────────┘
            ↓ borrowed for execution
┌─────────────────────────────────────┐
│ Frame Context (per-execution)       │
│ - pendingYields                     │
│ - resources                         │
│ - request-scoped values             │
└─────────────────────────────────────┘
```

## Success Criteria

- [ ] Zero allocations in scheduler hot path
- [ ] Work stealing functional with multiple workers
- [ ] Same yield format works for all hosts (process, workflow, Temporal)
- [ ] Handlers completely decoupled from frame/callback routing
- [ ] Benchmarks: target 1M+ yields/sec
