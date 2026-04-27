# PG / Raft / Gossip OpenTelemetry Observability — Design

Date: 2026-04-27
Branch: `feature/pg-process-groups` (runtime), new branch in `monkey/` (untracked)
Status: design approved, awaiting plan

## Problem

The current `feature/pg-process-groups` branch lands substantial logic for process
groups (PG), raft consensus, and gossip-based membership, but none of these
subsystems emit OpenTelemetry signals. The `monkey/` chaos project ships
"observability" dashboards that mostly visualize Kubernetes container metrics
(`kube_pod_*`, `container_*`) and a few invented `pg_harness_*` series produced
by an external health-checker. There is no panel that answers basic operator
questions during chaos:

- Is there a stable raft leader right now? What is the term?
- Are followers keeping up with commits? How big is the log lag per peer?
- How many voters vs non-voters under the new voter cap rule?
- Are PG broadcasts succeeding? How deep is the action queue? Is the circuit
  breaker open? Are retries spinning into giveup?
- Is gossip converging? Are members flapping between alive and suspect?
- During a chaos experiment, did we observe split-brain (two leaders)?

The runtime ships an OTel infrastructure (`runtime/api/metrics`,
`runtime/service/otel`, `runtime/boot/components/otel`) and an OTLP collector
that already has `traces`, `metrics` and `logs` pipelines wired to Jaeger and
Prometheus. The plumbing exists; the emission points do not.

## Scope

This spec covers two coupled deliverables:

1. **Runtime instrumentation.** Add OTel metrics + traces inside
   `runtime/system/pg`, `runtime/system/raft`, and
   `runtime/cluster/membership`. Per-package telemetry files following the
   existing layout convention.
2. **20 Grafana dashboards.** Static JSONs under
   `monkey/manifests/observability/dashboards/`, mounted via a single
   ConfigMap. The 20 dashboards consume the new runtime metrics plus existing
   Kubernetes / otelcol self-metrics. Old generators and overlapping ConfigMaps
   are removed to keep a single source of truth.

### Explicitly out of scope

- Prometheus Alertmanager rules / alerting.
- SLO definitions.
- Changes to existing logs.
- OTel collector pipeline changes (already wired).

## Architecture

```
runtime (Go)
 ├── system/pg          ─┐
 ├── system/raft         │── metrics + spans  ──► api/metrics.Collector
 └── cluster/membership ─┘                        + api/service/otel.tracer
                                                                │
                                                                ▼
                                                  service/otel.MetricsExporter
                                                  service/otel.TracerProvider
                                                                │
                                                  OTLP gRPC ────┘
                                                                │
                                                                ▼
                                            otel-collector (existing)
                                              ├── metrics → Prometheus
                                              └── traces  → Jaeger
                                                                │
                                                                ▼
                                                         Grafana (20 dashboards)
```

## Conventions

### Metric names

Short prefixes per subsystem: `pg_*`, `raft_*`, `gossip_*`. Conflicts with the
legacy `pg_harness_*` series are eliminated by the cleanup step (those
ConfigMaps are removed).

### Span names

Dotted, lowercase: `pg.join`, `pg.leave`, `pg.broadcast`, `pg.dispatch`,
`raft.append_entries`, `raft.election`, `raft.snapshot`, `raft.apply`,
`gossip.probe`, `gossip.sync`, `gossip.broadcast`. Standard span attributes:
`pg.name`, `node.id`, `peer`, `term`, `result`, `error`.

### Label cardinality guardrails

- `pg` (group name): caller-bounded, same as today.
- `peer` (raft): bounded by voter cap (≤ 7 typical).
- `node` (gossip): O(N) members.
- `op`, `result`, `kind`, `direction`, `state`, `reason`, `outcome`: closed
  enums.
- IDs that vary per request (message IDs, RPC IDs) live on **spans only**, never
  as metric labels.

### Naming rules

- No emojis in metric names, span names, dashboard titles, panel titles, or
  documentation. Existing dashboards violated this; the new ones do not.
- Dashboard JSON files: zero-padded prefix to fix display order
  (`01-pg-operations-overview.json` … `20-pod-resources.json`).

## Runtime instrumentation

### Per-package telemetry files

| File | Responsibility |
|---|---|
| `runtime/system/pg/telemetry.go` | PG meter + tracer + counters/gauges/histograms; `recordJoin`, `recordLeave`, `recordBroadcast`, `recordQueue`, `recordCircuitBreaker`, `recordRetry`, `recordFence`, `recordGlobalReg` |
| `runtime/system/raft/telemetry.go` | Raft meter + tracer; `recordState`, `recordTerm`, `recordLeaderChange`, `recordElection`, `recordCommit`, `recordAppendEntries`, `recordSnapshot`, `recordVoterLadder` |
| `runtime/cluster/membership/telemetry.go` | Gossip meter + tracer; `recordMemberStateChange`, `recordMessage`, `recordProbe`, `recordSuspicionResolution`, `recordConvergence` |

Constructor signature:

```go
func newTelemetry(
    coll metrics.Collector,    // may be nil (test default = no-op)
    mp   otelmetric.MeterProvider,
    tp   trace.TracerProvider,
) *telemetry
```

The constructor never panics on nil providers; falls back to
`otel.GetMeterProvider()` and `otel.GetTracerProvider()` if both args are nil.
Used by tests to opt out without mocking.

### Metrics — PG (`pg_*`)

| Name | Type | Labels | Captures |
|---|---|---|---|
| `pg_join_total` | counter | `pg`, `result` | Join attempts |
| `pg_leave_total` | counter | `pg`, `result` | Leave attempts |
| `pg_broadcast_total` | counter | `pg`, `result` | Broadcasts emitted |
| `pg_broadcast_recipients` | histogram | `pg` | Fan-out size |
| `pg_op_duration_seconds` | histogram | `pg`, `op` | Operation latency |
| `pg_queue_depth` | gauge | `pg` | Action queue depth |
| `pg_queue_dropped_total` | counter | `pg`, `reason` | Queue drops |
| `pg_circuit_breaker_state` | gauge | `pg` | 0=closed, 1=half-open, 2=open |
| `pg_circuit_breaker_trips_total` | counter | `pg` | Breaker trips |
| `pg_retry_total` | counter | `pg`, `op`, `attempt` | Retry attempts |
| `pg_retry_giveup_total` | counter | `pg`, `op` | Retries exhausted |
| `pg_dispatcher_inflight` | gauge | `pg` | In-flight messages |
| `pg_fence_token` | gauge | `pg`, `node` | Current fence token |
| `pg_fence_rejection_total` | counter | `pg`, `reason` | Stale-token rejections |
| `pg_globalreg_size` | gauge | — | Global registry entries |
| `pg_globalreg_dedupe_total` | counter | — | Dedup events |

### Metrics — Raft (`raft_*`)

| Name | Type | Labels | Captures |
|---|---|---|---|
| `raft_state` | gauge | `node` | 0=follower, 1=candidate, 2=leader |
| `raft_term` | gauge | — | Current term |
| `raft_leader_changes_total` | counter | — | Leader changes |
| `raft_election_duration_seconds` | histogram | — | Time to elect |
| `raft_commit_index` | gauge | — | Commit index |
| `raft_last_log_index` | gauge | `node` | Last log index per node |
| `raft_log_lag` | gauge | `node` | `commit_index − last_log_index` |
| `raft_append_entries_duration_seconds` | histogram | `peer`, `result` | AE latency |
| `raft_append_entries_total` | counter | `peer`, `result` | AE volume |
| `raft_voters` | gauge | — | Current voters |
| `raft_non_voters` | gauge | — | Non-voters (overflow under voter cap) |
| `raft_voter_cap` | gauge | — | Configured cap |
| `raft_snapshot_total` | counter | `result` | Snapshots produced |
| `raft_snapshot_duration_seconds` | histogram | — | Snapshot duration |
| `raft_snapshot_size_bytes` | histogram | — | Snapshot size |

### Metrics — Gossip / membership (`gossip_*`)

| Name | Type | Labels | Captures |
|---|---|---|---|
| `gossip_members` | gauge | `state` | Member count by state (alive/suspect/dead/left) |
| `gossip_join_total` | counter | `result` | Join attempts |
| `gossip_leave_total` | counter | — | Voluntary leaves |
| `gossip_message_total` | counter | `kind`, `direction` | Messages by kind × direction |
| `gossip_message_bytes` | histogram | `kind`, `direction` | Wire size |
| `gossip_probe_duration_seconds` | histogram | `result` | Probe duration |
| `gossip_probe_failures_total` | counter | `target` | Probe failures |
| `gossip_convergence_seconds` | histogram | — | Time from change to detection by all peers |
| `gossip_suspicion_resolutions_total` | counter | `outcome` | suspect → alive or dead |

### Spans

| Subsystem | Span name | Key attributes |
|---|---|---|
| PG | `pg.join`, `pg.leave`, `pg.broadcast`, `pg.dispatch`, `pg.queue.enqueue` | `pg.name`, `node.id`, `result`, `error` |
| Raft | `raft.append_entries` | `peer`, `term`, `entries_count`, `result` |
| Raft | `raft.election` | `term`, `outcome`, `duration_ms` |
| Raft | `raft.snapshot`, `raft.apply` | `index`, `bytes`, `result` |
| Gossip | `gossip.probe` | `target`, `result`, `rtt_ms` |
| Gossip | `gossip.sync` | `peer`, `member_count` |
| Gossip | `gossip.broadcast` | `kind`, `bytes` |

Status is set to `Error` when `result != "ok"`; the `error` attribute carries
the underlying error message, redacted of any PII.

### Wiring

- `system/pg/service.go` resolves `metrics.Collector` from context (existing
  `ctxapi.Key`) and the tracer from `api/service/otel.FromContext`. Stores the
  resulting `*telemetry` on the `Service` struct. Each operation calls the
  appropriate `t.recordXxx(ctx, …)` helper, which is responsible both for
  metric emission and span management.
- `system/raft/raft.go` does the same, hooking state-machine transitions:
  `becameLeader`, `stepDown`, `appendEntries`, `applySnapshot`,
  `voterLadderChange`.
- `cluster/membership/membership.go` instruments hooks `onJoin`, `onLeave`,
  `onSuspect`, `onDead`, `onAlive`, plus the send/recv paths on the gossip
  protocol.

## Dashboards

### File layout

```
monkey/manifests/observability/
├── dashboards/
│   ├── 01-pg-operations-overview.json
│   ├── 02-pg-queue-backpressure.json
│   ├── 03-pg-circuit-breaker.json
│   ├── 04-pg-retry-storms.json
│   ├── 05-pg-dispatcher.json
│   ├── 06-pg-fence-tokens-globalreg.json
│   ├── 07-raft-leader-term.json
│   ├── 08-raft-commit-log.json
│   ├── 09-raft-voter-ladder.json
│   ├── 10-raft-snapshots.json
│   ├── 11-raft-append-entries.json
│   ├── 12-gossip-member-states.json
│   ├── 13-gossip-message-flow.json
│   ├── 14-gossip-convergence.json
│   ├── 15-chaos-experiments-overlay.json
│   ├── 16-chaos-mttr.json
│   ├── 17-chaos-split-brain-detector.json
│   ├── 18-runtime-overview.json
│   ├── 19-otel-pipeline-health.json
│   └── 20-pod-resources.json
├── grafana-dashboards-configmap.yaml   ← single ConfigMap mounting dashboards/
└── otel-collector.yaml                 ← unchanged
```

### Files removed (cleanup)

```
monkey/generate_runtime_dashboards.py
monkey/generate_dashboards.py
monkey/generate_dashboards_simple.py
monkey/manifests/observability/grafana-dashboards.json
monkey/manifests/observability/grafana-extra-dashboards.yaml
monkey/manifests/observability/grafana-complete.yaml
monkey/manifests/observability/prometheus-advanced-dashboard.yaml
```

### Dashboard inventory

#### PG (6)

1. **pg-operations-overview** — ops/s by op type, success rate, p50/p95/p99
   latency, top groups by throughput.
2. **pg-queue-backpressure** — queue depth per group, drops/s by reason, drop
   ratio %, time-in-queue distribution.
3. **pg-circuit-breaker** — breaker state timeline, trip count, trips per
   group heatmap, mean time in open.
4. **pg-retry-storms** — retries/s per op, attempt distribution, giveup rate,
   retry÷op ratio (storm signal).
5. **pg-dispatcher** — in-flight gauge, fan-out histogram, fan-out p99 vs
   membership size, dispatch latency.
6. **pg-fence-tokens-globalreg** — fence token timeline per node, rejections/s
   by reason, globalreg size, dedup events/s.

#### Raft (5)

7. **raft-leader-term** — current leader (state), term timeline, leader
   changes/min, election duration p50/p99.
8. **raft-commit-log** — commit index vs last log index, log lag heatmap,
   apply latency.
9. **raft-voter-ladder** — voters vs non-voters vs voter_cap, gauge per node,
   ladder transitions.
10. **raft-snapshots** — snapshot rate, duration histogram, size bytes,
    success ratio.
11. **raft-append-entries** — AE rate by peer, AE p99 latency by peer,
    failure rate, in-flight.

#### Gossip / membership (3)

12. **gossip-member-states** — alive/suspect/dead timeline, churn rate
    (transitions/min), per-node membership.
13. **gossip-message-flow** — tx/rx by kind, bytes/s, probe duration, probe
    failures by target.
14. **gossip-convergence** — convergence histogram, suspicion outcomes (alive
    vs dead), worst-case detection time.

#### Chaos × cluster (3)

15. **chaos-experiments-overlay** — chaos experiment timeline overlaid on
    `pg_op_duration_seconds` p99, raft leader changes, gossip churn (Grafana
    annotations).
16. **chaos-mttr** — recovery time = experiment-start → (gossip alive ratio
    back to 100% AND raft leader stable for ≥ 30s AND pg success rate ≥ 99%).
17. **chaos-split-brain-detector** — count of nodes with `raft_state = 2`
    simultaneously, membership partitions, fence-token rejection spikes.

#### Cross-cutting (3)

18. **runtime-overview** — top-line landing page: pods, raft leader, pg ops/s,
    gossip alive count, error rate.
19. **otel-pipeline-health** — otelcol receivers, exporter queue, dropped
    points/spans, collector CPU/RAM, jaeger up.
20. **pod-resources** — container CPU, memory, network RX/TX, disk per pod in
    `wippy-runtime` namespace.

### Panel conventions

- Datasource: Prometheus (`PBFA97CFB590B2093`).
- Auto-refresh: `5s`. Default time window: `now-15m to now`.
- Each dashboard opens with one stat row (3–4 stats) followed by detailed
  time-series and heatmaps where per-key latency matters.
- Dashboards 15–17 use Grafana annotations to render chaos experiment windows
  inline.
- No emojis anywhere.

## Configuration

`monkey/manifests/runtime/configmap.yaml` already injects
`OTEL_EXPORTER_OTLP_ENDPOINT`. We confirm during plan execution that the
`OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` and `_METRICS_ENDPOINT` reach the
collector (`otel-collector.observability.svc.cluster.local:4317`); add them if
absent. No collector-side changes — pipelines for `traces`, `metrics`, `logs`
are already configured.

## Testing

### Unit tests (Go)

- `runtime/system/pg/telemetry_test.go` — uses an in-memory
  `metrics.Collector` mock and an `sdktrace.TracerProvider` with a
  `tracetest.SpanRecorder`. Asserts (a) every operation emits the expected
  metric name with the expected label set, and (b) failed ops produce a span
  with `status = Error`.
- `runtime/system/raft/telemetry_test.go` — same pattern, covering state
  transitions, leader election, voter ladder change, snapshot.
- `runtime/cluster/membership/telemetry_test.go` — same pattern, covering
  member state changes and probe outcomes.

Tests do not exercise the OTLP exporter; the export path is already covered
by `runtime/service/otel/provider_test.go`.

### Smoke E2E (manual, on the local K3D cluster)

The chaos cluster runs locally via K3D (see `monkey/k3s/` for setup). No EKS
or other cloud cluster is involved.

1. Confirm the K3D cluster is up: `k3d cluster list` (or bring it up via the
   project's bootstrap script under `monkey/k3s/`).
2. `kubectl apply -k monkey/manifests/observability` (collector + Grafana +
   dashboards ConfigMap).
3. `kubectl apply -k monkey/manifests/runtime` (new runtime image — built
   locally and imported with `k3d image import`).
4. Run `monkey/scenarios/network-chaos.yaml`.
5. Open each of the 20 dashboards. **Acceptance:** no PG/Raft/Gossip panel
   shows "No data".
6. Open Jaeger UI; confirm spans `pg.broadcast`, `raft.append_entries`,
   `gossip.probe` from `service.name = wippy-runtime`.

## Success criteria

- `golangci-lint run ./...` clean in `runtime/`.
- `go test ./...` clean in `runtime/`.
- All 20 dashboards render with live data during a chaos scenario.
- Jaeger shows traces from all three subsystems.
- `kubectl -n wippy-cloud get pods` healthy after chaos (sanity).
- Net diff: `runtime/` adds ~3 files, ~450 lines. `monkey/` removes ~3000
  lines of stale generators / ConfigMaps and adds ~3000 lines of new dashboard
  JSON whose queries reference series that actually exist.

## Rollout

- `runtime/` work lands on the existing branch `feature/pg-process-groups`.
- `monkey/` is not currently a git repository on this workstation. If it is
  tracked elsewhere, the new files land on a `feature/pg-observability` branch
  there. **No MR/branch is created without explicit user approval.**
- Validation target is the **local K3D cluster** under `monkey/k3s/`. No EKS
  or cloud cluster is involved at any step.
- **Stage only.** No production deploys per project policy.
