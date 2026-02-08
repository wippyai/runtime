# Wippy Workflow + WASM Deep Assessment

Status: Draft v0.2  
Date: 2026-02-07  
Scope: local Temporal-like engine, cluster coordination, WASM integration permutations

## 1. Inputs Reviewed

This assessment is based on direct code scan of:

- `wippy` core process/scheduler/topology/runtime + Temporal adapter code
- `wasmexp` runtime/engine/resource/WASI implementations
- desktop draft `__wasm` integration prototype

Key reviewed files include:

- `api/process/process.go`
- `api/process/step.go`
- `api/process/queue.go`
- `system/scheduler/actor/scheduler.go`
- `system/scheduler/actor/worker.go`
- `runtime/lua/engine/process.go`
- `service/host/host.go`
- `system/topology/topology.go`
- `system/topology/pid_registry.go`
- `cluster/membership/membership.go`
- `service/temporal/workflow/execution.go`
- `service/temporal/workflow/handler_process.go`
- `service/temporal/workflow/timer_manager.go`
- `/home/wolfy-j/projects/wasmexp/runtime/runtime.go`
- `/home/wolfy-j/projects/wasmexp/runtime/module.go`
- `/home/wolfy-j/projects/wasmexp/runtime/instance.go`
- `/home/wolfy-j/projects/wasmexp/runtime/wasi.go`
- `/home/wolfy-j/projects/wasmexp/runtime/python_test.go`
- `/mnt/c/Users/Wolfy-J/Desktop/__old/_-ready/__redo/__wasm/runtime_wasm/engine/process.go`
- `/mnt/c/Users/Wolfy-J/Desktop/__old/_-ready/__redo/__wasm/runtime_wasm/component/component/manager.go`

## 2. What Exists Today (Important for Lift Estimation)

### 2.1 Process Runtime Core Is Already Strong

- `wippy` already has a clean event-driven process contract (`Init/Step/Close`).
- Scheduler already supports:
  - yield correlation by `Tag`
  - message event delivery
  - race-safe wakeup/blocked transitions
  - pooled processor reuse with queue generation protection
- This means a local workflow engine can reuse the existing deterministic step loop model.

### 2.2 Cluster Data Plane Exists

- Membership + internode transport are already in place.
- Relay router can route local/peer/internode traffic.
- This is enough for node discovery and message transport.

### 2.3 Control Plane Is Partial

- PID/name registry is currently in-memory (`system/topology/pid_registry.go`).
- Topology monitor/link state is also process-memory on each node.
- There is no global durable name registry or global workflow ownership ledger yet.

Conclusion: data plane is mostly ready; durable control plane is the main missing piece.

### 2.4 Temporal Adapter Already Encodes Semantics to Preserve

Temporal adapter currently defines concrete behavior for:

- process send routing (self/update/temporal target/local relay)
- child spawn and exit propagation
- unsupported monitor/link commands in workflow context
- timer/ticker behavior via deterministic workflow events

This is exactly the seam to abstract.

## 3. Should We Have One or Two Workflow Implementations?

Decision: keep one workflow semantic layer, two backends.

- Keep one canonical workflow semantics package:
  - signal behavior
  - child behavior
  - timer semantics
  - deterministic replay rules
  - option normalization
- Put backend-specific execution/persistence in adapters:
  - Temporal adapter (existing)
  - Local DB adapter (new)

Do not fork semantics into "Temporal version" and "local version". That doubles long-term maintenance cost and drifts quickly.

## 4. Cluster Coordination Strategy (Narrowed to Workflow Domain)

### 4.1 What Must Be Strongly Consistent

- Workflow history append order per run
- Task claim lease ownership
- Timer ownership/fire dedupe
- Workflow ID uniqueness/conflict policy

Use DB transactions for this in v1.

### 4.2 What Can Be Eventually Consistent

- Node membership discovery
- Sticky worker hints
- Local routing caches
- Best-effort placement hints

Use gossip/memberlist + cache here.

### 4.3 Do We Need Raft Immediately?

No, not for v1 local workflow engine correctness.

You can start with:

- Postgres as source of truth for workflow control plane
- lease-based workers (`SELECT ... FOR UPDATE SKIP LOCKED` pattern)
- gossip only for membership/health/soft placement

Add Raft only if you later need independent non-DB control-plane guarantees (for example multi-primary control metadata without DB dependency).

## 5. Semantics Feasibility vs Temporal

### 5.1 Can We Match Temporal-like Semantics?

Mostly yes at workflow layer:

- deterministic replay
- durable timers
- signals
- child workflows
- continue-as-new
- activity retries

### 5.2 At-most-once?

Not end-to-end at distributed system boundary.

You can get:

- workflow state transitions: effectively-once via event log + replay
- activities: at-least-once (require idempotency key discipline)

This matches practical Temporal usage model (activities are not globally exactly-once).

### 5.3 Timers and Crons

Feasible in DB-backed engine:

- timers: rows + due-time index + lease-claimed firing tasks
- cron: scheduler rows that enqueue runs deterministically
- dedupe: unique key on `(run_id, timer_id, fire_seq)` style keys

## 6. WASM Integration Permutation Matrix

| Mode | Feasibility | Notes |
|---|---|---|
| Component WASM as function (`function.wasm`) | High | Best first target; desktop draft already proved shape. |
| Core WASM + WIT as function | High | `wasmexp` supports `LoadWASM` + WIT signatures. |
| Inline WAT as function (`function.wat`) | High | Great for dev/test and rapid iteration. |
| WASM as long-lived actor process | Medium | Works conceptually, but needs event/message integration and lifecycle hardening. |
| Reused permanent instance across many calls | Medium | Good for performance, but requires strict reset/isolation guarantees. |
| Per-call fresh instance (stateless) | High | Simplest correctness model for v1. |
| WASI HTTP incoming-handler bridge | Medium-High | Prototype exists in desktop draft; needs adaptation to current process contract. |
| Python in WASM | Low-Medium (v1) | `wasmexp` has test scaffold but integration is gated/skipped and socket support caveats exist. |
| Rust/C optimized code as function | High | Strong fit for core/component modules. |
| Non-component binary without WIT | Medium | Possible, but you lose type-safe friendly API; use explicit ABI mapping. |

### Practical language guidance

- Rust/C: best fit for predictable performance and stable ABI.
- Python: possible later through WASI/component packaging, but not a stable v1 target.
- "Low function without component": yes, via core WASM + WIT (recommended) or explicit low-level signatures.

## 7. Desktop Draft Reuse Assessment

What is reusable:

- package split (api/runtime/boot/component)
- transport abstraction
- hash verification for precompiled modules
- overall manager wiring idea

What must be rewritten/adapted:

- process contract mismatch (`Execute/old StepResult` vs current `Init/Step(events,out)/Close`)
- old function event constants (`function.Register/Delete` vs `FunctionRegister/Delete`)
- old pool constructor usage (now in subpackages `pool/inline`, `pool/static`, `pool/lazy`)
- debug prints in hot path
- scheduler bridge to current `process.Event` model

Conclusion: reuse architecture, not direct code.

## 8. Recommended Target Architecture

### 8.1 Workflow abstraction layers

- `service/workflow/core`:
  - workflow command interpreter
  - deterministic invariants
  - option normalization
- `service/workflow/adapter/temporal`
- `service/workflow/adapter/localdb`

### 8.2 WASM runtime layers

- `api/runtime/wasm`
  - kinds/config/transport/host class metadata
- `runtime/wasm/engine`
  - `process.Process` implementation on current contract
- `runtime/wasm/component`
  - registry-driven module manager + pool lifecycle
- `runtime/wasm/transport`
  - payload and wasi-http transport
- `runtime/wasm/host`
  - minimal deterministic-safe hosts first
- `boot/components/runtime/wasm`

### 8.3 Runtime policy

- v1 default: stateless per-call function execution
- v1 optional: pooled reusable instances for trusted modules
- long-lived WASM actor mode only after function mode is stable

## 9. Timeline (Assuming AI writes 100% of code)

### 9.1 POC

- 2-4 days:
  - workflow abstraction seam extraction
  - local db minimal run/queue loop
  - WASM function mode (`function.wat` + `function.wasm`) basic execution

### 9.2 Usable baseline

- 10-16 days:
  - replay correctness for core commands
  - timers/signals/child workflow subset
  - lease failover + crash recovery tests
  - WASI transport baseline

### 9.3 Hardening to semi-production

- additional 2-4 weeks:
  - determinism fuzz/replay differential testing
  - race/failure injection
  - queue contention/load tuning
  - operational tooling (visibility, stuck task repair, metrics)

Total realistic: ~3-6 weeks for "usable and safe enough", even with full AI coding.

## 10. Direct Answers to Your Core Questions

1. Can we build a local Temporal-like engine in one binary?  
Yes.

2. Do we need to start with Raft?  
No; start with DB-serialized workflow control plane + gossip for membership.

3. Can SQLite work?  
Yes for single-node/dev mode only.

4. Can we match Temporal-level semantics at this layer?  
Mostly yes for workflow determinism/timers/signals/children. Activity delivery remains at-least-once.

5. Should orchestration logic move below and be shared?  
Yes: one shared orchestration semantic layer, two adapters (Temporal + local DB).

6. Can WASM be function + process + reusable permanent worker?  
Yes, but phase it: function mode first, long-lived process mode second.

7. Can we run optimized Rust/C as low-level function without component model?  
Yes, via core WASM + WIT or explicit signature mapping.

8. Python?  
Possible later; not reliable v1 target yet.

## 11. Recommended Next Implementation Slice (Concrete)

1. Extract workflow semantic interfaces from Temporal code without behavior change.
2. Add `workflow/options` canonical normalization (`workflow.*`, `activity.*`, Temporal aliases).
3. Build local DB task lease loop + history append with deterministic replay stubs.
4. Integrate WASM function manager for `function.wat` and `function.wasm` on current pool APIs.
5. Add determinism + crash-recovery test harness before expanding feature surface.
