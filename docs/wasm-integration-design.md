# Wippy WASM Integration Design

Status: Draft v0.1  
Date: 2026-02-07  
Scope: integrate desktop WASM draft into main `wippy` runtime with current APIs

## 1. Goal

Bring a native WASM runtime into `wippy` as a first-class execution backend (same binary, cluster-capable through existing infra), without forking orchestration semantics.

This is not a full redesign. It is a convergence plan: keep what is good in the draft, adapt it to the current `wippy` contracts.

## 2. Current Runtime Contracts We Must Fit

### 2.1 Process contract is `Init/Step/Close`, not `Execute/StepResult`

Current interface is in `api/process/process.go`:

- `Init(ctx, method, input) error`
- `Step(events []Event, out *StepOutput) error`
- `Close()`

Pool/executor behavior in `system/scheduler/pool/pool.go` assumes this exact model and owns:

- event queue draining
- yield dispatch and completion routing
- lifecycle/status handling (`StepContinue`, `StepYield`, `StepDone`, etc.)

Any WASM process implementation must comply with this contract directly.

### 2.2 Function registration is ack-based

Current function registration flow (see `runtime/lua/component/function/pool.go`) uses:

- `function.FunctionRegister`
- `function.FunctionDelete`
- await `function.(accept|reject)` before considering registration successful

WASM manager must use the same handshake.

### 2.3 Pools are in subpackages

Current pool constructors are:

- `system/scheduler/pool/inline`
- `system/scheduler/pool/static`
- `system/scheduler/pool/lazy`
- `system/scheduler/pool/adaptive`

There is no `funcpool.NewStatic/NewInline` in root `system/scheduler/pool`.

### 2.4 Boot wiring pattern

Runtime components register listeners through boot handler registry:

- `boot/components/runtime/lua/engine.go`
- `boot/handler.go`

WASM should follow this pattern (component registration, start/stop lifecycle, dependency declarations).

## 3. Desktop Draft Assessment

Desktop draft location:

- `/mnt/c/Users/Wolfy-J/Desktop/__old/_-ready/__redo/__wasm`

### 3.1 Parts worth keeping

- Clean split between API/runtime/boot layers.
- Useful kinds:
  - `function.wat` for inline WAT
  - `function.wasm` for precompiled component binaries
- Host classification model (`deterministic`, `io`, `network`, etc.) mirrors Lua well.
- Transport abstraction (`payload`, `wasi-http`) is the right seam.
- Hash verification for component binaries is correct and should stay.

### 3.2 Critical drift that must be fixed

- Process API mismatch:
  - draft `runtime_wasm/engine/process.go` uses `Execute` + old step result types.
  - current runtime requires `Init/Step(events,out)/Close`.
- Function event constants mismatch:
  - draft managers use `function.Register/function.Delete`.
  - current constants are `function.FunctionRegister/function.FunctionDelete`.
- Pool constructor mismatch:
  - draft calls root-level pool constructors that do not exist.
- Dispatcher API mismatch:
  - draft references `dispatcher.AsyncScheduler` and some packages absent in current tree.
- Command package mismatch:
  - draft references `api/dispatcher/poll` style packages not present in current repo.
- Draft contains debug `fmt.Printf` in hot paths and hosts.

Conclusion: draft is a strong architecture prototype, but not mergeable without adaptation.

## 4. Target Architecture in Current Repo

## 4.1 Packages

Add:

- `api/runtime/wasm`
  - `api.go` (system constants, host classes, host metadata)
  - `config.go` (kinds/config validation)
  - `context.go` (transport registry, async frame state)
  - `errors.go`
- `runtime/wasm/engine`
  - process implementation for current `api/process.Process`
  - module factory
  - async bridge adapter
- `runtime/wasm/component/function`
  - registry entry manager for `function.wat` and `function.wasm`
  - pool lifecycle and caller registration with await ack
- `runtime/wasm/transport`
  - `payload` transport
  - `wasi-http` transport
- `runtime/wasm/host`
  - host registry + concrete hosts (clock first, then wasi/http/stream mapping)
- `boot/components/runtime/wasm`
  - boot component(s) and constants

Wire into:

- `cmd/wippy/cmd/components.go` by appending `wasm.All()`.

## 4.2 Manager model

Use one consolidated WASM function manager (not two separate managers) with two loaders:

- Source loader (`function.wat`)
- Binary loader (`function.wasm`, with `fs/path/hash` verification)

Both loaders feed one compiled module pipeline and one pool creation pipeline.

Reason: less duplicated lifecycle code, less drift, identical registration semantics.

## 4.3 Runtime/orchestration boundary

Keep orchestration above runtime.

- Workflow orchestration semantics should live in one abstraction layer (Temporal adapter + local DB adapter).
- WASM runtime is execution substrate (function/activity/process implementation detail), not orchestration authority.

This means we do not create two orchestration implementations. We create one orchestration API with backend adapters.

## 5. Semantics Contract for WASM Runtime

### 5.1 Determinism classes

Adopt Lua-style class tags for WASM hosts:

- deterministic
- nondeterministic
- io
- network
- time
- storage
- process

In deterministic workflow contexts, only deterministic-safe host classes are allowed.

### 5.2 Execution state machine

WASM process should be modeled exactly like other processes:

1. `Init`: instantiate/reset module instance, attach resource store, parse input transport.
2. `Step`:
   - consume `EventYieldComplete` events
   - resume async state (if pending yield)
   - emit new yields through `out.Yield(...)`
   - set `out.WaitForYields()` / `out.Done(...)` / `out.Idle()`
3. `Close`: release instance-scoped resources.

No custom scheduler loop outside existing pool executor.

### 5.3 Yield/result mapping

Host adapters are responsible for mapping dispatcher completion payloads back to WASM ABI-compatible return values.

Use explicit per-host result codecs, not a global type switch, to avoid hidden coupling.

### 5.4 Resource lifecycle

Use `api/runtime/resource.Store` in frame context, same as Lua.

- Allocate per execution in `Init`.
- Release via frame lifecycle (closer semantics), not ad hoc globals.

### 5.5 Transport modes

- `payload` (default):
  - payloads decoded to method arguments (v1: keep scope narrow and explicit)
- `wasi-http`:
  - reads `api/service/http.RequestContext`
  - creates request/response handles in resource table
  - function signature corresponds to WASI incoming-handler conventions

## 6. Cluster and Coordination Impact

For WASM function runtime itself, cluster coordination can stay on existing pieces:

- registry distribution
- host routing via relay/topology
- per-node pools

No new Raft/consensus requirement is needed for this layer.

Consensus work (if needed) belongs to workflow orchestration state management, not to WASM function execution.

## 7. Delivery Plan

## 7.1 Phase plan

1. Contract-aligned scaffolding (0.5-1 day)
- create `api/runtime/wasm`
- add boot component skeleton
- add manager skeleton with current function event constants + await flow

2. POC execution path (1-2 days)
- inline `function.wat`
- static/inline pool support
- sync execution only
- happy-path tests

3. Async + host yields (2-4 days)
- async bridge in `Step(events,out)`
- clock host integration
- yield completion codecs

4. Precompiled component + FS/hash (1-2 days)
- `function.wasm` loader
- hash verification
- regression tests

5. Transport + hardening (3-5 days)
- `wasi-http` transport
- security gates/class filtering
- failure-path tests and race/leak checks

Total AI-assisted estimate to production-usable baseline: about 8-14 days.  
Fast POC can land in 1-2 days, but correctness hardening dominates after that.

## 7.2 What to defer from v1

- Full WASI surface
- Generic poll subsystem redesign
- Cross-language workflow determinism guarantees for all host classes
- aggressive performance optimization before correctness baseline

## 8. Recommended First Build Slice

Implement in this order:

1. `api/runtime/wasm` + kinds/config validation.
2. Single WASM function manager wired to current function registry/await semantics.
3. Minimal process engine that matches current `Init/Step/Close`.
4. Inline WAT `function.wat` smoke tests.

After this slice, add asyncify/hosts/transport incrementally.

