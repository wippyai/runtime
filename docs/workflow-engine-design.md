# Wippy Local Workflow Engine Design

Status: Draft v0.1  
Date: 2026-02-07  
Scope: workflow abstraction + custom DB-backed engine (single binary, cluster-capable)

## 1. Problem Statement

Wippy already integrates deeply with Temporal and exposes workflow semantics through Lua and process APIs.  
Goal: add an internal, local-first workflow engine that can run in the same binary, with Postgres primary and SQLite local mode, while preserving most current workflow behavior.

This engine is not meant to be a full Temporal replacement initially. It must be usable, deterministic, and operationally simple.

## 2. Current System Facts (from code)

### 2.1 Host Abstraction Already Exists

- Process execution is host-pluggable through `process.Host`:
  - `api/process/process.go`
  - `Run(ctx, start)` + `Terminate(ctx, pid)` + `relay.Receiver`
- Process manager delegates start/terminate to host:
  - `system/process/manager.go`
- Temporal worker is registered as just another host:
  - `service/temporal/worker/manager.go`

### 2.2 Deterministic Workflow Command Model Already Exists

- Workflow command IDs and payloads are backend-agnostic:
  - `api/runtime/workflow/command.go`
  - `SideEffect`, `Exec`, `Version`, `UpsertAttrs`
- Lua workflow module yields these commands when deterministic mode is set:
  - `runtime/lua/modules/workflow/module.go`
  - `runtime/lua/modules/workflow/yield.go`
- Deterministic context flag exists independently of Temporal:
  - `api/runtime/workflow/context.go`

### 2.3 Temporal-Specific Semantics to Preserve

- Workflow step loop translates process/workflow commands into backend operations:
  - `service/temporal/workflow/execution.go`
- Workflow `process.send` behavior:
  - self-send short-circuit
  - update response routing (`host=update`)
  - external workflow signal path
  - local process route via relay side effect
  - `service/temporal/workflow/handler_process.go`
- Workflow `process.spawn` maps to child workflow execution and queues EXIT event on completion:
  - `service/temporal/workflow/handler_process.go`
- `process.monitor`, `process.link`, `process.unlink` unsupported in workflow context:
  - `service/temporal/workflow/handler_process.go`
- Timer semantics exist through workflow timer manager:
  - `service/temporal/workflow/handler_timer.go`
- Spawn options are host-interpreted and opaque at process module level:
  - `runtime/lua/modules/process/spec.md`

### 2.4 Existing Cluster/Relay Infrastructure

- Cluster membership and internode transport already exist:
  - `boot/components/system/cluster.go`
- Relay node and host registration manager already exist:
  - `system/relay/manager.go`
- Distributed topology/watcher/link state already exists:
  - `system/topology/topology.go`

## 3. Goals and Non-Goals

### 3.1 Goals (v1)

- Single-binary workflow engine mode.
- Postgres-backed durable workflow execution.
- Cluster-capable worker polling/claiming.
- Deterministic replay from history.
- At-least-once activities.
- Durable timers.
- Signal, cancel, terminate, child workflow, continue-as-new.
- Backward-compatible runtime semantics for existing Lua workflows.

### 3.2 Non-Goals (v1)

- Full Temporal feature parity.
- Strong multi-region guarantees.
- Full advanced search/visibility parity.
- Exactly-once activity execution (use idempotency + at-least-once).

## 4. Proposed Abstraction Split

### 4.1 Keep Stable

- `process.Host` contract.
- `api/runtime/workflow` command types.
- Lua workflow API (`workflow.exec`, `workflow.version`, `workflow.attrs`, `workflow.info`).
- Process manager/relay/topology boundaries.

### 4.2 Extract From Temporal

Create backend-neutral workflow runtime interfaces:

```go
// api/service/workflow/runtime.go
type Runtime interface {
    Info() Info

    // deterministic workflow ops
    SideEffect(key string, fn func() (payload.Payload, error)) (payload.Payload, error)
    GetVersion(changeID string, min, max int) (int, error)
    UpsertAttrs(search map[string]any, memo map[string]any) error

    // orchestration ops
    StartChild(req StartChildRequest) (pid.PID, error)
    AwaitChild(pid pid.PID) (*runtime.Result, error)
    Signal(target pid.PID, topic string, payloads payload.Payloads) error
    Cancel(target pid.PID) error
    Terminate(target pid.PID, reason string) error
    ContinueAsNew(req ContinueAsNewRequest) error

    // time
    Sleep(d time.Duration) error
    StartTimer(req TimerStartRequest) (string, error)
    StopTimer(id string) error
    ResetTimer(id string, d time.Duration) error
    StartTicker(req TickerStartRequest) (string, error)
    StopTicker(id string) error
}
```

Adapters:

- `service/workflow/adapter/temporal`: wraps existing Temporal runtime behavior.
- `service/workflow/adapter/localdb`: new custom engine runtime.

### 4.3 Option Namespace Normalization

Current Temporal-specific option parser is centralized:

- `service/temporal/options/options.go`

Add core parser:

- `service/workflow/options/options.go`

Rules:

- Canonical namespace: `workflow.*`, `activity.*`.
- Compatibility aliases:
  - `temporal.workflow.*` -> `workflow.*`
  - `temporal.activity.*` -> `activity.*`
- Backends consume normalized options.

## 5. Semantics Contract (v1)

This section defines behavior that both Temporal adapter and local engine should follow.

### 5.1 Workflow Identity and Start Conflict

- If explicit workflow ID/name exists:
  - default conflict policy: `use_existing`.
- If generated ID:
  - default conflict policy: `fail`.
- Behavior mirrors current Temporal host defaults in `service/temporal/worker/host.go`.

### 5.2 process.send in Workflow Context

- If target is self: complete immediately.
- If target is update pseudo-host: route to update response manager.
- If target is external workflow host: send as workflow signal.
- Else: route as relay package side effect.

### 5.3 process.spawn in Workflow Context

- Starts child workflow.
- Returns child PID on start ack.
- Emits EXIT-style event when child completes.
- `Start.HostID` may override task queue/host routing.

### 5.4 Unsupported In-Workflow Commands

- `process.monitor`, `process.link`, `process.unlink`, `process.unmonitor` return deterministic invalid errors in workflow context (same as current behavior).

### 5.5 Deterministic Commands

- `SideEffect`: first run executes + stores result; replay returns stored result.
- `Version`: value is stable per `(run_id, change_id)`.
- `UpsertAttrs`: updates queryable/search + memo metadata durably.

### 5.6 Timers

- Timers must survive process/node restart.
- Timer fire produces deterministic event in history.
- Timer callbacks resume process via yield completion event.

## 6. Local Engine Architecture

### 6.1 Components

- `workflowpg.Manager` (entry listener + host registration).
- `workflowpg.Host` (`process.Host`) for `Run/Terminate/Send`.
- `workflowpg.Worker` loop for polling and executing tasks.
- `workflowpg.Store` for SQL persistence.
- `workflowpg.RuntimeEnv` implementing workflow runtime interface.
- `workflowpg.Replayer` to reconstruct process state from history.

### 6.2 Data Model (Postgres primary)

Suggested tables:

- `wf_execution`
  - workflow identity, type, status, attempt, parent linkage
  - sticky owner hints, timestamps
  - metadata (`memo`, `search_attrs`)
- `wf_history_event`
  - append-only per `run_id`, monotonic `event_id`
  - event type + payload
- `wf_task`
  - pending work items (`workflow`, `activity`, `timer`)
  - `available_at`, `lease_owner`, `lease_until`, `attempt`
- `wf_timer`
  - logical timers/tickers mapped to scheduled task creation
- `wf_side_effect`
  - deterministic side-effect results keyed by event position
- `wf_version`
  - version decisions keyed by `change_id`

Optional later:

- `wf_snapshot` (checkpointed VM/process state)
- `wf_visibility` (denormalized query index)

### 6.3 Claiming and Leases

Postgres claim pattern:

```sql
WITH cte AS (
  SELECT id
  FROM wf_task
  WHERE available_at <= now()
    AND (lease_until IS NULL OR lease_until < now())
  ORDER BY available_at, id
  FOR UPDATE SKIP LOCKED
  LIMIT $1
)
UPDATE wf_task t
SET lease_owner = $2,
    lease_until = now() + $3::interval,
    attempt = attempt + 1
FROM cte
WHERE t.id = cte.id
RETURNING t.*;
```

SQLite mode:

- single-node recommended
- claim by optimistic `UPDATE ... WHERE lease_until < now OR lease_owner IS NULL`
- WAL mode required
- no multi-node guarantees

## 7. Execution and Replay Flow

### 7.1 Task Execution Loop

1. Claim workflow task.
2. Load execution state + history tail.
3. Rebuild process by replaying deterministic events.
4. Deliver queued signals/updates/cancel events.
5. Execute step loop until:
   - yield (schedule external work)
   - idle (await new events)
   - done (complete run)
   - continue-as-new (spawn next run atomically)
6. Persist new history + next tasks in one transaction.
7. Ack task.

### 7.2 Replay Invariants

- Event IDs strictly monotonic per run.
- Command completion bound to deterministic event index.
- Side effect and version lookups deterministic by stable key.
- No external side effects during replay.

## 8. Reliability Model (v1)

- Workflow task execution: at-least-once.
- Activity execution: at-least-once.
- User-facing exactly-once requires idempotency keys in activities and external sinks.
- Crash recovery via replay from durable history.
- Soft stickiness only for performance; correctness must not depend on it.

## 9. API/Registry Design

### 9.1 New Entry Kinds

- `workflow.engine` (backend config, persistence, leasing)
- `workflow.worker` (poller/executor config, concurrency)

or align with existing style:

- `workflow.client` + `workflow.worker`

Either model must still register `process.Host` into relay like Temporal does today.

### 9.2 Config Sketch

```yaml
workflow:
  engine: postgres # postgres | sqlite
  namespace: default
  lease_ttl: 15s
  poll_interval: 200ms
  max_batch: 128
  sticky:
    enabled: true
    ttl: 30s
  sqlite:
    path: .wippy/workflow.db
    wal: true
  postgres:
    dsn: ${WORKFLOW_DB_DSN}
```

## 10. Compatibility and Migration

### 10.1 Dual-Run Strategy

- Keep Temporal integration active.
- Add local engine host in parallel.
- Route selected workflows to local host by host ID.
- Compare outputs/latency/failure handling in shadow mode before canary.

### 10.2 Option Compatibility

- Continue accepting `temporal.workflow.*`.
- Normalize to backend-neutral keys.
- Emit warnings for Temporal-only unsupported fields in local engine.

### 10.3 Error Surface

- Preserve current error kind/retryable metadata behavior where possible.
- Map engine errors to current runtime error taxonomy.

## 11. Implementation Composition Decision

Decision: do not duplicate orchestration logic.

- Keep one workflow orchestration abstraction in core runtime.
- Keep two backend adapters:
  - Temporal adapter (existing)
  - local DB adapter (new)
- Move Temporal-specific code behind adapter boundaries, not into core semantics.

Practical rule:

- orchestration semantics (signals, timers, child workflow, replay contract) live in shared layer
- backend execution/persistence details live in adapters

This avoids maintaining two independent workflow semantics implementations.

## 12. Cluster Coordination Scope

For workflow abstraction itself, DB + leases is sufficient for v1.

- Postgres: `FOR UPDATE SKIP LOCKED` + lease renewal
- SQLite: single-node mode only

Consensus/Raft is not required to start this layer.  
If introduced later, it should target specific control-plane responsibilities (e.g. global naming/placement), not core workflow replay semantics.

## 11. Test Strategy and Gates

### 11.1 Required Test Layers

- Unit: option parsing, lease claiming, idempotency keys, replay determinism.
- Integration: workflow lifecycle, signals, timers, retries, cancel/terminate.
- Cluster simulation: worker crash during lease, split ownership recovery.
- Soak/stress: many short workflows, timer-heavy workflows.

### 11.2 Acceptance Gates

- Determinism gate:
  - run same history replay N times -> identical command/output stream.
- Recovery gate:
  - kill worker mid-task -> run completes correctly after restart.
- Compatibility gate:
  - existing sample Temporal workflows pass on local engine subset.

## 12. Delivery Plan (AI-Assisted)

Phase 0 (1 day):

- Freeze semantics + invariants doc.
- Define option normalization table.

Phase 1 (2-3 days):

- Extract workflow runtime interfaces.
- Move Temporal-specific logic behind adapter.

Phase 2 (4-6 days):

- Postgres schema + store + task leasing.
- Minimal worker loop + run lifecycle.

Phase 3 (4-6 days):

- Replay engine for deterministic commands.
- Side effects/version persistence.

Phase 4 (3-4 days):

- Timers + signals + child workflows.

Phase 5 (4-5 days):

- Hardening: crash recovery, idempotency edges, load tests.

Phase 6 (2-3 days):

- SQLite single-node adapter.

Projected usable v1: 3-4 weeks with focused AI coding + human review.  
Projected semi-prod confidence: 6-8 weeks with hardening cycles.

## 13. Open Decisions

- Entry kind naming for local engine (`workflow.*` vs reuse `temporal.*` patterns).
- Minimum supported subset for v1 (child workflows, updates, advanced attrs).
- Snapshot strategy:
  - defer for v1
  - or include periodic snapshots from day 1.
- Visibility/query schema scope for v1.

## 14. Immediate Next Step

Implement Phase 1 extraction:

- add `api/service/workflow` runtime interfaces
- add `service/workflow/options` canonical parser
- adapt current Temporal workflow execution to use new interfaces without behavior change

This creates a stable seam for plugging in `service/workflowpg` next.
