# No-Crash Runtime — Design Spec

**Date:** 2026-04-28
**Author:** chaos-engineering session
**Status:** approved

## Goal

The `wippy-runtime` StatefulSet must never be OOMKilled while the K3D chaos
profile is active (50% NetworkChaos partition + 200ms±100ms delay + periodic
PodChaos container-kill). Cannot raise the 512Mi pod memory limit. Cannot
reduce the chaos load — the current chaos profile is the spec, the runtime
must tolerate it. Fix has to land in `runtime/` (and supporting config in
`monkey/`).

## Symptom

Three pods running for ~12h:

```
wippy-runtime-0  16 restarts  exitCode=137  reason=OOMKilled  mem=498Mi/512Mi
wippy-runtime-1  14 restarts  exitCode=137  reason=OOMKilled  mem=500Mi/512Mi
wippy-runtime-2   7 restarts  exitCode=137  reason=OOMKilled  mem= 60Mi/512Mi  (just restarted)
```

Memory grows from ~60Mi at boot to ~500Mi over a few minutes, then OOMKilled
by the kernel. Loop repeats. Raft logs show `term=10298+` — ten thousand
elections — driven by the partition.

## Root causes (from diagnostic)

Investigation identified five compounding issues, prioritized by impact:

1. **Unbounded internode send queue** (`cluster/internode/state_manager.go`,
   `connection.go`) — `*list.List` with no upper bound. Under partition,
   `flushBatch -> writer.Write` blocks while every PG broadcast and raft RPC
   keeps appending. Worse: `RequeueMessages` on reconnect duplicates the
   stuck queue back into `messageQueue`.
2. **Unbounded retry queue** (`system/pg/retry.go`) — `entries []*retryEntry`
   has no cap; one entry per failed broadcast × peer. Each retains
   `payloads`/`pids` slices. `processRetries` rebuilds `ready`/`remaining`
   slices each tick (O(N), allocating).
3. **OTel `BatchSpanProcessor` accumulates spans** (`service/otel/provider.go`)
   — defaults are `MaxQueueSize=2048`, no explicit bound. With
   `instrumentedTransport.AppendEntries` opening a span per call during
   election storms (term=10298), the batcher fills when the collector is
   unreachable.
4. **High-cardinality metric labels** — `pg_retry_total{attempt}` uses
   `strconv.Itoa(int)`; series count grows unboundedly per distinct attempt.
5. **Goroutine leak in `LeadershipTransfer`** (`raft.go:523-530`) —
   `go func() { done <- f.Error() }()` blocks until `f.Error()` returns,
   which is "never" under partition. One leak per stuck transfer.

## Principle

**Bounded everything, silent nothing.** Every structure that grows under
chaos has a fixed cap; every drop emits an observable metric. RSS
stabilizes; OOM disappears; Kubernetes restarts the pod only on real
failures, not OOM loops. Mirrors etcd/raft, memberlist/SWIM, and OTP `pg`
literally.

## Architecture

### Per-class transport queues (P0)

`cluster/internode` today uses an unbounded `*list.List` per
`NodeConnection`. Replace with one **bounded ring queue per QoS class**:

```go
type Class uint8
const (
    ClassRaftControl  Class = iota  // AE, RV, IS  → drop-oldest
    ClassGossip                     // SWIM probes  → drop-newest
    ClassPGBroadcast                // app msgs     → drop-newest + caller error
)
```

Per-class capacities (chosen from canonical defaults):

| Class            | Cap  | Source                    |
|------------------|------|---------------------------|
| `RaftControl`    | 4096 | etcd default              |
| `Gossip`         | 1024 | 2× memberlist default 512 |
| `PGBroadcast`    | 2048 | sized for fan-out         |

Drop policy mapping to canonical references:
- **Raft control** (etcd, hashicorp/raft): drop-oldest. AE/RV/IS are
  idempotent; the leader retransmits via heartbeat. Newer state always
  wins.
- **Gossip** (SWIM, memberlist): drop-newest. Gossip is lossy by design;
  the next round corrects it.
- **PG broadcast** (Erlang OTP `pg`): drop-newest with `ErrQueueFull`
  returned to the caller. Fire-and-forget, but observable.
- **Raft proposals (FSM commands)** are unchanged — `hraft` already
  applies backpressure via `Future.Error()`. Never silently dropped.

Dispatcher consumes in priority `RaftControl > Gossip > PGBroadcast` so
control traffic is never starved by application broadcasts.

`RequeueMessages` on reconnect respects per-class caps (no duplication
on reconnect — the current bug).

Metrics emitted on every drop:

```
internode_dropped_total{class="raft|gossip|pg", reason="queue_full"}
internode_queue_depth{class, peer}  # gauge sampled by dispatcher
```

### Bounded retry queue (P1)

`system/pg/retry.go`:
- Replace `entries []*retryEntry` with `container/heap` keyed by `nextTry`.
  `processRetries` becomes O(log N), no slice rebuild per tick.
- Cap at 2048 entries. On overflow, drop oldest with
  `pg_retry_dropped_total{reason="cap"}`.
- Track `pg_retry_queue_size` gauge.

### OTel batcher constraints (P2)

`service/otel/provider.go`:

```go
sdktrace.NewTracerProvider(
    sdktrace.WithBatcher(exporter,
        sdktrace.WithMaxQueueSize(512),
        sdktrace.WithMaxExportBatchSize(128),
        sdktrace.WithBatchTimeout(2*time.Second),
        sdktrace.WithBlocking(false),
    ),
    sdktrace.WithSpanLimits(sdktrace.SpanLimits{
        AttributeCountLimit: 16,
        EventCountLimit:     16,
        LinkCountLimit:      16,
    }),
    ...
)
```

Spans drop silently when the collector is unreachable — canonical OTel
behavior in memory-bound environments. Drop count visible via the
collector's `otelcol_processor_dropped_spans` metric.

### Hot-path cleanup (P4, P5)

`system/raft/raft.go`:
- Remove the per-call `startSpan` in `instrumentedTransport.AppendEntries`
  (lines ~584 and ~702). Under election storm, hundreds of spans/sec from
  this one path overwhelm the batcher. Sampler handles trace coverage at
  the source. AppendEntries keeps its counter+histogram (cheap,
  pre-allocated).
- `LeadershipTransfer` (lines 523-530): the helper goroutine becomes
  context-aware:

  ```go
  go func() {
      select {
      case done <- f.Error():
      case <-ctx.Done():
      }
  }()
  ```

  No leak when the transfer hangs under partition.

### Cardinality bounds (P3)

`system/pg/telemetry.go`:
- `pg_retry_total{pg, op, attempt}` — bucket `attempt` to
  `"1" | "2-3" | "4+"`. Prometheus rule: never label with unbounded values.

### Process-level memory ceiling

`monkey/Dockerfile.runtime`:

```dockerfile
ENV GOMEMLIMIT=400MiB
```

Go 1.19+ honors `GOMEMLIMIT` and triggers GC more aggressively as the
process approaches it, providing headroom before the kernel cap (512Mi).

## Observability — dashboards

### New dashboard: `monkey/manifests/observability/dashboards/21-bounded-runtime.json`

Title: **"Bounded Runtime — No-Crash Guarantees"**. Five rows, each
proving one invariant visually.

**Row A — RSS plateau (proves zero-OOM):**
- Pod memory vs limit (timeseries):
  `container_memory_working_set_bytes{namespace="wippy-runtime"}` per pod,
  with horizontal threshold at 512Mi (limit) and 400Mi (`GOMEMLIMIT`).
- GC pressure (timeseries): `rate(go_gc_duration_seconds_count[1m])` and
  `go_memstats_heap_inuse_bytes`.
- Restarts (stat):
  `sum(increase(kube_pod_container_status_restarts_total{namespace="wippy-runtime"}[15m]))`.
  Color: green=0, yellow>0, red>3.

**Row B — Drops by class (proves "drops are visible, not silent"):**
- Internode drops by class (stacked area):
  `sum by (class) (rate(internode_dropped_total[1m]))`.
- PG broadcast drops (timeseries):
  `sum(rate(pg_broadcast_dropped_total{reason="queue_full"}[1m]))` per pod.
- PG retry drops (timeseries):
  `sum(rate(pg_retry_dropped_total[1m]))`.
- OTel spans dropped (timeseries):
  `sum(rate(otelcol_processor_dropped_spans[1m]))` (collector) +
  `sum(rate(otel_sdk_spans_dropped_total[1m]))` (SDK).

**Row C — Bounded queue depth (proves effective ceiling):**
- Internode queue depth per class:
  `internode_queue_depth{class}` with thresholds at 4096/1024/2048.
- PG retry heap size: `pg_retry_queue_size`, threshold 2048.
- OTel batcher fill: `otel_sdk_span_queue_size / 512`.

**Row D — Health under chaos (correlation):**
- Chaos windows annotation: `chaos_mesh_experiments{phase="Running"}` as
  vertical bands.
- Raft leader churn vs raft drops (multi-axis):
  `rate(raft_leader_changes_total[1m])` × `rate(internode_dropped_total{class="raft"}[1m])`.
- Gossip suspect vs gossip drops (multi-axis):
  `gossip_members{state="suspect"}` × `rate(internode_dropped_total{class="gossip"}[1m])`.

**Row E — Recovery post-chaos (proves auto-heal):**
- Within 5min of `chaos_mesh_experiments{phase="Finished"}`:
  - `count(raft_state==2)` returns to 1
  - `gossip_members{state="alive"}` returns to 3
  - `internode_queue_depth` drains to 0

### Updates to existing `00-crash-and-failure-overview.json`

Add a top "Bounded guarantees" row:
- Stat: `restart_count_15m == 0` (fixed green).
- Stat: `max_pod_rss_15m < 400Mi` (fixed green).
- Stat: `sum(rate(internode_dropped_total[1m])) > 0` during chaos = "Working
  as designed" (informational blue, not red — drops under chaos are
  correct).

### New metrics surface

| Metric                                | Type    | Source           |
|---------------------------------------|---------|------------------|
| `internode_dropped_total{class,reason}` | counter | internode dispatcher |
| `internode_queue_depth{class,peer}`     | gauge   | internode dispatcher |
| `pg_broadcast_dropped_total{reason}`    | counter | pg/broadcast.go      |
| `pg_retry_dropped_total{reason}`        | counter | pg/retry.go          |
| `pg_retry_queue_size`                   | gauge   | pg/retry.go          |
| `otel_sdk_spans_dropped_total`          | counter | OTel SDK self-metrics |

## Guarantees

| Invariant                             | Mechanism                                              |
|---------------------------------------|--------------------------------------------------------|
| RSS ≤ 500Mi under any chaos profile   | All structures bounded; cap sum < 200Mi; rest is Go heap with `GOMEMLIMIT` |
| Zero OOMKills                         | Hard caps on queues + aggressive GC near `GOMEMLIMIT`  |
| Raft remains correct                  | Drops on RaftControl ≡ packet loss; hraft retries via heartbeat |
| PG semantics match Erlang `pg`        | Best-effort fire-and-forget with observable drop metrics |
| No public-API regression              | Internal-only changes; existing tests still pass        |

## Testing strategy

**Unit tests:**
- `cluster/internode/state_manager_test.go`: queue-full case for each
  class (drop-oldest preserves order, drop-newest returns error).
- `cluster/internode/connection_test.go`: `RequeueMessages` respects cap
  on reconnect; no duplication.
- `system/pg/retry_test.go`: heap ordering under random insertion;
  cap-overflow drops oldest; no slice rebuild per tick.
- `service/otel/provider_test.go`: batcher uses configured bounds.
- `system/raft/raft_test.go`: `LeadershipTransfer` goroutine exits on
  context cancel.

**Integration (race detector):**
- `system/raft/integration_test.go`: 3-node cluster, 30s simulated
  partition, assert `runtime.MemStats.Alloc` plateaus.
- `system/pg/manager_test.go`: 100 PG members, 50% drop rate, 60s
  broadcast flood, assert no goroutine leak (`goleak.VerifyNone`).

**pg-harness:**
- New scenario `partition_storm`: 3 nodes, 50% loss + 1k broadcasts/sec
  for 60s. Assert RSS bounded, drop counters > 0, zero panics.

**Live chaos cluster:**
- Run for 1h after deploy.
- `container_memory_working_set_bytes` plateau below 450Mi (visible on
  Row A).
- `kube_pod_container_status_restarts_total` flat at current value
  throughout the hour.
- All Row B drop counters > 0 (proves the bounded paths are exercising).

## Out of scope

- No changes to `monkey/manifests/runtime/configmap.yaml` (Lua workload
  unchanged — fire-and-forget broadcasts are exactly the test we want).
- No changes to chaos profile (`monkey/manifests/chaos/core-chaos.yaml`).
- No changes to pod resource limits (`512Mi`/`800m` stays).
- No `pg-harness/` changes other than the new `partition_storm` scenario.
- No memory watchdog goroutine (Approach 2). Not needed if bounds are
  correct; would only mask new bugs.
- No subsystem supervisor restarts (Approach 3). Kubernetes is the
  supervisor; OTP semantics are satisfied at the pod boundary.

## Files touched

**`runtime/`:**
- `cluster/internode/state_manager.go` — per-class queues, drop policy
- `cluster/internode/connection.go` — bounded `RequeueMessages`
- `cluster/internode/manager.go` — `Class` enum, `QueueMessage(msg, class)`
- `cluster/internode/state_manager_test.go` — new cases
- `cluster/internode/connection_test.go` — new cases
- `system/pg/retry.go` — heap-based, capped
- `system/pg/retry_test.go` — new cases
- `system/pg/broadcast.go` — class-aware send
- `system/pg/telemetry.go` — `pg_broadcast_dropped_total`,
  `pg_retry_dropped_total`, `pg_retry_queue_size`, bucket `attempt`
- `system/raft/raft.go` — drop per-AE span, fix `LeadershipTransfer`
  goroutine, class-aware send
- `service/otel/provider.go` — bounded batcher, span limits
- `service/otel/provider_test.go` — verify bounds

**`monkey/`:**
- `Dockerfile.runtime` — `ENV GOMEMLIMIT=400MiB`
- `manifests/observability/dashboards/21-bounded-runtime.json` — new
- `manifests/observability/dashboards/00-crash-and-failure-overview.json`
  — add "Bounded guarantees" row

**`pg-harness/`:**
- `cmd/runner/partition_storm.go` — new scenario function
  `RunPartitionStorm(SyntheticConfig)` invoked from `main.go` via a
  `--scenario=partition-storm` flag
