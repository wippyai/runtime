# PG / Raft / Gossip OpenTelemetry Observability — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add OTel metrics + traces to `runtime/system/pg`, `runtime/system/raft`, `runtime/cluster/membership`, then ship 20 Grafana dashboards in `monkey/` that visualize the new signals during chaos scenarios.

**Architecture:** Per-package `telemetry.go` files own metric/span emission. Constructors accept `metrics.Collector`, `MeterProvider`, and `TracerProvider`; nil-safe, fall back to global no-ops. Service structs hold a `*telemetry`. Existing OTLP exporter (`runtime/service/otel`) and the chaos OTel collector (`monkey/manifests/observability/otel-collector.yaml`) handle export with no changes. Dashboards are 20 static JSON files mounted as a single Grafana ConfigMap; old generators and overlapping ConfigMaps are deleted.

**Tech Stack:** Go 1.22+, `go.opentelemetry.io/otel/metric`, `go.opentelemetry.io/otel/trace`, hashicorp/raft, hashicorp/memberlist, Prometheus, Grafana, K3D (local).

**Spec:** [`docs/superpowers/specs/2026-04-27-pg-raft-gossip-otel-observability-design.md`](../specs/2026-04-27-pg-raft-gossip-otel-observability-design.md)

**Repos touched:**
- `runtime/` — instrumentation code on branch `feature/pg-process-groups` (current).
- `monkey/` — dashboards + cleanup (not git-tracked locally; do not create branches/MRs without user approval).

**Validation target:** local K3D cluster (`monkey/k3s/`). No EKS / no production deploys.

---

## File Structure

### Created in `runtime/`

```
runtime/system/pg/telemetry.go              # PG meter, tracer, recorders
runtime/system/pg/telemetry_test.go         # PG telemetry unit tests

runtime/system/raft/telemetry.go            # Raft meter, tracer, recorders
runtime/system/raft/telemetry_test.go       # Raft telemetry unit tests

runtime/cluster/membership/telemetry.go      # Gossip meter, tracer, recorders
runtime/cluster/membership/telemetry_test.go # Gossip telemetry unit tests

runtime/internal/telemetrytest/recorder.go   # Shared in-memory metric+span recorder for tests
```

### Modified in `runtime/`

- `runtime/system/pg/service.go` — wire telemetry into `Service` struct + call recorders in `Join`/`Leave`/`Broadcast`/`submit`/handler paths.
- `runtime/system/pg/circuit_breaker.go` — emit on trip and state changes.
- `runtime/system/pg/retry.go` — emit on retry/giveup.
- `runtime/system/pg/dispatcher.go` — emit fan-out and dispatch latency.
- `runtime/system/raft/raft.go` — wire telemetry into `Node` struct + emit on state transitions, leader changes, voter ladder, snapshot, AppendEntries.
- `runtime/cluster/membership/membership.go` — wire telemetry + emit on `eventDelegate.NotifyJoin/Leave/Update` and `delegate.NotifyMsg`.

### Created in `monkey/`

```
monkey/manifests/observability/dashboards/01-pg-operations-overview.json
monkey/manifests/observability/dashboards/02-pg-queue-backpressure.json
monkey/manifests/observability/dashboards/03-pg-circuit-breaker.json
monkey/manifests/observability/dashboards/04-pg-retry-storms.json
monkey/manifests/observability/dashboards/05-pg-dispatcher.json
monkey/manifests/observability/dashboards/06-pg-fence-tokens-globalreg.json
monkey/manifests/observability/dashboards/07-raft-leader-term.json
monkey/manifests/observability/dashboards/08-raft-commit-log.json
monkey/manifests/observability/dashboards/09-raft-voter-ladder.json
monkey/manifests/observability/dashboards/10-raft-snapshots.json
monkey/manifests/observability/dashboards/11-raft-append-entries.json
monkey/manifests/observability/dashboards/12-gossip-member-states.json
monkey/manifests/observability/dashboards/13-gossip-message-flow.json
monkey/manifests/observability/dashboards/14-gossip-convergence.json
monkey/manifests/observability/dashboards/15-chaos-experiments-overlay.json
monkey/manifests/observability/dashboards/16-chaos-mttr.json
monkey/manifests/observability/dashboards/17-chaos-split-brain-detector.json
monkey/manifests/observability/dashboards/18-runtime-overview.json
monkey/manifests/observability/dashboards/19-otel-pipeline-health.json
monkey/manifests/observability/dashboards/20-pod-resources.json
monkey/manifests/observability/grafana-dashboards-configmap.yaml
```

### Deleted in `monkey/`

```
monkey/generate_runtime_dashboards.py
monkey/generate_dashboards.py
monkey/generate_dashboards_simple.py
monkey/manifests/observability/grafana-dashboards.json
monkey/manifests/observability/grafana-extra-dashboards.yaml
monkey/manifests/observability/grafana-complete.yaml
monkey/manifests/observability/prometheus-advanced-dashboard.yaml
```

---

## Naming and conventions (apply to every code task)

- Metric names: `pg_*`, `raft_*`, `gossip_*`. No `wippy_runtime_` prefix.
- Span names: `pg.<op>`, `raft.<op>`, `gossip.<op>` (lowercase, dotted).
- No emojis in metric names, span names, dashboard JSON, panel titles, file names, commit messages.
- Closed-enum labels only: `op`, `result`, `kind`, `direction`, `state`, `reason`, `outcome`. Never put request IDs / message IDs in labels.
- Each commit: focused, conventional commit message (`feat(pg): …`, `feat(raft): …`, `chore(monkey): …`, `test(pg): …`).

---

## Task 1: Shared telemetry test recorder

A tiny in-memory `metrics.Collector` impl + a thin wrapper around the OTel `tracetest.SpanRecorder` so each subsystem's `_test.go` doesn't reinvent it.

**Files:**
- Create: `runtime/internal/telemetrytest/recorder.go`
- Create: `runtime/internal/telemetrytest/recorder_test.go`

- [ ] **Step 1: Write the failing test**

`runtime/internal/telemetrytest/recorder_test.go`:

```go
// SPDX-License-Identifier: MPL-2.0

package telemetrytest

import (
	"testing"

	"github.com/wippyai/runtime/api/metrics"
)

func TestRecorder_RecordsCountersAndLabels(t *testing.T) {
	r := NewRecorder()

	r.CounterAdd("pg_join_total", 1, metrics.Labels{"pg": "g1", "result": "ok"})
	r.CounterAdd("pg_join_total", 1, metrics.Labels{"pg": "g1", "result": "ok"})
	r.CounterAdd("pg_join_total", 1, metrics.Labels{"pg": "g2", "result": "err"})

	got := r.CounterValue("pg_join_total", metrics.Labels{"pg": "g1", "result": "ok"})
	if got != 2 {
		t.Fatalf("want 2, got %v", got)
	}
	got = r.CounterValue("pg_join_total", metrics.Labels{"pg": "g2", "result": "err"})
	if got != 1 {
		t.Fatalf("want 1, got %v", got)
	}
}

func TestRecorder_GaugeAndHistogram(t *testing.T) {
	r := NewRecorder()
	r.GaugeSet("pg_queue_depth", 7, metrics.Labels{"pg": "g1"})
	if v := r.GaugeValue("pg_queue_depth", metrics.Labels{"pg": "g1"}); v != 7 {
		t.Fatalf("want 7, got %v", v)
	}
	r.HistogramObserve("pg_op_duration_seconds", 0.5, metrics.Labels{"pg": "g1", "op": "join"})
	r.HistogramObserve("pg_op_duration_seconds", 0.7, metrics.Labels{"pg": "g1", "op": "join"})
	if c := r.HistogramCount("pg_op_duration_seconds", metrics.Labels{"pg": "g1", "op": "join"}); c != 2 {
		t.Fatalf("want 2 observations, got %v", c)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /opt/workspace/wippy/runtime && go test ./internal/telemetrytest/...`
Expected: FAIL — package does not exist yet.

- [ ] **Step 3: Implement the recorder**

`runtime/internal/telemetrytest/recorder.go`:

```go
// SPDX-License-Identifier: MPL-2.0

// Package telemetrytest provides in-memory test doubles for metric and trace
// recording, used by subsystem telemetry_test.go files.
package telemetrytest

import (
	"sort"
	"strings"
	"sync"

	"github.com/wippyai/runtime/api/metrics"
)

type sample struct {
	value float64
	count uint64
}

type Recorder struct {
	mu      sync.Mutex
	samples map[string]map[string]*sample
}

func NewRecorder() *Recorder {
	return &Recorder{samples: make(map[string]map[string]*sample)}
}

func labelKey(labels metrics.Labels) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
	}
	return b.String()
}

func (r *Recorder) bucket(name, key string) *sample {
	m, ok := r.samples[name]
	if !ok {
		m = make(map[string]*sample)
		r.samples[name] = m
	}
	s, ok := m[key]
	if !ok {
		s = &sample{}
		m[key] = s
	}
	return s
}

func (r *Recorder) CounterInc(name string, labels metrics.Labels) {
	r.CounterAdd(name, 1, labels)
}

func (r *Recorder) CounterAdd(name string, delta float64, labels metrics.Labels) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.bucket(name, labelKey(labels))
	s.value += delta
}

func (r *Recorder) GaugeSet(name string, value float64, labels metrics.Labels) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.bucket(name, labelKey(labels))
	s.value = value
}

func (r *Recorder) GaugeInc(name string, labels metrics.Labels) {
	r.CounterAdd(name, 1, labels)
}

func (r *Recorder) GaugeDec(name string, labels metrics.Labels) {
	r.CounterAdd(name, -1, labels)
}

func (r *Recorder) HistogramObserve(name string, value float64, labels metrics.Labels) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.bucket(name, labelKey(labels))
	s.value += value
	s.count++
}

func (r *Recorder) RegisterExporter(_ metrics.Exporter) error { return nil }

func (r *Recorder) Close() error { return nil }

func (r *Recorder) CounterValue(name string, labels metrics.Labels) float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.samples[name]; ok {
		if s, ok := m[labelKey(labels)]; ok {
			return s.value
		}
	}
	return 0
}

func (r *Recorder) GaugeValue(name string, labels metrics.Labels) float64 {
	return r.CounterValue(name, labels)
}

func (r *Recorder) HistogramCount(name string, labels metrics.Labels) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.samples[name]; ok {
		if s, ok := m[labelKey(labels)]; ok {
			return s.count
		}
	}
	return 0
}

// Names returns all metric names recorded so far (sorted).
func (r *Recorder) Names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.samples))
	for k := range r.samples {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var _ metrics.Collector = (*Recorder)(nil)
```

- [ ] **Step 4: Run tests**

Run: `cd /opt/workspace/wippy/runtime && go test ./internal/telemetrytest/... -v`
Expected: PASS for both tests.

- [ ] **Step 5: Lint**

Run: `cd /opt/workspace/wippy/runtime && golangci-lint run ./internal/telemetrytest/...`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
cd /opt/workspace/wippy/runtime && \
  git add internal/telemetrytest/recorder.go internal/telemetrytest/recorder_test.go && \
  git commit -m "feat(telemetrytest): add in-memory recorder for telemetry unit tests"
```

---

## Task 2: PG telemetry — file skeleton, ops counters, latency histograms

Cover `pg_join_total`, `pg_leave_total`, `pg_broadcast_total`, `pg_broadcast_recipients`, `pg_op_duration_seconds`. Wire into `Join`, `Leave`, `Broadcast`.

**Files:**
- Create: `runtime/system/pg/telemetry.go`
- Create: `runtime/system/pg/telemetry_test.go`
- Modify: `runtime/system/pg/service.go` (struct, constructor, `Join` ~L556, `Leave` ~L670, `Broadcast` ~L972)

- [ ] **Step 1: Write the failing test**

`runtime/system/pg/telemetry_test.go`:

```go
// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"testing"
	"time"

	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/internal/telemetrytest"
)

func newTestTelemetry(t *testing.T) (*telemetry, *telemetrytest.Recorder) {
	t.Helper()
	rec := telemetrytest.NewRecorder()
	tt := newTelemetry(rec, nil, nil)
	if tt == nil {
		t.Fatalf("newTelemetry returned nil")
	}
	return tt, rec
}

func TestTelemetry_RecordJoin_OK(t *testing.T) {
	tt, rec := newTestTelemetry(t)
	tt.recordJoin("g1", nil, 12*time.Millisecond)

	if v := rec.CounterValue("pg_join_total", metrics.Labels{"pg": "g1", "result": "ok"}); v != 1 {
		t.Fatalf("pg_join_total{pg=g1,result=ok}: want 1, got %v", v)
	}
	if c := rec.HistogramCount("pg_op_duration_seconds", metrics.Labels{"pg": "g1", "op": "join"}); c != 1 {
		t.Fatalf("pg_op_duration_seconds count: want 1, got %v", c)
	}
}

func TestTelemetry_RecordJoin_Error(t *testing.T) {
	tt, rec := newTestTelemetry(t)
	tt.recordJoin("g1", errSomething, 0)
	if v := rec.CounterValue("pg_join_total", metrics.Labels{"pg": "g1", "result": "err"}); v != 1 {
		t.Fatalf("pg_join_total{result=err}: want 1, got %v", v)
	}
}

func TestTelemetry_RecordLeave(t *testing.T) {
	tt, rec := newTestTelemetry(t)
	tt.recordLeave("g1", nil, 5*time.Millisecond)
	if v := rec.CounterValue("pg_leave_total", metrics.Labels{"pg": "g1", "result": "ok"}); v != 1 {
		t.Fatalf("pg_leave_total: want 1, got %v", v)
	}
}

func TestTelemetry_RecordBroadcast(t *testing.T) {
	tt, rec := newTestTelemetry(t)
	tt.recordBroadcast("g1", 7, nil, 20*time.Millisecond)
	if v := rec.CounterValue("pg_broadcast_total", metrics.Labels{"pg": "g1", "result": "ok"}); v != 1 {
		t.Fatalf("pg_broadcast_total: want 1, got %v", v)
	}
	if c := rec.HistogramCount("pg_broadcast_recipients", metrics.Labels{"pg": "g1"}); c != 1 {
		t.Fatalf("pg_broadcast_recipients count: want 1, got %v", c)
	}
}

func TestTelemetry_NilCollector_NoPanic(t *testing.T) {
	tt := newTelemetry(nil, nil, nil)
	tt.recordJoin("g1", nil, time.Millisecond)
	tt.recordLeave("g1", nil, time.Millisecond)
	tt.recordBroadcast("g1", 1, nil, time.Millisecond)
}

var errSomething = errStub("boom")

type errStub string

func (e errStub) Error() string { return string(e) }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /opt/workspace/wippy/runtime && go test ./system/pg/ -run TestTelemetry -v`
Expected: FAIL — `newTelemetry` undefined.

- [ ] **Step 3: Create `telemetry.go` with the join/leave/broadcast recorders**

`runtime/system/pg/telemetry.go`:

```go
// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"context"
	"time"

	"github.com/wippyai/runtime/api/metrics"
	"go.opentelemetry.io/otel"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// telemetry owns metric and span emission for the pg subsystem. All recorders
// are nil-safe so callers can ignore the absence of a configured collector or
// tracer (e.g., in unit tests that don't wire OTel).
type telemetry struct {
	coll   metrics.Collector
	tracer trace.Tracer
}

func newTelemetry(coll metrics.Collector, mp otelmetric.MeterProvider, tp trace.TracerProvider) *telemetry {
	if tp == nil {
		tp = otel.GetTracerProvider()
	}
	return &telemetry{
		coll:   coll,
		tracer: tp.Tracer("wippy-runtime"),
	}
}

func resultLabel(err error) string {
	if err != nil {
		return "err"
	}
	return "ok"
}

func (t *telemetry) recordJoin(pg string, err error, dur time.Duration) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("pg_join_total", metrics.Labels{"pg": pg, "result": resultLabel(err)})
	t.coll.HistogramObserve("pg_op_duration_seconds", dur.Seconds(), metrics.Labels{"pg": pg, "op": "join"})
}

func (t *telemetry) recordLeave(pg string, err error, dur time.Duration) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("pg_leave_total", metrics.Labels{"pg": pg, "result": resultLabel(err)})
	t.coll.HistogramObserve("pg_op_duration_seconds", dur.Seconds(), metrics.Labels{"pg": pg, "op": "leave"})
}

func (t *telemetry) recordBroadcast(pg string, recipients int, err error, dur time.Duration) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("pg_broadcast_total", metrics.Labels{"pg": pg, "result": resultLabel(err)})
	t.coll.HistogramObserve("pg_broadcast_recipients", float64(recipients), metrics.Labels{"pg": pg})
	t.coll.HistogramObserve("pg_op_duration_seconds", dur.Seconds(), metrics.Labels{"pg": pg, "op": "broadcast"})
}

// Span helpers (used by Tasks 5 and on). Kept here so future callers don't
// re-derive the tracer.
func (t *telemetry) startSpan(ctx context.Context, name string, attrs ...trace.SpanStartOption) (context.Context, trace.Span) {
	if t == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return t.tracer.Start(ctx, name, attrs...)
}
```

- [ ] **Step 4: Run tests**

Run: `cd /opt/workspace/wippy/runtime && go test ./system/pg/ -run TestTelemetry -v`
Expected: all 5 tests PASS.

- [ ] **Step 5: Wire into `Service`**

Modify `runtime/system/pg/service.go`:

(a) Add field to the `Service` struct (around line 88 — after `maxRetries int`):

```go
	maxRetries         int
	tel                *telemetry
}
```

(b) Update `NewService` signature to accept telemetry deps and construct it. Append three new params after `localNodeID pid.NodeID`:

```go
func NewService(
	logger *zap.Logger,
	hostID pid.HostID,
	config *pgapi.Config,
	router relay.Receiver,
	topo topology.Topology,
	membership cluster.Membership,
	bus event.Bus,
	localNodeID pid.NodeID,
	coll metrics.Collector,
	mp otelmetric.MeterProvider,
	tp trace.TracerProvider,
) *Service {
```

Add the imports at the top:

```go
	"github.com/wippyai/runtime/api/metrics"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
```

In the constructor body, before `return svc`:

```go
	svc.tel = newTelemetry(coll, mp, tp)
```

(c) Wire calls into `Join`, `Leave`, `Broadcast`. Inside `Join` (currently around line 556) wrap the existing body with timing + end-of-op record:

```go
func (s *Service) Join(group pgapi.Group, p pid.PID) error {
	start := time.Now()
	done := make(chan error, 1)
	if !s.submit(func() {
		// ... existing body unchanged ...
	}) {
		err := s.submitError()
		s.tel.recordJoin(string(group), err, time.Since(start))
		return err
	}

	select {
	case err := <-done:
		s.tel.recordJoin(string(group), err, time.Since(start))
		return err
	case <-s.currentCtx().Done():
		s.tel.recordJoin(string(group), ErrServiceStopped, time.Since(start))
		return ErrServiceStopped
	}
}
```

Apply the same pattern to `Leave` (~L670) → `s.tel.recordLeave(string(group), err, time.Since(start))`.

For `Broadcast` (~L972), capture recipients count from the existing `(int, error)` return:

```go
func (s *Service) Broadcast(from pid.PID, group pgapi.Group, topic string, payloads payload.Payloads) (int, error) {
	start := time.Now()
	n, err := s.broadcast(from, group, topic, payloads, false)
	s.tel.recordBroadcast(string(group), n, err, time.Since(start))
	return n, err
}
```

(If the existing body inlines the broadcast logic rather than calling a helper, leave the body as-is and just wrap with the timer+record at the top and at every return path.)

(d) Update **all callers** of `NewService` to pass the three new args. Search:

```bash
cd /opt/workspace/wippy/runtime && rg "pg\.NewService\(" --type go
```

For each caller, pass `nil, nil, nil` if the caller does not have providers (tests, ad-hoc bringup); pass the wired providers when the caller has them (production boot, `boot/components`).

- [ ] **Step 6: Run package tests + lint**

Run:
```bash
cd /opt/workspace/wippy/runtime && \
  go test ./system/pg/... && \
  golangci-lint run ./system/pg/... ./internal/telemetrytest/...
```
Expected: clean.

- [ ] **Step 7: Commit**

```bash
cd /opt/workspace/wippy/runtime && \
  git add system/pg/telemetry.go system/pg/telemetry_test.go system/pg/service.go && \
  git commit -m "feat(pg): emit OTel metrics for join/leave/broadcast operations"
```

> Update callers committed in Step 5(d) live in the same commit.

---

## Task 3: PG telemetry — queue depth, drops, circuit breaker, retry

Cover `pg_queue_depth`, `pg_queue_dropped_total`, `pg_circuit_breaker_state`, `pg_circuit_breaker_trips_total`, `pg_retry_total`, `pg_retry_giveup_total`.

**Files:**
- Modify: `runtime/system/pg/telemetry.go` (extend recorders)
- Modify: `runtime/system/pg/telemetry_test.go` (new tests)
- Modify: `runtime/system/pg/service.go` (`submit`, queue drop path)
- Modify: `runtime/system/pg/circuit_breaker.go` (trip / state-change emit)
- Modify: `runtime/system/pg/retry.go` (per-attempt and giveup emit)

- [ ] **Step 1: Write failing tests**

Append to `runtime/system/pg/telemetry_test.go`:

```go
func TestTelemetry_RecordQueue(t *testing.T) {
	tt, rec := newTestTelemetry(t)
	tt.recordQueueDepth("g1", 7)
	if v := rec.GaugeValue("pg_queue_depth", metrics.Labels{"pg": "g1"}); v != 7 {
		t.Fatalf("pg_queue_depth: want 7, got %v", v)
	}
	tt.recordQueueDropped("g1", "full")
	if v := rec.CounterValue("pg_queue_dropped_total", metrics.Labels{"pg": "g1", "reason": "full"}); v != 1 {
		t.Fatalf("pg_queue_dropped_total: want 1, got %v", v)
	}
}

func TestTelemetry_RecordCircuitBreaker(t *testing.T) {
	tt, rec := newTestTelemetry(t)
	tt.recordCircuitBreakerState("g1", "open")
	if v := rec.GaugeValue("pg_circuit_breaker_state", metrics.Labels{"pg": "g1"}); v != 2 {
		t.Fatalf("cb_state(open): want 2, got %v", v)
	}
	tt.recordCircuitBreakerState("g1", "half-open")
	if v := rec.GaugeValue("pg_circuit_breaker_state", metrics.Labels{"pg": "g1"}); v != 1 {
		t.Fatalf("cb_state(half-open): want 1, got %v", v)
	}
	tt.recordCircuitBreakerTrip("g1")
	if v := rec.CounterValue("pg_circuit_breaker_trips_total", metrics.Labels{"pg": "g1"}); v != 1 {
		t.Fatalf("cb_trips: want 1, got %v", v)
	}
}

func TestTelemetry_RecordRetry(t *testing.T) {
	tt, rec := newTestTelemetry(t)
	tt.recordRetry("g1", "broadcast", 2)
	if v := rec.CounterValue("pg_retry_total", metrics.Labels{"pg": "g1", "op": "broadcast", "attempt": "2"}); v != 1 {
		t.Fatalf("pg_retry_total: want 1, got %v", v)
	}
	tt.recordRetryGiveup("g1", "broadcast")
	if v := rec.CounterValue("pg_retry_giveup_total", metrics.Labels{"pg": "g1", "op": "broadcast"}); v != 1 {
		t.Fatalf("pg_retry_giveup_total: want 1, got %v", v)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /opt/workspace/wippy/runtime && go test ./system/pg/ -run TestTelemetry -v`
Expected: FAIL — recorders missing.

- [ ] **Step 3: Extend `telemetry.go`**

Append to `runtime/system/pg/telemetry.go`:

```go
import "strconv"

func cbStateValue(state string) float64 {
	switch state {
	case "open":
		return 2
	case "half-open":
		return 1
	default: // "closed"
		return 0
	}
}

func (t *telemetry) recordQueueDepth(pg string, depth int) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.GaugeSet("pg_queue_depth", float64(depth), metrics.Labels{"pg": pg})
}

func (t *telemetry) recordQueueDropped(pg, reason string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("pg_queue_dropped_total", metrics.Labels{"pg": pg, "reason": reason})
}

func (t *telemetry) recordCircuitBreakerState(pg, state string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.GaugeSet("pg_circuit_breaker_state", cbStateValue(state), metrics.Labels{"pg": pg})
}

func (t *telemetry) recordCircuitBreakerTrip(pg string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("pg_circuit_breaker_trips_total", metrics.Labels{"pg": pg})
}

func (t *telemetry) recordRetry(pg, op string, attempt int) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("pg_retry_total", metrics.Labels{"pg": pg, "op": op, "attempt": strconv.Itoa(attempt)})
}

func (t *telemetry) recordRetryGiveup(pg, op string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("pg_retry_giveup_total", metrics.Labels{"pg": pg, "op": op})
}
```

(Move the `import "strconv"` into the existing import block at the top of the file.)

- [ ] **Step 4: Run tests**

Run: `cd /opt/workspace/wippy/runtime && go test ./system/pg/ -run TestTelemetry -v`
Expected: PASS.

- [ ] **Step 5: Wire into `submit` (queue depth + drops)**

Modify `runtime/system/pg/service.go`. Find `func (s *Service) submit(fn action)` (~L299). At the start of the function body, after the existing length read used for the warn-threshold check, call:

```go
	depth := len(s.actions)
	s.tel.recordQueueDepth(s.hostID, depth)
```

If the function has a path that drops the action (queue full / cancelled), call:

```go
	s.tel.recordQueueDropped(s.hostID, "full")
```

(Use `"cancelled"` for the closed-context branch instead.)

- [ ] **Step 6: Wire into circuit breaker**

In `runtime/system/pg/circuit_breaker.go`, on every state transition emit `recordCircuitBreakerState`; on each trip emit `recordCircuitBreakerTrip`. The breaker manager already has a back-reference path; extend the existing `cbManager` to receive `*telemetry`. Suggested change pattern: pass `tel *telemetry` into `newCircuitBreakerManager` (currently constructed at `service.go:147`) and store it on the manager. Wherever the breaker logs a state change, immediately call `mgr.tel.recordCircuitBreakerState(group, newState)`. Wherever it transitions to `open`, additionally call `mgr.tel.recordCircuitBreakerTrip(group)`.

- [ ] **Step 7: Wire into retry**

In `runtime/system/pg/retry.go`, on each retry attempt call `q.tel.recordRetry(group, op, attempt)`; on giveup call `q.tel.recordRetryGiveup(group, op)`. Extend `newRetryQueue` (constructed at `service.go:154`) with a `tel *telemetry` arg.

- [ ] **Step 8: Run package tests + lint**

Run:
```bash
cd /opt/workspace/wippy/runtime && \
  go test ./system/pg/... && \
  golangci-lint run ./system/pg/...
```
Expected: clean.

- [ ] **Step 9: Commit**

```bash
cd /opt/workspace/wippy/runtime && \
  git add system/pg/telemetry.go system/pg/telemetry_test.go system/pg/service.go \
          system/pg/circuit_breaker.go system/pg/retry.go && \
  git commit -m "feat(pg): emit OTel metrics for queue, circuit breaker, retry"
```

---

## Task 4: PG telemetry — dispatcher, fence tokens, globalreg

Cover `pg_dispatcher_inflight`, `pg_fence_token`, `pg_fence_rejection_total`, `pg_globalreg_size`, `pg_globalreg_dedupe_total`.

**Files:**
- Modify: `runtime/system/pg/telemetry.go`
- Modify: `runtime/system/pg/telemetry_test.go`
- Modify: `runtime/system/pg/dispatcher.go`
- Modify: `runtime/system/pg/service.go` (fence token emit on ApplyFence; globalreg accessor hooks — emit when registry size changes or a dedupe event occurs)

- [ ] **Step 1: Write failing tests**

Append to `runtime/system/pg/telemetry_test.go`:

```go
func TestTelemetry_RecordDispatcher(t *testing.T) {
	tt, rec := newTestTelemetry(t)
	tt.recordDispatcherInflight("g1", 4)
	if v := rec.GaugeValue("pg_dispatcher_inflight", metrics.Labels{"pg": "g1"}); v != 4 {
		t.Fatalf("pg_dispatcher_inflight: want 4, got %v", v)
	}
}

func TestTelemetry_RecordFence(t *testing.T) {
	tt, rec := newTestTelemetry(t)
	tt.recordFenceToken("g1", "node-a", 7)
	if v := rec.GaugeValue("pg_fence_token", metrics.Labels{"pg": "g1", "node": "node-a"}); v != 7 {
		t.Fatalf("pg_fence_token: want 7, got %v", v)
	}
	tt.recordFenceRejection("g1", "stale_token")
	if v := rec.CounterValue("pg_fence_rejection_total", metrics.Labels{"pg": "g1", "reason": "stale_token"}); v != 1 {
		t.Fatalf("pg_fence_rejection_total: want 1, got %v", v)
	}
}

func TestTelemetry_RecordGlobalreg(t *testing.T) {
	tt, rec := newTestTelemetry(t)
	tt.recordGlobalregSize(42)
	if v := rec.GaugeValue("pg_globalreg_size", nil); v != 42 {
		t.Fatalf("pg_globalreg_size: want 42, got %v", v)
	}
	tt.recordGlobalregDedupe()
	if v := rec.CounterValue("pg_globalreg_dedupe_total", nil); v != 1 {
		t.Fatalf("pg_globalreg_dedupe_total: want 1, got %v", v)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /opt/workspace/wippy/runtime && go test ./system/pg/ -run TestTelemetry -v`
Expected: FAIL.

- [ ] **Step 3: Add recorders**

Append to `runtime/system/pg/telemetry.go`:

```go
func (t *telemetry) recordDispatcherInflight(pg string, n int) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.GaugeSet("pg_dispatcher_inflight", float64(n), metrics.Labels{"pg": pg})
}

func (t *telemetry) recordFenceToken(pg, node string, token uint64) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.GaugeSet("pg_fence_token", float64(token), metrics.Labels{"pg": pg, "node": node})
}

func (t *telemetry) recordFenceRejection(pg, reason string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("pg_fence_rejection_total", metrics.Labels{"pg": pg, "reason": reason})
}

func (t *telemetry) recordGlobalregSize(n int) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.GaugeSet("pg_globalreg_size", float64(n), nil)
}

func (t *telemetry) recordGlobalregDedupe() {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("pg_globalreg_dedupe_total", nil)
}
```

- [ ] **Step 4: Run tests**

Run: `cd /opt/workspace/wippy/runtime && go test ./system/pg/ -run TestTelemetry -v`
Expected: PASS.

- [ ] **Step 5: Wire fence tokens into `service.go`**

The fence-token persistence path was added in commit `6af795957` (`fix(globalreg,pg): dedupe registrations and persist fence tokens`). Locate the function that updates `fenceCaches` (search: `rg "fenceCaches" runtime/system/pg/`). Where the cache is mutated to a new value, add:

```go
	s.tel.recordFenceToken(pg, node, token)
```

Where a message is dropped because of stale token, add:

```go
	s.tel.recordFenceRejection(pg, "stale_token")
```

(Use `"unknown_node"` for the unknown-source path if present.)

- [ ] **Step 6: Wire dispatcher inflight**

In `runtime/system/pg/dispatcher.go`, locate the field tracking active dispatches (`inFlight`, `pending`, or similar — search: `rg "inflight\|pending" runtime/system/pg/dispatcher.go`). On increment and on decrement, call:

```go
	d.tel.recordDispatcherInflight(d.group, atomic.LoadInt32(&d.inFlight))
```

Pass `*telemetry` into the dispatcher constructor.

- [ ] **Step 7: Wire globalreg**

The globalreg path is owned by `runtime/system/globalreg/...` (separate package). For this task, only emit from the **PG-side observer** that already sits in `service.go` and reacts to globalreg changes (`handleNodeJoinedEvent`, `handleNodeLeftEvent`, broadcast handlers). Where the local view of registrations changes, emit:

```go
	s.tel.recordGlobalregSize(len(s.state.local))
```

Where the dedupe path discards a duplicate registration (commit `6af795957` introduced this), emit:

```go
	s.tel.recordGlobalregDedupe()
```

- [ ] **Step 8: Run package tests + lint**

Run:
```bash
cd /opt/workspace/wippy/runtime && \
  go test ./system/pg/... && \
  golangci-lint run ./system/pg/...
```

- [ ] **Step 9: Commit**

```bash
cd /opt/workspace/wippy/runtime && \
  git add system/pg/telemetry.go system/pg/telemetry_test.go system/pg/dispatcher.go system/pg/service.go && \
  git commit -m "feat(pg): emit OTel metrics for dispatcher, fence tokens, globalreg"
```

---

## Task 5: PG telemetry — spans for join/leave/broadcast/dispatch

Add tracing on the same operations already metric'd. Use the `tracetest.SpanRecorder` from `go.opentelemetry.io/otel/sdk/trace/tracetest` to assert spans.

**Files:**
- Modify: `runtime/system/pg/telemetry.go`
- Modify: `runtime/system/pg/telemetry_test.go`
- Modify: `runtime/system/pg/service.go`

- [ ] **Step 1: Write failing test**

Append to `runtime/system/pg/telemetry_test.go`:

```go
import (
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/codes"
)

func newTestTelemetryWithSpans(t *testing.T) (*telemetry, *telemetrytest.Recorder, *tracetest.SpanRecorder) {
	t.Helper()
	rec := telemetrytest.NewRecorder()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	return newTelemetry(rec, nil, tp), rec, sr
}

func TestTelemetry_JoinSpan_Success(t *testing.T) {
	tt, _, sr := newTestTelemetryWithSpans(t)
	ctx, span := tt.startSpan(context.Background(), "pg.join")
	span.End()
	_ = ctx
	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("want 1 span, got %d", len(spans))
	}
	if spans[0].Name() != "pg.join" {
		t.Fatalf("name: want pg.join, got %s", spans[0].Name())
	}
}

func TestTelemetry_SetSpanError(t *testing.T) {
	tt, _, sr := newTestTelemetryWithSpans(t)
	_, span := tt.startSpan(context.Background(), "pg.broadcast")
	tt.setSpanError(span, errSomething)
	span.End()
	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatal("want 1 span")
	}
	if spans[0].Status().Code != codes.Error {
		t.Fatalf("want Error code, got %v", spans[0].Status().Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /opt/workspace/wippy/runtime && go test ./system/pg/ -run TestTelemetry_JoinSpan -v`
Expected: FAIL — `setSpanError` undefined.

- [ ] **Step 3: Implement `setSpanError` and span attributes helpers**

Append to `runtime/system/pg/telemetry.go`:

```go
import "go.opentelemetry.io/otel/codes"

func (t *telemetry) setSpanError(span trace.Span, err error) {
	if err == nil || span == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
```

Move the `codes` import into the existing import block.

- [ ] **Step 4: Wrap operations with spans**

In `runtime/system/pg/service.go`:

```go
func (s *Service) Join(group pgapi.Group, p pid.PID) error {
	ctx, span := s.tel.startSpan(s.currentCtx(), "pg.join",
		trace.WithAttributes(
			attribute.String("pg.name", string(group)),
			attribute.String("node.id", s.localNodeID),
		),
	)
	defer span.End()
	start := time.Now()
	// ... existing body ...
	err := <- done   // illustrative; actual structure preserved
	s.tel.setSpanError(span, err)
	s.tel.recordJoin(string(group), err, time.Since(start))
	_ = ctx
	return err
}
```

Apply the same wrap to `Leave` and `Broadcast`. For `Broadcast` add:

```go
	span.SetAttributes(attribute.Int("pg.recipients", n))
```

Add imports:

```go
	"go.opentelemetry.io/otel/attribute"
```

- [ ] **Step 5: Run tests + lint**

Run:
```bash
cd /opt/workspace/wippy/runtime && \
  go test ./system/pg/... && \
  golangci-lint run ./system/pg/...
```

- [ ] **Step 6: Commit**

```bash
cd /opt/workspace/wippy/runtime && \
  git add system/pg/telemetry.go system/pg/telemetry_test.go system/pg/service.go && \
  git commit -m "feat(pg): emit OTel spans for join/leave/broadcast"
```

---

## Task 6: Raft telemetry — state, term, leader, election

Cover `raft_state`, `raft_term`, `raft_leader_changes_total`, `raft_election_duration_seconds`. Wire into leadership monitor.

**Files:**
- Create: `runtime/system/raft/telemetry.go`
- Create: `runtime/system/raft/telemetry_test.go`
- Modify: `runtime/system/raft/raft.go` (add field, constructor params, hook leadership events)

- [ ] **Step 1: Write failing tests**

`runtime/system/raft/telemetry_test.go`:

```go
// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"testing"
	"time"

	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/internal/telemetrytest"
)

func newTestTel(t *testing.T) (*telemetry, *telemetrytest.Recorder) {
	t.Helper()
	rec := telemetrytest.NewRecorder()
	return newTelemetry(rec, nil, nil), rec
}

func TestRaftTelemetry_State(t *testing.T) {
	tt, rec := newTestTel(t)
	tt.recordState("node-a", "leader")
	if v := rec.GaugeValue("raft_state", metrics.Labels{"node": "node-a"}); v != 2 {
		t.Fatalf("raft_state(leader): want 2, got %v", v)
	}
	tt.recordState("node-a", "candidate")
	if v := rec.GaugeValue("raft_state", metrics.Labels{"node": "node-a"}); v != 1 {
		t.Fatalf("raft_state(candidate): want 1, got %v", v)
	}
	tt.recordState("node-a", "follower")
	if v := rec.GaugeValue("raft_state", metrics.Labels{"node": "node-a"}); v != 0 {
		t.Fatalf("raft_state(follower): want 0, got %v", v)
	}
}

func TestRaftTelemetry_TermAndLeaderChange(t *testing.T) {
	tt, rec := newTestTel(t)
	tt.recordTerm(5)
	if v := rec.GaugeValue("raft_term", nil); v != 5 {
		t.Fatalf("raft_term: want 5, got %v", v)
	}
	tt.recordLeaderChange()
	tt.recordLeaderChange()
	if v := rec.CounterValue("raft_leader_changes_total", nil); v != 2 {
		t.Fatalf("raft_leader_changes_total: want 2, got %v", v)
	}
}

func TestRaftTelemetry_Election(t *testing.T) {
	tt, rec := newTestTel(t)
	tt.recordElection(50 * time.Millisecond)
	if c := rec.HistogramCount("raft_election_duration_seconds", nil); c != 1 {
		t.Fatalf("raft_election_duration_seconds: want 1, got %v", c)
	}
}

func TestRaftTelemetry_NilSafe(t *testing.T) {
	tt := newTelemetry(nil, nil, nil)
	tt.recordState("node-a", "leader")
	tt.recordTerm(1)
	tt.recordLeaderChange()
	tt.recordElection(time.Millisecond)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /opt/workspace/wippy/runtime && go test ./system/raft/ -run TestRaftTelemetry -v`
Expected: FAIL — package missing `telemetry`.

- [ ] **Step 3: Create `telemetry.go`**

`runtime/system/raft/telemetry.go`:

```go
// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"context"
	"time"

	"github.com/wippyai/runtime/api/metrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type telemetry struct {
	coll   metrics.Collector
	tracer trace.Tracer
}

func newTelemetry(coll metrics.Collector, mp otelmetric.MeterProvider, tp trace.TracerProvider) *telemetry {
	if tp == nil {
		tp = otel.GetTracerProvider()
	}
	return &telemetry{coll: coll, tracer: tp.Tracer("wippy-runtime")}
}

func raftStateValue(state string) float64 {
	switch state {
	case "leader":
		return 2
	case "candidate":
		return 1
	default: // follower or shutdown
		return 0
	}
}

func raftResultLabel(err error) string {
	if err != nil {
		return "err"
	}
	return "ok"
}

func (t *telemetry) recordState(node, state string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.GaugeSet("raft_state", raftStateValue(state), metrics.Labels{"node": node})
}

func (t *telemetry) recordTerm(term uint64) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.GaugeSet("raft_term", float64(term), nil)
}

func (t *telemetry) recordLeaderChange() {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("raft_leader_changes_total", nil)
}

func (t *telemetry) recordElection(dur time.Duration) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.HistogramObserve("raft_election_duration_seconds", dur.Seconds(), nil)
}

func (t *telemetry) startSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if t == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return t.tracer.Start(ctx, name, opts...)
}

func (t *telemetry) setSpanError(span trace.Span, err error) {
	if err == nil || span == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
```

- [ ] **Step 4: Run tests**

Run: `cd /opt/workspace/wippy/runtime && go test ./system/raft/ -run TestRaftTelemetry -v`
Expected: PASS.

- [ ] **Step 5: Wire into `Node`**

In `runtime/system/raft/raft.go`:

(a) Add field on `Node` struct (~L40, before the closing brace):

```go
	tel         *telemetry
```

(b) Update `NewNode` signature:

```go
func NewNode(localID string, fsm hraft.FSM, cfg raftapi.Config, bus event.Bus, logger *zap.Logger,
	coll metrics.Collector, mp otelmetric.MeterProvider, tp trace.TracerProvider) *Node {
	cfg.InitDefaults()
	return &Node{
		fsm:     fsm,
		config:  cfg,
		localID: localID,
		bus:     bus,
		logger:  logger,
		stopCh:  make(chan struct{}),
		tel:     newTelemetry(coll, mp, tp),
	}
}
```

Add the imports:

```go
	"github.com/wippyai/runtime/api/metrics"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
```

(c) Hook into `monitorLeadership` (~L218). The current loop reads `LeaderCh()` and emits status events; add at every iteration:

```go
		state := strings.ToLower(n.raft.State().String())
		n.tel.recordState(n.localID, state)
		n.tel.recordTerm(n.raft.LastIndex()) // leader's last commit index doubles as term-anchor display in dashboards; replace with `n.raft.Stats()["term"]` if available
```

Where the current code detects a transition into leadership, also emit `n.tel.recordLeaderChange()`. If the function tracks `electionStart time.Time`, on the transition emit:

```go
		n.tel.recordElection(time.Since(electionStart))
```

(If election timing is not currently tracked, add `electionStart = time.Now()` when the previous state was `follower` or `candidate` and the new state is `leader`.)

(d) Update **all callers** of `NewNode`:

```bash
cd /opt/workspace/wippy/runtime && rg "raft\.NewNode\(" --type go
```

Pass `nil, nil, nil` for non-production callers (tests).

- [ ] **Step 6: Run tests + lint**

Run:
```bash
cd /opt/workspace/wippy/runtime && \
  go test ./system/raft/... && \
  golangci-lint run ./system/raft/... ./internal/telemetrytest/...
```

- [ ] **Step 7: Commit**

```bash
cd /opt/workspace/wippy/runtime && \
  git add system/raft/telemetry.go system/raft/telemetry_test.go system/raft/raft.go && \
  git commit -m "feat(raft): emit OTel metrics for state, term, leader changes, elections"
```

---

## Task 7: Raft telemetry — commit / log / append-entries

Cover `raft_commit_index`, `raft_last_log_index`, `raft_log_lag`, `raft_append_entries_duration_seconds`, `raft_append_entries_total`.

**Files:**
- Modify: `runtime/system/raft/telemetry.go`
- Modify: `runtime/system/raft/telemetry_test.go`
- Modify: `runtime/system/raft/raft.go`

- [ ] **Step 1: Write failing tests**

Append to `runtime/system/raft/telemetry_test.go`:

```go
func TestRaftTelemetry_CommitLog(t *testing.T) {
	tt, rec := newTestTel(t)
	tt.recordCommitIndex(100)
	tt.recordLastLogIndex("node-a", 95)
	tt.recordLogLag("node-a", 5)
	if v := rec.GaugeValue("raft_commit_index", nil); v != 100 {
		t.Fatalf("raft_commit_index: want 100, got %v", v)
	}
	if v := rec.GaugeValue("raft_last_log_index", metrics.Labels{"node": "node-a"}); v != 95 {
		t.Fatalf("raft_last_log_index: want 95, got %v", v)
	}
	if v := rec.GaugeValue("raft_log_lag", metrics.Labels{"node": "node-a"}); v != 5 {
		t.Fatalf("raft_log_lag: want 5, got %v", v)
	}
}

func TestRaftTelemetry_AppendEntries(t *testing.T) {
	tt, rec := newTestTel(t)
	tt.recordAppendEntries("node-b", nil, 10*time.Millisecond)
	tt.recordAppendEntries("node-b", errSomething, 0)
	if v := rec.CounterValue("raft_append_entries_total", metrics.Labels{"peer": "node-b", "result": "ok"}); v != 1 {
		t.Fatalf("AE ok counter: want 1, got %v", v)
	}
	if v := rec.CounterValue("raft_append_entries_total", metrics.Labels{"peer": "node-b", "result": "err"}); v != 1 {
		t.Fatalf("AE err counter: want 1, got %v", v)
	}
	if c := rec.HistogramCount("raft_append_entries_duration_seconds", metrics.Labels{"peer": "node-b", "result": "ok"}); c != 1 {
		t.Fatalf("AE duration count: want 1, got %v", c)
	}
}

var errSomething = errStub("boom")

type errStub string

func (e errStub) Error() string { return string(e) }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /opt/workspace/wippy/runtime && go test ./system/raft/ -run TestRaftTelemetry_CommitLog -v`
Expected: FAIL.

- [ ] **Step 3: Add recorders**

Append to `runtime/system/raft/telemetry.go`:

```go
func (t *telemetry) recordCommitIndex(idx uint64) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.GaugeSet("raft_commit_index", float64(idx), nil)
}

func (t *telemetry) recordLastLogIndex(node string, idx uint64) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.GaugeSet("raft_last_log_index", float64(idx), metrics.Labels{"node": node})
}

func (t *telemetry) recordLogLag(node string, lag int64) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.GaugeSet("raft_log_lag", float64(lag), metrics.Labels{"node": node})
}

func (t *telemetry) recordAppendEntries(peer string, err error, dur time.Duration) {
	if t == nil || t.coll == nil {
		return
	}
	res := raftResultLabel(err)
	t.coll.CounterInc("raft_append_entries_total", metrics.Labels{"peer": peer, "result": res})
	t.coll.HistogramObserve("raft_append_entries_duration_seconds", dur.Seconds(),
		metrics.Labels{"peer": peer, "result": res})
}
```

- [ ] **Step 4: Run tests**

Run: `cd /opt/workspace/wippy/runtime && go test ./system/raft/ -run TestRaftTelemetry_CommitLog -v`
Expected: PASS.

- [ ] **Step 5: Wire into `monitorLeadership` periodic stats**

The current `monitorLeadership` (~L218) loops on `LeaderCh()`. Hashicorp raft exposes `Stats()` returning a string map (`commit_index`, `last_log_index`, `term`, `state`). Replace the periodic body to also pull stats every 1s on a ticker:

```go
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case isLeader := <-n.raft.LeaderCh():
			// existing logic
			_ = isLeader
		case <-t.C:
			stats := n.raft.Stats()
			if v, err := strconv.ParseUint(stats["commit_index"], 10, 64); err == nil {
				n.tel.recordCommitIndex(v)
			}
			if v, err := strconv.ParseUint(stats["last_log_index"], 10, 64); err == nil {
				n.tel.recordLastLogIndex(n.localID, v)
				if cv, err := strconv.ParseUint(stats["commit_index"], 10, 64); err == nil {
					n.tel.recordLogLag(n.localID, int64(cv)-int64(v))
				}
			}
			if v, err := strconv.ParseUint(stats["term"], 10, 64); err == nil {
				n.tel.recordTerm(v)
			}
		case <-n.stopCh:
			return
		}
	}
```

- [ ] **Step 6: AppendEntries timing**

Append-entries latency on the leader side requires hooking the transport. The cleanest path: wrap `hraft.NetworkTransport.AppendEntries` via an `Interceptor` in transport options. If hashicorp/raft-NetworkTransport doesn't expose this, instrument follower-side via `Stats()["last_contact"]` per peer (gauge, not histogram) — acceptable fallback. Implement the fallback first:

```go
func (t *telemetry) recordLastContact(peer string, sinceMillis int64) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.GaugeSet("raft_last_contact_ms", float64(sinceMillis), metrics.Labels{"peer": peer})
}
```

(Add `raft_last_contact_ms` to the metric registry in the spec — this is an addition the dashboards will need; document in the dashboard JSON queries.)

For exact AE histograms (which the dashboards expect), expose a small `instrumentedTransport` wrapping `hraft.Transport`:

```go
// instrumentedTransport wraps a NetworkTransport and records AE latency.
type instrumentedTransport struct {
	hraft.Transport
	tel *telemetry
}

func (it *instrumentedTransport) AppendEntries(id hraft.ServerID, target hraft.ServerAddress,
	args *hraft.AppendEntriesRequest, resp *hraft.AppendEntriesResponse) error {
	start := time.Now()
	err := it.Transport.AppendEntries(id, target, args, resp)
	it.tel.recordAppendEntries(string(id), err, time.Since(start))
	return err
}
```

Replace the transport assignment (~L125) with:

```go
	n.transport = &instrumentedTransport{Transport: transport, tel: n.tel}
```

- [ ] **Step 7: Run tests + lint**

Run:
```bash
cd /opt/workspace/wippy/runtime && \
  go test ./system/raft/... && \
  golangci-lint run ./system/raft/...
```

- [ ] **Step 8: Commit**

```bash
cd /opt/workspace/wippy/runtime && \
  git add system/raft/telemetry.go system/raft/telemetry_test.go system/raft/raft.go && \
  git commit -m "feat(raft): emit OTel metrics for commit/log lag and AppendEntries latency"
```

---

## Task 8: Raft telemetry — voter ladder, snapshots, spans

Cover `raft_voters`, `raft_non_voters`, `raft_voter_cap`, `raft_snapshot_total`, `raft_snapshot_duration_seconds`, `raft_snapshot_size_bytes`. Add spans `raft.election`, `raft.snapshot`, `raft.append_entries`.

**Files:**
- Modify: `runtime/system/raft/telemetry.go`, `telemetry_test.go`
- Modify: `runtime/system/raft/raft.go`
- Modify: `runtime/system/raft/membership.go` (voter ladder hooks)

- [ ] **Step 1: Write failing tests**

Append to `runtime/system/raft/telemetry_test.go`:

```go
func TestRaftTelemetry_VoterLadder(t *testing.T) {
	tt, rec := newTestTel(t)
	tt.recordVoterLadder(3, 2, 5)
	if v := rec.GaugeValue("raft_voters", nil); v != 3 {
		t.Fatalf("raft_voters: want 3, got %v", v)
	}
	if v := rec.GaugeValue("raft_non_voters", nil); v != 2 {
		t.Fatalf("raft_non_voters: want 2, got %v", v)
	}
	if v := rec.GaugeValue("raft_voter_cap", nil); v != 5 {
		t.Fatalf("raft_voter_cap: want 5, got %v", v)
	}
}

func TestRaftTelemetry_Snapshot(t *testing.T) {
	tt, rec := newTestTel(t)
	tt.recordSnapshot(nil, 100*time.Millisecond, 4096)
	if v := rec.CounterValue("raft_snapshot_total", metrics.Labels{"result": "ok"}); v != 1 {
		t.Fatalf("raft_snapshot_total: want 1, got %v", v)
	}
	if c := rec.HistogramCount("raft_snapshot_duration_seconds", nil); c != 1 {
		t.Fatalf("raft_snapshot_duration_seconds: want 1, got %v", c)
	}
	if c := rec.HistogramCount("raft_snapshot_size_bytes", nil); c != 1 {
		t.Fatalf("raft_snapshot_size_bytes: want 1, got %v", c)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /opt/workspace/wippy/runtime && go test ./system/raft/ -run TestRaftTelemetry_VoterLadder -v`
Expected: FAIL.

- [ ] **Step 3: Implement recorders**

Append to `runtime/system/raft/telemetry.go`:

```go
func (t *telemetry) recordVoterLadder(voters, nonVoters, cap int) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.GaugeSet("raft_voters", float64(voters), nil)
	t.coll.GaugeSet("raft_non_voters", float64(nonVoters), nil)
	t.coll.GaugeSet("raft_voter_cap", float64(cap), nil)
}

func (t *telemetry) recordSnapshot(err error, dur time.Duration, sizeBytes int64) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("raft_snapshot_total", metrics.Labels{"result": raftResultLabel(err)})
	t.coll.HistogramObserve("raft_snapshot_duration_seconds", dur.Seconds(), nil)
	t.coll.HistogramObserve("raft_snapshot_size_bytes", float64(sizeBytes), nil)
}
```

- [ ] **Step 4: Wire voter ladder**

In `runtime/system/raft/membership.go`, locate the function applying voter / non-voter changes (commit `92044648` introduced voter cap + non-voter overflow). On every successful `AddVoter`/`AddNonvoter`/`DemoteVoter`/`RemoveServer`, emit:

```go
	servers, err := n.GetConfiguration()
	if err == nil {
		voters, nonVoters := 0, 0
		for _, s := range servers {
			if s.Suffrage == raftapi.Voter {
				voters++
			} else {
				nonVoters++
			}
		}
		n.tel.recordVoterLadder(voters, nonVoters, n.config.VoterCap)
	}
```

- [ ] **Step 5: Wire snapshot**

In `runtime/system/raft/raft.go`, hook the snapshot path. Hashicorp raft snapshots are owned by the FSM. Wrap the FSM with `instrumentedFSM`:

```go
type instrumentedFSM struct {
	hraft.FSM
	tel *telemetry
}

func (i *instrumentedFSM) Snapshot() (hraft.FSMSnapshot, error) {
	start := time.Now()
	snap, err := i.FSM.Snapshot()
	if err != nil {
		i.tel.recordSnapshot(err, time.Since(start), 0)
		return nil, err
	}
	return &instrumentedFSMSnapshot{FSMSnapshot: snap, tel: i.tel, start: start}, nil
}

type instrumentedFSMSnapshot struct {
	hraft.FSMSnapshot
	tel   *telemetry
	start time.Time
}

func (s *instrumentedFSMSnapshot) Persist(sink hraft.SnapshotSink) error {
	cw := &countingSink{SnapshotSink: sink}
	err := s.FSMSnapshot.Persist(cw)
	s.tel.recordSnapshot(err, time.Since(s.start), cw.bytes)
	return err
}

type countingSink struct {
	hraft.SnapshotSink
	bytes int64
}

func (c *countingSink) Write(p []byte) (int, error) {
	n, err := c.SnapshotSink.Write(p)
	c.bytes += int64(n)
	return n, err
}
```

In `Start()` (~L129), where `hraft.NewRaft(rc, n.fsm, ...)` is called, wrap:

```go
	wrappedFSM := &instrumentedFSM{FSM: n.fsm, tel: n.tel}
	r, err := hraft.NewRaft(rc, wrappedFSM, n.logStore, n.stableStore, n.snapStore, n.transport)
```

- [ ] **Step 6: Add spans for raft operations**

Around the snapshot wrapper:

```go
func (s *instrumentedFSMSnapshot) Persist(sink hraft.SnapshotSink) error {
	_, span := s.tel.startSpan(context.Background(), "raft.snapshot")
	defer span.End()
	cw := &countingSink{SnapshotSink: sink}
	err := s.FSMSnapshot.Persist(cw)
	span.SetAttributes(attribute.Int64("raft.snapshot.bytes", cw.bytes))
	s.tel.setSpanError(span, err)
	s.tel.recordSnapshot(err, time.Since(s.start), cw.bytes)
	return err
}
```

In `instrumentedTransport.AppendEntries`, wrap with span `raft.append_entries`:

```go
func (it *instrumentedTransport) AppendEntries(id hraft.ServerID, target hraft.ServerAddress,
	args *hraft.AppendEntriesRequest, resp *hraft.AppendEntriesResponse) error {
	_, span := it.tel.startSpan(context.Background(), "raft.append_entries",
		trace.WithAttributes(
			attribute.String("peer", string(id)),
			attribute.Int("entries", len(args.Entries)),
		),
	)
	defer span.End()
	start := time.Now()
	err := it.Transport.AppendEntries(id, target, args, resp)
	it.tel.setSpanError(span, err)
	it.tel.recordAppendEntries(string(id), err, time.Since(start))
	return err
}
```

Add imports as needed:

```go
	"context"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
```

- [ ] **Step 7: Run tests + lint**

Run:
```bash
cd /opt/workspace/wippy/runtime && \
  go test ./system/raft/... && \
  golangci-lint run ./system/raft/...
```

- [ ] **Step 8: Commit**

```bash
cd /opt/workspace/wippy/runtime && \
  git add system/raft/telemetry.go system/raft/telemetry_test.go system/raft/raft.go system/raft/membership.go && \
  git commit -m "feat(raft): emit OTel metrics+spans for voter ladder and snapshots"
```

---

## Task 9: Gossip telemetry — member states + messages

Cover `gossip_members`, `gossip_join_total`, `gossip_leave_total`, `gossip_message_total`, `gossip_message_bytes`.

**Files:**
- Create: `runtime/cluster/membership/telemetry.go`
- Create: `runtime/cluster/membership/telemetry_test.go`
- Modify: `runtime/cluster/membership/membership.go` (struct, constructor, eventDelegate hooks, delegate.NotifyMsg)

- [ ] **Step 1: Write failing tests**

`runtime/cluster/membership/telemetry_test.go`:

```go
// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"testing"
	"time"

	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/internal/telemetrytest"
)

func newTestTel(t *testing.T) (*telemetry, *telemetrytest.Recorder) {
	t.Helper()
	rec := telemetrytest.NewRecorder()
	return newTelemetry(rec, nil, nil), rec
}

func TestGossipTelemetry_Members(t *testing.T) {
	tt, rec := newTestTel(t)
	tt.recordMembers("alive", 5)
	tt.recordMembers("suspect", 1)
	tt.recordMembers("dead", 0)
	if v := rec.GaugeValue("gossip_members", metrics.Labels{"state": "alive"}); v != 5 {
		t.Fatalf("alive: want 5, got %v", v)
	}
	if v := rec.GaugeValue("gossip_members", metrics.Labels{"state": "suspect"}); v != 1 {
		t.Fatalf("suspect: want 1, got %v", v)
	}
}

func TestGossipTelemetry_JoinLeave(t *testing.T) {
	tt, rec := newTestTel(t)
	tt.recordJoin(nil)
	tt.recordJoin(errSomething)
	tt.recordLeave()
	if v := rec.CounterValue("gossip_join_total", metrics.Labels{"result": "ok"}); v != 1 {
		t.Fatalf("join ok: want 1, got %v", v)
	}
	if v := rec.CounterValue("gossip_join_total", metrics.Labels{"result": "err"}); v != 1 {
		t.Fatalf("join err: want 1, got %v", v)
	}
	if v := rec.CounterValue("gossip_leave_total", nil); v != 1 {
		t.Fatalf("leave: want 1, got %v", v)
	}
}

func TestGossipTelemetry_Message(t *testing.T) {
	tt, rec := newTestTel(t)
	tt.recordMessage("ping", "tx", 64)
	tt.recordMessage("ping", "rx", 64)
	if v := rec.CounterValue("gossip_message_total", metrics.Labels{"kind": "ping", "direction": "tx"}); v != 1 {
		t.Fatalf("tx: want 1, got %v", v)
	}
	if c := rec.HistogramCount("gossip_message_bytes", metrics.Labels{"kind": "ping", "direction": "tx"}); c != 1 {
		t.Fatalf("bytes obs: want 1, got %v", c)
	}
}

func TestGossipTelemetry_NilSafe(t *testing.T) {
	tt := newTelemetry(nil, nil, nil)
	tt.recordMembers("alive", 1)
	tt.recordJoin(nil)
	tt.recordLeave()
	tt.recordMessage("ping", "tx", 0)
	_ = time.Now
}

var errSomething = errStub("boom")

type errStub string

func (e errStub) Error() string { return string(e) }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /opt/workspace/wippy/runtime && go test ./cluster/membership/ -run TestGossipTelemetry -v`
Expected: FAIL.

- [ ] **Step 3: Implement `telemetry.go`**

`runtime/cluster/membership/telemetry.go`:

```go
// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"context"
	"time"

	"github.com/wippyai/runtime/api/metrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type telemetry struct {
	coll   metrics.Collector
	tracer trace.Tracer
}

func newTelemetry(coll metrics.Collector, mp otelmetric.MeterProvider, tp trace.TracerProvider) *telemetry {
	if tp == nil {
		tp = otel.GetTracerProvider()
	}
	return &telemetry{coll: coll, tracer: tp.Tracer("wippy-runtime")}
}

func gossipResult(err error) string {
	if err != nil {
		return "err"
	}
	return "ok"
}

func (t *telemetry) recordMembers(state string, n int) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.GaugeSet("gossip_members", float64(n), metrics.Labels{"state": state})
}

func (t *telemetry) recordJoin(err error) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("gossip_join_total", metrics.Labels{"result": gossipResult(err)})
}

func (t *telemetry) recordLeave() {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("gossip_leave_total", nil)
}

func (t *telemetry) recordMessage(kind, direction string, bytes int) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("gossip_message_total", metrics.Labels{"kind": kind, "direction": direction})
	if bytes > 0 {
		t.coll.HistogramObserve("gossip_message_bytes", float64(bytes),
			metrics.Labels{"kind": kind, "direction": direction})
	}
}

func (t *telemetry) startSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if t == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return t.tracer.Start(ctx, name, opts...)
}

func (t *telemetry) setSpanError(span trace.Span, err error) {
	if err == nil || span == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

var _ = time.Now // appeased
```

- [ ] **Step 4: Wire into `Service`**

Modify `runtime/cluster/membership/membership.go`:

(a) Add field on `Service` struct (~L32):

```go
	tel   *telemetry
```

(b) Update `NewService` and `New` to accept telemetry deps. For `New`, add an `Option`:

```go
func WithTelemetry(coll metrics.Collector, mp otelmetric.MeterProvider, tp trace.TracerProvider) Option {
	return func(o *options) {
		o.coll = coll
		o.mp = mp
		o.tp = tp
	}
}
```

Update `options` struct in `runtime/cluster/membership/options.go` with `coll`, `mp`, `tp` fields. In `New`, after constructing the Service:

```go
	svc.tel = newTelemetry(o.coll, o.mp, o.tp)
```

For `NewService`, add three trailing parameters (`coll`, `mp`, `tp`) and call `newTelemetry`. Update all callers.

(c) Hook into eventDelegate (`NotifyJoin` ~L313, `NotifyLeave` ~L337, `NotifyUpdate` ~L360). After each notify, recompute member counts:

```go
	ed.service.refreshMemberStateGauges()
```

Add the helper:

```go
func (s *Service) refreshMemberStateGauges() {
	if s.memberlist == nil {
		return
	}
	alive, suspect, dead, left := 0, 0, 0, 0
	for _, m := range s.memberlist.Members() {
		switch m.State {
		case memberlist.StateAlive:
			alive++
		case memberlist.StateSuspect:
			suspect++
		case memberlist.StateDead:
			dead++
		case memberlist.StateLeft:
			left++
		}
	}
	s.tel.recordMembers("alive", alive)
	s.tel.recordMembers("suspect", suspect)
	s.tel.recordMembers("dead", dead)
	s.tel.recordMembers("left", left)
}
```

(d) Hook `delegate.NotifyMsg` (~L428) to count incoming application messages:

```go
func (d *delegate) NotifyMsg(data []byte) {
	d.service.tel.recordMessage("user", "rx", len(data))
}
```

(e) Hook `delegate.GetBroadcasts` (~L435) to count outgoing:

```go
func (d *delegate) GetBroadcasts(overhead, limit int) [][]byte {
	out := [][]byte(nil) // existing logic returns this
	for _, b := range out {
		d.service.tel.recordMessage("broadcast", "tx", len(b))
	}
	return out
}
```

(f) Hook `Start` and `Stop`. In `Start` (~L86), at the end, after `Join` succeeds: `s.tel.recordJoin(nil)`. On error: `s.tel.recordJoin(err)`. In `Stop` (~L172): `s.tel.recordLeave()`.

- [ ] **Step 5: Run tests + lint**

Run:
```bash
cd /opt/workspace/wippy/runtime && \
  go test ./cluster/membership/... && \
  golangci-lint run ./cluster/membership/...
```

- [ ] **Step 6: Commit**

```bash
cd /opt/workspace/wippy/runtime && \
  git add cluster/membership/telemetry.go cluster/membership/telemetry_test.go \
          cluster/membership/membership.go cluster/membership/options.go && \
  git commit -m "feat(gossip): emit OTel metrics for members, joins, messages"
```

---

## Task 10: Gossip telemetry — probes, suspicion, convergence, spans

Cover `gossip_probe_duration_seconds`, `gossip_probe_failures_total`, `gossip_suspicion_resolutions_total`, `gossip_convergence_seconds`, plus spans `gossip.probe`, `gossip.sync`, `gossip.broadcast`.

**Files:**
- Modify: `runtime/cluster/membership/telemetry.go`, `telemetry_test.go`
- Modify: `runtime/cluster/membership/membership.go`

- [ ] **Step 1: Write failing tests**

Append to `telemetry_test.go`:

```go
func TestGossipTelemetry_Probe(t *testing.T) {
	tt, rec := newTestTel(t)
	tt.recordProbe(nil, 5*time.Millisecond)
	tt.recordProbe(errSomething, 0)
	tt.recordProbeFailure("node-x")
	if c := rec.HistogramCount("gossip_probe_duration_seconds", metrics.Labels{"result": "ok"}); c != 1 {
		t.Fatalf("probe ok hist: want 1, got %v", c)
	}
	if v := rec.CounterValue("gossip_probe_failures_total", metrics.Labels{"target": "node-x"}); v != 1 {
		t.Fatalf("probe failures: want 1, got %v", v)
	}
}

func TestGossipTelemetry_SuspicionAndConvergence(t *testing.T) {
	tt, rec := newTestTel(t)
	tt.recordSuspicionOutcome("alive")
	tt.recordSuspicionOutcome("dead")
	tt.recordConvergence(2 * time.Second)
	if v := rec.CounterValue("gossip_suspicion_resolutions_total", metrics.Labels{"outcome": "alive"}); v != 1 {
		t.Fatalf("suspicion alive: want 1, got %v", v)
	}
	if v := rec.CounterValue("gossip_suspicion_resolutions_total", metrics.Labels{"outcome": "dead"}); v != 1 {
		t.Fatalf("suspicion dead: want 1, got %v", v)
	}
	if c := rec.HistogramCount("gossip_convergence_seconds", nil); c != 1 {
		t.Fatalf("convergence hist: want 1, got %v", c)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /opt/workspace/wippy/runtime && go test ./cluster/membership/ -run TestGossipTelemetry_Probe -v`
Expected: FAIL.

- [ ] **Step 3: Implement recorders**

Append to `telemetry.go`:

```go
func (t *telemetry) recordProbe(err error, dur time.Duration) {
	if t == nil || t.coll == nil {
		return
	}
	res := gossipResult(err)
	t.coll.HistogramObserve("gossip_probe_duration_seconds", dur.Seconds(),
		metrics.Labels{"result": res})
}

func (t *telemetry) recordProbeFailure(target string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("gossip_probe_failures_total", metrics.Labels{"target": target})
}

func (t *telemetry) recordSuspicionOutcome(outcome string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("gossip_suspicion_resolutions_total", metrics.Labels{"outcome": outcome})
}

func (t *telemetry) recordConvergence(dur time.Duration) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.HistogramObserve("gossip_convergence_seconds", dur.Seconds(), nil)
}
```

- [ ] **Step 4: Wire suspicion outcomes**

In `membership.go` `eventDelegate.NotifyUpdate` (~L360) — track each node's previous state in `s.nodes` and detect transitions:

```go
func (ed *eventDelegate) NotifyUpdate(node *memberlist.Node) {
	prev, _ := ed.service.nodes[node.Name]
	// existing parse + assign of new state...
	switch {
	case prev.State == "suspect" && newState == "alive":
		ed.service.tel.recordSuspicionOutcome("alive")
	case prev.State == "suspect" && newState == "dead":
		ed.service.tel.recordSuspicionOutcome("dead")
	}
}
```

(Adapt to actual struct field name; `cluster.NodeInfo` exposes a `State` string in the existing code — verify by reading the package.)

- [ ] **Step 5: Wire probe + convergence**

memberlist does not expose probe RTT directly via callback. Two pragmatic options:
1. **Periodic snapshot every 1s** of `memberlist.Health()` (a 0..N score, 0 = healthy) and log it as a gauge `gossip_health_score` — added in this task as a bonus signal.
2. For convergence, on every `NotifyJoin`/`NotifyLeave`, record the time delta from the original cluster state change (best-effort: use `time.Since(membership.lastChangeAt)` where `lastChangeAt` is updated whenever `s.nodes` mutates).

Implementation in `membership.go`:

```go
type Service struct {
	// ... existing fields ...
	tel             *telemetry
	lastChangeAt    time.Time
	healthTickerStop chan struct{}
}

func (s *Service) emitHealthLoop(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if s.memberlist != nil {
				score := s.memberlist.GetHealthScore()
				s.tel.recordProbe(nil, time.Duration(score)*time.Millisecond) // proxy: 0 = healthy
			}
		}
	}
}
```

In `Start`, after Join: `go s.emitHealthLoop(s.ctx)`.

In every place that mutates `s.nodes`, record convergence:

```go
	if !s.lastChangeAt.IsZero() {
		s.tel.recordConvergence(time.Since(s.lastChangeAt))
	}
	s.lastChangeAt = time.Now()
```

- [ ] **Step 6: Add spans for probe + sync + broadcast**

Around the user-message receive path in `delegate.NotifyMsg`:

```go
func (d *delegate) NotifyMsg(data []byte) {
	_, span := d.service.tel.startSpan(d.service.ctx, "gossip.broadcast",
		trace.WithAttributes(attribute.Int("bytes", len(data))),
	)
	defer span.End()
	d.service.tel.recordMessage("user", "rx", len(data))
}
```

Around `MergeRemoteState`:

```go
func (d *delegate) MergeRemoteState(buf []byte, join bool) {
	_, span := d.service.tel.startSpan(d.service.ctx, "gossip.sync",
		trace.WithAttributes(
			attribute.Int("bytes", len(buf)),
			attribute.Bool("join", join),
		),
	)
	defer span.End()
}
```

Imports:

```go
	"go.opentelemetry.io/otel/attribute"
```

- [ ] **Step 7: Run tests + lint**

Run:
```bash
cd /opt/workspace/wippy/runtime && \
  go test ./cluster/membership/... && \
  golangci-lint run ./cluster/membership/...
```

- [ ] **Step 8: Run full runtime suite**

Run:
```bash
cd /opt/workspace/wippy/runtime && \
  go test ./... && \
  golangci-lint run ./...
```
Expected: clean. Fix any callers that didn't update `NewService`/`NewNode`/`NewService` signatures.

- [ ] **Step 9: Commit**

```bash
cd /opt/workspace/wippy/runtime && \
  git add cluster/membership/telemetry.go cluster/membership/telemetry_test.go cluster/membership/membership.go && \
  git commit -m "feat(gossip): emit OTel metrics+spans for probes, suspicion, convergence"
```

---

## Task 11: Cleanup old monkey/ generators and overlapping ConfigMaps

**Files:**
- Delete: `monkey/generate_runtime_dashboards.py`
- Delete: `monkey/generate_dashboards.py`
- Delete: `monkey/generate_dashboards_simple.py`
- Delete: `monkey/manifests/observability/grafana-dashboards.json`
- Delete: `monkey/manifests/observability/grafana-extra-dashboards.yaml`
- Delete: `monkey/manifests/observability/grafana-complete.yaml`
- Delete: `monkey/manifests/observability/prometheus-advanced-dashboard.yaml`

- [ ] **Step 1: Confirm files exist**

Run:
```bash
ls -la \
  /opt/workspace/wippy/monkey/generate_runtime_dashboards.py \
  /opt/workspace/wippy/monkey/generate_dashboards.py \
  /opt/workspace/wippy/monkey/generate_dashboards_simple.py \
  /opt/workspace/wippy/monkey/manifests/observability/grafana-dashboards.json \
  /opt/workspace/wippy/monkey/manifests/observability/grafana-extra-dashboards.yaml \
  /opt/workspace/wippy/monkey/manifests/observability/grafana-complete.yaml \
  /opt/workspace/wippy/monkey/manifests/observability/prometheus-advanced-dashboard.yaml
```
Expected: all listed.

- [ ] **Step 2: Confirm nothing else references them**

Run:
```bash
cd /opt/workspace/wippy/monkey && \
  grep -RIn "generate_runtime_dashboards\|generate_dashboards\.py\|generate_dashboards_simple\|grafana-dashboards.json\|grafana-extra-dashboards\|grafana-complete\|prometheus-advanced-dashboard" \
  --exclude-dir=node_modules --exclude-dir=.git
```
Expected: empty output (no live references). If there are kustomization or Makefile references, capture them — they'll need updating in Step 3.

- [ ] **Step 3: Update any callers**

If `grep` from Step 2 found references in `monkey/Makefile`, `monkey/scripts/*.sh`, or `monkey/manifests/observability/kustomization.yaml`, edit them in-place to remove the references. The `kustomization.yaml` (if present) gets a new `resources:` entry added in Task 12.

- [ ] **Step 4: Delete files**

Run:
```bash
rm \
  /opt/workspace/wippy/monkey/generate_runtime_dashboards.py \
  /opt/workspace/wippy/monkey/generate_dashboards.py \
  /opt/workspace/wippy/monkey/generate_dashboards_simple.py \
  /opt/workspace/wippy/monkey/manifests/observability/grafana-dashboards.json \
  /opt/workspace/wippy/monkey/manifests/observability/grafana-extra-dashboards.yaml \
  /opt/workspace/wippy/monkey/manifests/observability/grafana-complete.yaml \
  /opt/workspace/wippy/monkey/manifests/observability/prometheus-advanced-dashboard.yaml
```

- [ ] **Step 5: Verify gone**

Run:
```bash
ls /opt/workspace/wippy/monkey/manifests/observability/
```
Expected: `dashboards/` (created in Task 12), `grafana-dashboards-configmap.yaml` (created in Task 12), `otel-collector.yaml`. Nothing else from the deleted set.

> No commit step here — `monkey/` is not git-tracked locally. If user later initializes / mirrors to a remote, all monkey changes go in one commit.

---

## Task 12: New ConfigMap + Dashboard 01 (PG Operations Overview)

The ConfigMap mounts every JSON in `dashboards/` via a Grafana sidecar's auto-discovery (`grafana_dashboard: "1"` label).

**Files:**
- Create: `monkey/manifests/observability/grafana-dashboards-configmap.yaml`
- Create: `monkey/manifests/observability/dashboards/01-pg-operations-overview.json`

- [ ] **Step 1: Create the dashboards directory**

Run:
```bash
mkdir -p /opt/workspace/wippy/monkey/manifests/observability/dashboards
```

- [ ] **Step 2: Write the ConfigMap**

`monkey/manifests/observability/grafana-dashboards-configmap.yaml`:

```yaml
# SPDX-License-Identifier: MPL-2.0
#
# Single ConfigMap containing all 20 PG/Raft/Gossip dashboards. Grafana's
# dashboard sidecar picks these up via the grafana_dashboard label.
#
# Regenerate the data: section with:
#   for f in monkey/manifests/observability/dashboards/*.json; do
#     name=$(basename "$f")
#     printf '  %s: |\n' "$name"
#     sed 's/^/    /' "$f"
#   done
#
apiVersion: v1
kind: ConfigMap
metadata:
  name: grafana-dashboards-pg-observability
  namespace: observability
  labels:
    grafana_dashboard: "1"
    app: grafana
data:
  01-pg-operations-overview.json: |
    {{< inline contents of dashboards/01-pg-operations-overview.json >}}
  02-pg-queue-backpressure.json: |
    {{< inline contents of dashboards/02-pg-queue-backpressure.json >}}
  # ... entries 03–20 added by Tasks 13–17 ...
```

(The CI/Make step that materializes this ConfigMap — embedding each JSON file as a `data:` entry — is added in Task 18; until then, the `{{< inline … >}}` placeholders mark which files to inline.)

- [ ] **Step 3: Write Dashboard 01 — pg-operations-overview**

`monkey/manifests/observability/dashboards/01-pg-operations-overview.json`:

```json
{
  "title": "PG Operations Overview",
  "uid": "wippy-pg-01",
  "tags": ["pg", "wippy", "operations"],
  "timezone": "browser",
  "refresh": "5s",
  "time": {"from": "now-15m", "to": "now"},
  "schemaVersion": 38,
  "version": 1,
  "panels": [
    {
      "id": 1,
      "title": "Ops/sec by op",
      "type": "timeseries",
      "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
      "targets": [
        {"expr": "sum by (op) (rate(pg_op_duration_seconds_count[1m]))", "legendFormat": "{{op}}"}
      ],
      "gridPos": {"h": 8, "w": 12, "x": 0, "y": 0}
    },
    {
      "id": 2,
      "title": "Success rate %",
      "type": "stat",
      "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
      "targets": [
        {"expr": "100 * sum(rate(pg_join_total{result=\"ok\"}[5m]) + rate(pg_leave_total{result=\"ok\"}[5m]) + rate(pg_broadcast_total{result=\"ok\"}[5m])) / sum(rate(pg_join_total[5m]) + rate(pg_leave_total[5m]) + rate(pg_broadcast_total[5m]))", "legendFormat": "success %"}
      ],
      "fieldConfig": {"defaults": {"unit": "percent", "thresholds": {"mode": "absolute", "steps": [{"color": "red", "value": 0}, {"color": "yellow", "value": 95}, {"color": "green", "value": 99}]}}},
      "gridPos": {"h": 8, "w": 6, "x": 12, "y": 0}
    },
    {
      "id": 3,
      "title": "P99 op duration",
      "type": "stat",
      "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
      "targets": [
        {"expr": "histogram_quantile(0.99, sum by (le) (rate(pg_op_duration_seconds_bucket[5m])))", "legendFormat": "p99"}
      ],
      "fieldConfig": {"defaults": {"unit": "s"}},
      "gridPos": {"h": 8, "w": 6, "x": 18, "y": 0}
    },
    {
      "id": 4,
      "title": "Latency p50/p95/p99 by op",
      "type": "timeseries",
      "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
      "targets": [
        {"expr": "histogram_quantile(0.50, sum by (le, op) (rate(pg_op_duration_seconds_bucket[1m])))", "legendFormat": "p50 {{op}}"},
        {"expr": "histogram_quantile(0.95, sum by (le, op) (rate(pg_op_duration_seconds_bucket[1m])))", "legendFormat": "p95 {{op}}"},
        {"expr": "histogram_quantile(0.99, sum by (le, op) (rate(pg_op_duration_seconds_bucket[1m])))", "legendFormat": "p99 {{op}}"}
      ],
      "fieldConfig": {"defaults": {"unit": "s"}},
      "gridPos": {"h": 9, "w": 24, "x": 0, "y": 8}
    },
    {
      "id": 5,
      "title": "Top groups by ops/sec",
      "type": "table",
      "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
      "targets": [
        {"expr": "topk(10, sum by (pg) (rate(pg_op_duration_seconds_count[5m])))", "legendFormat": "{{pg}}", "format": "table", "instant": true}
      ],
      "gridPos": {"h": 9, "w": 24, "x": 0, "y": 17}
    }
  ]
}
```

- [ ] **Step 4: Smoke-validate the JSON**

Run:
```bash
python3 -c 'import json,sys; json.load(open("/opt/workspace/wippy/monkey/manifests/observability/dashboards/01-pg-operations-overview.json"))' && echo "valid JSON"
```
Expected: `valid JSON`.

- [ ] **Step 5: Pause point**

> No commit (monkey/ not git-tracked locally).

---

## Task 13: PG Dashboards 02–06

For each, create the JSON file. Same Prometheus datasource UID, same overall structure as Dashboard 01.

**Files:**
- Create: `monkey/manifests/observability/dashboards/02-pg-queue-backpressure.json`
- Create: `monkey/manifests/observability/dashboards/03-pg-circuit-breaker.json`
- Create: `monkey/manifests/observability/dashboards/04-pg-retry-storms.json`
- Create: `monkey/manifests/observability/dashboards/05-pg-dispatcher.json`
- Create: `monkey/manifests/observability/dashboards/06-pg-fence-tokens-globalreg.json`

- [ ] **Step 1: 02-pg-queue-backpressure.json**

```json
{
  "title": "PG Queue & Backpressure",
  "uid": "wippy-pg-02",
  "tags": ["pg", "wippy", "queue"],
  "timezone": "browser",
  "refresh": "5s",
  "time": {"from": "now-15m", "to": "now"},
  "schemaVersion": 38,
  "version": 1,
  "panels": [
    {"id": 1, "title": "Queue depth (max across groups)", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "max(pg_queue_depth)"}], "gridPos": {"h": 7, "w": 6, "x": 0, "y": 0}},
    {"id": 2, "title": "Drops/sec", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(rate(pg_queue_dropped_total[1m]))"}], "fieldConfig": {"defaults": {"thresholds": {"mode": "absolute", "steps": [{"color": "green", "value": 0}, {"color": "yellow", "value": 1}, {"color": "red", "value": 10}]}}},
     "gridPos": {"h": 7, "w": 6, "x": 6, "y": 0}},
    {"id": 3, "title": "Drop ratio %", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "100 * sum(rate(pg_queue_dropped_total[5m])) / clamp_min(sum(rate(pg_op_duration_seconds_count[5m])), 1)"}],
     "fieldConfig": {"defaults": {"unit": "percent"}}, "gridPos": {"h": 7, "w": 6, "x": 12, "y": 0}},
    {"id": 4, "title": "Drops by reason", "type": "piechart", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum by (reason) (increase(pg_queue_dropped_total[15m]))", "legendFormat": "{{reason}}"}],
     "gridPos": {"h": 7, "w": 6, "x": 18, "y": 0}},
    {"id": 5, "title": "Queue depth per group", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "pg_queue_depth", "legendFormat": "{{pg}}"}], "gridPos": {"h": 9, "w": 24, "x": 0, "y": 7}},
    {"id": 6, "title": "Drops over time by reason", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum by (reason) (rate(pg_queue_dropped_total[1m]))", "legendFormat": "{{reason}}"}],
     "gridPos": {"h": 9, "w": 24, "x": 0, "y": 16}}
  ]
}
```

- [ ] **Step 2: 03-pg-circuit-breaker.json**

```json
{
  "title": "PG Circuit Breaker",
  "uid": "wippy-pg-03",
  "tags": ["pg", "wippy", "resilience"],
  "timezone": "browser",
  "refresh": "5s",
  "time": {"from": "now-15m", "to": "now"},
  "schemaVersion": 38, "version": 1,
  "panels": [
    {"id": 1, "title": "Trips (15m)", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(increase(pg_circuit_breaker_trips_total[15m]))"}], "gridPos": {"h": 7, "w": 6, "x": 0, "y": 0}},
    {"id": 2, "title": "Open breakers", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "count(pg_circuit_breaker_state == 2)"}],
     "fieldConfig": {"defaults": {"thresholds": {"mode": "absolute", "steps": [{"color": "green", "value": 0}, {"color": "red", "value": 1}]}}},
     "gridPos": {"h": 7, "w": 6, "x": 6, "y": 0}},
    {"id": 3, "title": "Half-open breakers", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "count(pg_circuit_breaker_state == 1)"}], "gridPos": {"h": 7, "w": 6, "x": 12, "y": 0}},
    {"id": 4, "title": "Total groups tracked", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "count(pg_circuit_breaker_state)"}], "gridPos": {"h": 7, "w": 6, "x": 18, "y": 0}},
    {"id": 5, "title": "Breaker state per group", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "pg_circuit_breaker_state", "legendFormat": "{{pg}}"}],
     "fieldConfig": {"defaults": {"min": 0, "max": 2, "mappings": [{"type": "value", "options": {"0": {"text": "closed"}, "1": {"text": "half-open"}, "2": {"text": "open"}}}]}},
     "gridPos": {"h": 9, "w": 12, "x": 0, "y": 7}},
    {"id": 6, "title": "Trips heatmap by group", "type": "heatmap", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum by (pg) (increase(pg_circuit_breaker_trips_total[1m]))", "format": "heatmap", "legendFormat": "{{pg}}"}],
     "gridPos": {"h": 9, "w": 12, "x": 12, "y": 7}}
  ]
}
```

- [ ] **Step 3: 04-pg-retry-storms.json**

```json
{
  "title": "PG Retry Storms",
  "uid": "wippy-pg-04",
  "tags": ["pg", "wippy", "retry"],
  "timezone": "browser",
  "refresh": "5s",
  "time": {"from": "now-15m", "to": "now"},
  "schemaVersion": 38, "version": 1,
  "panels": [
    {"id": 1, "title": "Retries/sec", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(rate(pg_retry_total[1m]))"}], "gridPos": {"h": 7, "w": 6, "x": 0, "y": 0}},
    {"id": 2, "title": "Giveup rate/sec", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(rate(pg_retry_giveup_total[1m]))"}],
     "fieldConfig": {"defaults": {"thresholds": {"mode": "absolute", "steps": [{"color": "green", "value": 0}, {"color": "red", "value": 1}]}}},
     "gridPos": {"h": 7, "w": 6, "x": 6, "y": 0}},
    {"id": 3, "title": "Retry/op storm ratio", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(rate(pg_retry_total[5m])) / clamp_min(sum(rate(pg_op_duration_seconds_count[5m])), 1)"}],
     "fieldConfig": {"defaults": {"thresholds": {"mode": "absolute", "steps": [{"color": "green", "value": 0}, {"color": "yellow", "value": 0.5}, {"color": "red", "value": 1.0}]}}},
     "gridPos": {"h": 7, "w": 12, "x": 12, "y": 0}},
    {"id": 4, "title": "Retries/sec by op", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum by (op) (rate(pg_retry_total[1m]))", "legendFormat": "{{op}}"}],
     "gridPos": {"h": 9, "w": 12, "x": 0, "y": 7}},
    {"id": 5, "title": "Attempt distribution", "type": "barchart", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum by (attempt) (increase(pg_retry_total[15m]))", "legendFormat": "attempt {{attempt}}"}],
     "gridPos": {"h": 9, "w": 12, "x": 12, "y": 7}},
    {"id": 6, "title": "Giveups by op", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum by (op) (rate(pg_retry_giveup_total[1m]))", "legendFormat": "{{op}}"}],
     "gridPos": {"h": 9, "w": 24, "x": 0, "y": 16}}
  ]
}
```

- [ ] **Step 4: 05-pg-dispatcher.json**

```json
{
  "title": "PG Dispatcher",
  "uid": "wippy-pg-05",
  "tags": ["pg", "wippy", "dispatcher"],
  "timezone": "browser", "refresh": "5s", "time": {"from": "now-15m", "to": "now"}, "schemaVersion": 38, "version": 1,
  "panels": [
    {"id": 1, "title": "In-flight (max)", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "max(pg_dispatcher_inflight)"}], "gridPos": {"h": 7, "w": 8, "x": 0, "y": 0}},
    {"id": 2, "title": "Broadcasts/sec", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(rate(pg_broadcast_total[1m]))"}], "gridPos": {"h": 7, "w": 8, "x": 8, "y": 0}},
    {"id": 3, "title": "P99 broadcast latency", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "histogram_quantile(0.99, sum by (le) (rate(pg_op_duration_seconds_bucket{op=\"broadcast\"}[5m])))"}],
     "fieldConfig": {"defaults": {"unit": "s"}}, "gridPos": {"h": 7, "w": 8, "x": 16, "y": 0}},
    {"id": 4, "title": "In-flight per group", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "pg_dispatcher_inflight", "legendFormat": "{{pg}}"}], "gridPos": {"h": 9, "w": 12, "x": 0, "y": 7}},
    {"id": 5, "title": "Fan-out distribution (recipients)", "type": "heatmap", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum by (le) (increase(pg_broadcast_recipients_bucket[5m]))", "format": "heatmap"}],
     "gridPos": {"h": 9, "w": 12, "x": 12, "y": 7}},
    {"id": 6, "title": "Broadcast latency by group (p99)", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "histogram_quantile(0.99, sum by (le, pg) (rate(pg_op_duration_seconds_bucket{op=\"broadcast\"}[1m])))", "legendFormat": "{{pg}}"}],
     "fieldConfig": {"defaults": {"unit": "s"}}, "gridPos": {"h": 9, "w": 24, "x": 0, "y": 16}}
  ]
}
```

- [ ] **Step 5: 06-pg-fence-tokens-globalreg.json**

```json
{
  "title": "PG Fence Tokens & Global Registry",
  "uid": "wippy-pg-06",
  "tags": ["pg", "wippy", "fence", "globalreg"],
  "timezone": "browser", "refresh": "5s", "time": {"from": "now-15m", "to": "now"}, "schemaVersion": 38, "version": 1,
  "panels": [
    {"id": 1, "title": "Globalreg size", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "max(pg_globalreg_size)"}], "gridPos": {"h": 7, "w": 6, "x": 0, "y": 0}},
    {"id": 2, "title": "Dedupes/sec", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(rate(pg_globalreg_dedupe_total[1m]))"}], "gridPos": {"h": 7, "w": 6, "x": 6, "y": 0}},
    {"id": 3, "title": "Stale-token rejections/sec", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(rate(pg_fence_rejection_total[1m]))"}],
     "fieldConfig": {"defaults": {"thresholds": {"mode": "absolute", "steps": [{"color": "green", "value": 0}, {"color": "yellow", "value": 1}, {"color": "red", "value": 10}]}}},
     "gridPos": {"h": 7, "w": 6, "x": 12, "y": 0}},
    {"id": 4, "title": "Active fence tokens", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "count(pg_fence_token)"}], "gridPos": {"h": 7, "w": 6, "x": 18, "y": 0}},
    {"id": 5, "title": "Fence token timeline by node", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "pg_fence_token", "legendFormat": "{{pg}}/{{node}}"}],
     "gridPos": {"h": 9, "w": 12, "x": 0, "y": 7}},
    {"id": 6, "title": "Rejections by reason", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum by (reason) (rate(pg_fence_rejection_total[1m]))", "legendFormat": "{{reason}}"}],
     "gridPos": {"h": 9, "w": 12, "x": 12, "y": 7}},
    {"id": 7, "title": "Globalreg size trend", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "pg_globalreg_size", "legendFormat": "size"}, {"expr": "rate(pg_globalreg_dedupe_total[1m])", "legendFormat": "dedupes/s"}],
     "gridPos": {"h": 9, "w": 24, "x": 0, "y": 16}}
  ]
}
```

- [ ] **Step 6: Validate JSON**

Run:
```bash
for f in /opt/workspace/wippy/monkey/manifests/observability/dashboards/0[2-6]*.json; do
  python3 -c "import json,sys; json.load(open('$f'))" && echo "ok: $f" || (echo "fail: $f"; exit 1)
done
```
Expected: 5 lines `ok: …`.

---

## Task 14: Raft Dashboards 07–11

**Files:**
- Create: `monkey/manifests/observability/dashboards/07-raft-leader-term.json`
- Create: `monkey/manifests/observability/dashboards/08-raft-commit-log.json`
- Create: `monkey/manifests/observability/dashboards/09-raft-voter-ladder.json`
- Create: `monkey/manifests/observability/dashboards/10-raft-snapshots.json`
- Create: `monkey/manifests/observability/dashboards/11-raft-append-entries.json`

- [ ] **Step 1: 07-raft-leader-term.json**

```json
{
  "title": "Raft Leader & Term",
  "uid": "wippy-raft-07",
  "tags": ["raft", "wippy"],
  "timezone": "browser", "refresh": "5s", "time": {"from": "now-15m", "to": "now"}, "schemaVersion": 38, "version": 1,
  "panels": [
    {"id": 1, "title": "Current leader (1=leader)", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "max by (node) (raft_state == 2)", "legendFormat": "{{node}}"}],
     "gridPos": {"h": 7, "w": 8, "x": 0, "y": 0}},
    {"id": 2, "title": "Term", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "raft_term"}], "gridPos": {"h": 7, "w": 8, "x": 8, "y": 0}},
    {"id": 3, "title": "Leader changes / 5m", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "increase(raft_leader_changes_total[5m])"}],
     "fieldConfig": {"defaults": {"thresholds": {"mode": "absolute", "steps": [{"color": "green", "value": 0}, {"color": "yellow", "value": 1}, {"color": "red", "value": 3}]}}},
     "gridPos": {"h": 7, "w": 8, "x": 16, "y": 0}},
    {"id": 4, "title": "State per node", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "raft_state", "legendFormat": "{{node}}"}],
     "fieldConfig": {"defaults": {"min": 0, "max": 2, "mappings": [{"type": "value", "options": {"0": {"text": "follower"}, "1": {"text": "candidate"}, "2": {"text": "leader"}}}]}},
     "gridPos": {"h": 9, "w": 12, "x": 0, "y": 7}},
    {"id": 5, "title": "Term timeline", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "raft_term", "legendFormat": "term"}], "gridPos": {"h": 9, "w": 12, "x": 12, "y": 7}},
    {"id": 6, "title": "Election duration p50/p99", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [
       {"expr": "histogram_quantile(0.50, sum by (le) (rate(raft_election_duration_seconds_bucket[5m])))", "legendFormat": "p50"},
       {"expr": "histogram_quantile(0.99, sum by (le) (rate(raft_election_duration_seconds_bucket[5m])))", "legendFormat": "p99"}
     ],
     "fieldConfig": {"defaults": {"unit": "s"}}, "gridPos": {"h": 9, "w": 24, "x": 0, "y": 16}}
  ]
}
```

- [ ] **Step 2: 08-raft-commit-log.json**

```json
{
  "title": "Raft Commit & Log Lag",
  "uid": "wippy-raft-08",
  "tags": ["raft", "wippy", "log"],
  "timezone": "browser", "refresh": "5s", "time": {"from": "now-15m", "to": "now"}, "schemaVersion": 38, "version": 1,
  "panels": [
    {"id": 1, "title": "Commit index", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "max(raft_commit_index)"}], "gridPos": {"h": 7, "w": 6, "x": 0, "y": 0}},
    {"id": 2, "title": "Max log lag", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "max(raft_log_lag)"}],
     "fieldConfig": {"defaults": {"thresholds": {"mode": "absolute", "steps": [{"color": "green", "value": 0}, {"color": "yellow", "value": 100}, {"color": "red", "value": 1000}]}}},
     "gridPos": {"h": 7, "w": 6, "x": 6, "y": 0}},
    {"id": 3, "title": "Followers behind > 0", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "count(raft_log_lag > 0)"}], "gridPos": {"h": 7, "w": 6, "x": 12, "y": 0}},
    {"id": 4, "title": "Commit rate (entries/sec)", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "rate(raft_commit_index[1m])"}], "gridPos": {"h": 7, "w": 6, "x": 18, "y": 0}},
    {"id": 5, "title": "Commit vs last-log per node", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [
       {"expr": "raft_commit_index", "legendFormat": "commit"},
       {"expr": "raft_last_log_index", "legendFormat": "last log {{node}}"}
     ],
     "gridPos": {"h": 10, "w": 12, "x": 0, "y": 7}},
    {"id": 6, "title": "Log lag heatmap by node", "type": "heatmap", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "raft_log_lag", "format": "heatmap", "legendFormat": "{{node}}"}],
     "gridPos": {"h": 10, "w": 12, "x": 12, "y": 7}}
  ]
}
```

- [ ] **Step 3: 09-raft-voter-ladder.json**

```json
{
  "title": "Raft Voter Ladder",
  "uid": "wippy-raft-09",
  "tags": ["raft", "wippy", "voters"],
  "timezone": "browser", "refresh": "5s", "time": {"from": "now-15m", "to": "now"}, "schemaVersion": 38, "version": 1,
  "panels": [
    {"id": 1, "title": "Voters", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "raft_voters"}], "gridPos": {"h": 7, "w": 6, "x": 0, "y": 0}},
    {"id": 2, "title": "Non-voters", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "raft_non_voters"}], "gridPos": {"h": 7, "w": 6, "x": 6, "y": 0}},
    {"id": 3, "title": "Voter cap", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "raft_voter_cap"}], "gridPos": {"h": 7, "w": 6, "x": 12, "y": 0}},
    {"id": 4, "title": "Total cluster size", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "raft_voters + raft_non_voters"}], "gridPos": {"h": 7, "w": 6, "x": 18, "y": 0}},
    {"id": 5, "title": "Voter ladder over time", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [
       {"expr": "raft_voters", "legendFormat": "voters"},
       {"expr": "raft_non_voters", "legendFormat": "non-voters"},
       {"expr": "raft_voter_cap", "legendFormat": "cap"}
     ], "gridPos": {"h": 10, "w": 24, "x": 0, "y": 7}}
  ]
}
```

- [ ] **Step 4: 10-raft-snapshots.json**

```json
{
  "title": "Raft Snapshots",
  "uid": "wippy-raft-10",
  "tags": ["raft", "wippy", "snapshot"],
  "timezone": "browser", "refresh": "5s", "time": {"from": "now-1h", "to": "now"}, "schemaVersion": 38, "version": 1,
  "panels": [
    {"id": 1, "title": "Snapshots / 1h", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(increase(raft_snapshot_total[1h]))"}], "gridPos": {"h": 7, "w": 8, "x": 0, "y": 0}},
    {"id": 2, "title": "Snapshot success ratio", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "100 * sum(rate(raft_snapshot_total{result=\"ok\"}[1h])) / clamp_min(sum(rate(raft_snapshot_total[1h])), 1)"}],
     "fieldConfig": {"defaults": {"unit": "percent"}}, "gridPos": {"h": 7, "w": 8, "x": 8, "y": 0}},
    {"id": 3, "title": "Avg size", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(rate(raft_snapshot_size_bytes_sum[1h])) / clamp_min(sum(rate(raft_snapshot_size_bytes_count[1h])), 1)"}],
     "fieldConfig": {"defaults": {"unit": "bytes"}}, "gridPos": {"h": 7, "w": 8, "x": 16, "y": 0}},
    {"id": 4, "title": "Duration p50/p99", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [
       {"expr": "histogram_quantile(0.50, sum by (le) (rate(raft_snapshot_duration_seconds_bucket[10m])))", "legendFormat": "p50"},
       {"expr": "histogram_quantile(0.99, sum by (le) (rate(raft_snapshot_duration_seconds_bucket[10m])))", "legendFormat": "p99"}
     ],
     "fieldConfig": {"defaults": {"unit": "s"}}, "gridPos": {"h": 10, "w": 12, "x": 0, "y": 7}},
    {"id": 5, "title": "Size distribution (heatmap)", "type": "heatmap", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum by (le) (increase(raft_snapshot_size_bytes_bucket[10m]))", "format": "heatmap"}],
     "gridPos": {"h": 10, "w": 12, "x": 12, "y": 7}}
  ]
}
```

- [ ] **Step 5: 11-raft-append-entries.json**

```json
{
  "title": "Raft AppendEntries",
  "uid": "wippy-raft-11",
  "tags": ["raft", "wippy", "ae"],
  "timezone": "browser", "refresh": "5s", "time": {"from": "now-15m", "to": "now"}, "schemaVersion": 38, "version": 1,
  "panels": [
    {"id": 1, "title": "AE/sec total", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(rate(raft_append_entries_total[1m]))"}], "gridPos": {"h": 7, "w": 8, "x": 0, "y": 0}},
    {"id": 2, "title": "AE failure ratio", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(rate(raft_append_entries_total{result=\"err\"}[5m])) / clamp_min(sum(rate(raft_append_entries_total[5m])), 1)"}],
     "fieldConfig": {"defaults": {"unit": "percentunit", "thresholds": {"mode": "absolute", "steps": [{"color": "green", "value": 0}, {"color": "yellow", "value": 0.01}, {"color": "red", "value": 0.05}]}}},
     "gridPos": {"h": 7, "w": 8, "x": 8, "y": 0}},
    {"id": 3, "title": "P99 latency", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "histogram_quantile(0.99, sum by (le) (rate(raft_append_entries_duration_seconds_bucket[5m])))"}],
     "fieldConfig": {"defaults": {"unit": "s"}}, "gridPos": {"h": 7, "w": 8, "x": 16, "y": 0}},
    {"id": 4, "title": "AE/sec by peer", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum by (peer) (rate(raft_append_entries_total[1m]))", "legendFormat": "{{peer}}"}],
     "gridPos": {"h": 9, "w": 12, "x": 0, "y": 7}},
    {"id": 5, "title": "AE p99 latency by peer", "type": "heatmap", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "histogram_quantile(0.99, sum by (le, peer) (rate(raft_append_entries_duration_seconds_bucket[1m])))", "format": "heatmap", "legendFormat": "{{peer}}"}],
     "gridPos": {"h": 9, "w": 12, "x": 12, "y": 7}},
    {"id": 6, "title": "Errors by peer", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum by (peer) (rate(raft_append_entries_total{result=\"err\"}[1m]))", "legendFormat": "{{peer}}"}],
     "gridPos": {"h": 9, "w": 24, "x": 0, "y": 16}}
  ]
}
```

- [ ] **Step 6: Validate**

Run:
```bash
for f in /opt/workspace/wippy/monkey/manifests/observability/dashboards/0[7-9]*.json /opt/workspace/wippy/monkey/manifests/observability/dashboards/1[01]*.json; do
  python3 -c "import json,sys; json.load(open('$f'))" && echo "ok: $f" || (echo "fail: $f"; exit 1)
done
```
Expected: 5 ok lines.

---

## Task 15: Gossip Dashboards 12–14

**Files:**
- Create: `monkey/manifests/observability/dashboards/12-gossip-member-states.json`
- Create: `monkey/manifests/observability/dashboards/13-gossip-message-flow.json`
- Create: `monkey/manifests/observability/dashboards/14-gossip-convergence.json`

- [ ] **Step 1: 12-gossip-member-states.json**

```json
{
  "title": "Gossip Member States",
  "uid": "wippy-gossip-12",
  "tags": ["gossip", "wippy", "membership"],
  "timezone": "browser", "refresh": "5s", "time": {"from": "now-15m", "to": "now"}, "schemaVersion": 38, "version": 1,
  "panels": [
    {"id": 1, "title": "Alive", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(gossip_members{state=\"alive\"})"}], "gridPos": {"h": 7, "w": 6, "x": 0, "y": 0}},
    {"id": 2, "title": "Suspect", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(gossip_members{state=\"suspect\"})"}],
     "fieldConfig": {"defaults": {"thresholds": {"mode": "absolute", "steps": [{"color": "green", "value": 0}, {"color": "yellow", "value": 1}]}}},
     "gridPos": {"h": 7, "w": 6, "x": 6, "y": 0}},
    {"id": 3, "title": "Dead", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(gossip_members{state=\"dead\"})"}],
     "fieldConfig": {"defaults": {"thresholds": {"mode": "absolute", "steps": [{"color": "green", "value": 0}, {"color": "red", "value": 1}]}}},
     "gridPos": {"h": 7, "w": 6, "x": 12, "y": 0}},
    {"id": 4, "title": "Left", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(gossip_members{state=\"left\"})"}], "gridPos": {"h": 7, "w": 6, "x": 18, "y": 0}},
    {"id": 5, "title": "Members by state over time", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum by (state) (gossip_members)", "legendFormat": "{{state}}"}],
     "gridPos": {"h": 10, "w": 12, "x": 0, "y": 7}},
    {"id": 6, "title": "Churn (transitions/min)", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "rate(gossip_join_total[1m]) + rate(gossip_leave_total[1m])"}],
     "gridPos": {"h": 10, "w": 12, "x": 12, "y": 7}}
  ]
}
```

- [ ] **Step 2: 13-gossip-message-flow.json**

```json
{
  "title": "Gossip Message Flow",
  "uid": "wippy-gossip-13",
  "tags": ["gossip", "wippy", "messages"],
  "timezone": "browser", "refresh": "5s", "time": {"from": "now-15m", "to": "now"}, "schemaVersion": 38, "version": 1,
  "panels": [
    {"id": 1, "title": "TX/sec", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(rate(gossip_message_total{direction=\"tx\"}[1m]))"}], "gridPos": {"h": 7, "w": 6, "x": 0, "y": 0}},
    {"id": 2, "title": "RX/sec", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(rate(gossip_message_total{direction=\"rx\"}[1m]))"}], "gridPos": {"h": 7, "w": 6, "x": 6, "y": 0}},
    {"id": 3, "title": "TX bytes/sec", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(rate(gossip_message_bytes_sum{direction=\"tx\"}[1m]))"}],
     "fieldConfig": {"defaults": {"unit": "Bps"}}, "gridPos": {"h": 7, "w": 6, "x": 12, "y": 0}},
    {"id": 4, "title": "Probe failures/sec", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(rate(gossip_probe_failures_total[1m]))"}],
     "fieldConfig": {"defaults": {"thresholds": {"mode": "absolute", "steps": [{"color": "green", "value": 0}, {"color": "yellow", "value": 1}, {"color": "red", "value": 5}]}}},
     "gridPos": {"h": 7, "w": 6, "x": 18, "y": 0}},
    {"id": 5, "title": "Messages by kind × direction", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum by (kind, direction) (rate(gossip_message_total[1m]))", "legendFormat": "{{kind}} {{direction}}"}],
     "gridPos": {"h": 10, "w": 12, "x": 0, "y": 7}},
    {"id": 6, "title": "Probe duration p99 (s)", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "histogram_quantile(0.99, sum by (le) (rate(gossip_probe_duration_seconds_bucket[1m])))"}],
     "fieldConfig": {"defaults": {"unit": "s"}}, "gridPos": {"h": 10, "w": 12, "x": 12, "y": 7}},
    {"id": 7, "title": "Probe failures by target", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum by (target) (rate(gossip_probe_failures_total[1m]))", "legendFormat": "{{target}}"}],
     "gridPos": {"h": 10, "w": 24, "x": 0, "y": 17}}
  ]
}
```

- [ ] **Step 3: 14-gossip-convergence.json**

```json
{
  "title": "Gossip Convergence",
  "uid": "wippy-gossip-14",
  "tags": ["gossip", "wippy", "convergence"],
  "timezone": "browser", "refresh": "5s", "time": {"from": "now-30m", "to": "now"}, "schemaVersion": 38, "version": 1,
  "panels": [
    {"id": 1, "title": "Median convergence (s)", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "histogram_quantile(0.50, sum by (le) (rate(gossip_convergence_seconds_bucket[10m])))"}],
     "fieldConfig": {"defaults": {"unit": "s"}}, "gridPos": {"h": 7, "w": 8, "x": 0, "y": 0}},
    {"id": 2, "title": "P99 convergence (s)", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "histogram_quantile(0.99, sum by (le) (rate(gossip_convergence_seconds_bucket[10m])))"}],
     "fieldConfig": {"defaults": {"unit": "s", "thresholds": {"mode": "absolute", "steps": [{"color": "green", "value": 0}, {"color": "yellow", "value": 5}, {"color": "red", "value": 30}]}}},
     "gridPos": {"h": 7, "w": 8, "x": 8, "y": 0}},
    {"id": 3, "title": "Suspicion outcomes (15m)", "type": "piechart", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum by (outcome) (increase(gossip_suspicion_resolutions_total[15m]))", "legendFormat": "{{outcome}}"}],
     "gridPos": {"h": 7, "w": 8, "x": 16, "y": 0}},
    {"id": 4, "title": "Convergence histogram", "type": "heatmap", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum by (le) (increase(gossip_convergence_seconds_bucket[5m]))", "format": "heatmap"}],
     "gridPos": {"h": 10, "w": 24, "x": 0, "y": 7}}
  ]
}
```

- [ ] **Step 4: Validate**

Run:
```bash
for f in /opt/workspace/wippy/monkey/manifests/observability/dashboards/1[2-4]*.json; do
  python3 -c "import json,sys; json.load(open('$f'))" && echo "ok: $f" || (echo "fail: $f"; exit 1)
done
```
Expected: 3 ok lines.

---

## Task 16: Chaos Dashboards 15–17

**Files:**
- Create: `monkey/manifests/observability/dashboards/15-chaos-experiments-overlay.json`
- Create: `monkey/manifests/observability/dashboards/16-chaos-mttr.json`
- Create: `monkey/manifests/observability/dashboards/17-chaos-split-brain-detector.json`

- [ ] **Step 1: 15-chaos-experiments-overlay.json**

Annotations overlay chaos experiment windows on top of cluster signal panels. The annotation expression queries `kube_pod_labels{namespace="monkey-chaos"}` (the Chaos Mesh experiments live under that namespace).

```json
{
  "title": "Chaos Experiments Overlay",
  "uid": "wippy-chaos-15",
  "tags": ["chaos", "wippy", "experiments"],
  "timezone": "browser", "refresh": "5s", "time": {"from": "now-1h", "to": "now"}, "schemaVersion": 38, "version": 1,
  "annotations": {"list": [
    {"name": "Chaos experiments", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "expr": "changes(kube_pod_labels{namespace=\"monkey-chaos\"}[1m]) > 0", "iconColor": "red", "enable": true,
     "titleFormat": "{{pod}}", "tagKeys": "namespace,pod"}
  ]},
  "panels": [
    {"id": 1, "title": "PG op p99 latency", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "histogram_quantile(0.99, sum by (le) (rate(pg_op_duration_seconds_bucket[1m])))", "legendFormat": "p99"}],
     "fieldConfig": {"defaults": {"unit": "s"}}, "gridPos": {"h": 9, "w": 24, "x": 0, "y": 0}},
    {"id": 2, "title": "Raft leader changes/min", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "increase(raft_leader_changes_total[1m])"}], "gridPos": {"h": 9, "w": 12, "x": 0, "y": 9}},
    {"id": 3, "title": "Gossip churn/min", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "rate(gossip_join_total[1m]) + rate(gossip_leave_total[1m])"}], "gridPos": {"h": 9, "w": 12, "x": 12, "y": 9}},
    {"id": 4, "title": "Active chaos experiments", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "count(kube_pod_labels{namespace=\"monkey-chaos\"})"}], "gridPos": {"h": 7, "w": 24, "x": 0, "y": 18}}
  ]
}
```

- [ ] **Step 2: 16-chaos-mttr.json**

```json
{
  "title": "Chaos MTTR",
  "uid": "wippy-chaos-16",
  "tags": ["chaos", "wippy", "mttr"],
  "timezone": "browser", "refresh": "5s", "time": {"from": "now-1h", "to": "now"}, "schemaVersion": 38, "version": 1,
  "panels": [
    {"id": 1, "title": "Healthy now? (1=yes)", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "(sum(gossip_members{state=\"alive\"}) / clamp_min(sum(gossip_members), 1) >= 1) * (avg_over_time(raft_state{node=~\".+\"} == 2)[30s:5s] >= 1) * (sum(rate(pg_join_total{result=\"ok\"}[5m]) + rate(pg_leave_total{result=\"ok\"}[5m]) + rate(pg_broadcast_total{result=\"ok\"}[5m])) / clamp_min(sum(rate(pg_join_total[5m]) + rate(pg_leave_total[5m]) + rate(pg_broadcast_total[5m])), 1) >= 0.99)"}],
     "gridPos": {"h": 7, "w": 24, "x": 0, "y": 0}},
    {"id": 2, "title": "Recovery components", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [
       {"expr": "sum(gossip_members{state=\"alive\"}) / clamp_min(sum(gossip_members), 1)", "legendFormat": "alive ratio"},
       {"expr": "sum(rate(pg_join_total{result=\"ok\"}[5m]) + rate(pg_leave_total{result=\"ok\"}[5m]) + rate(pg_broadcast_total{result=\"ok\"}[5m])) / clamp_min(sum(rate(pg_join_total[5m]) + rate(pg_leave_total[5m]) + rate(pg_broadcast_total[5m])), 1)", "legendFormat": "pg success ratio"}
     ],
     "fieldConfig": {"defaults": {"unit": "percentunit"}}, "gridPos": {"h": 9, "w": 24, "x": 0, "y": 7}},
    {"id": 3, "title": "Time since last leader change", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "time() - max_over_time(timestamp(changes(raft_leader_changes_total[1m]) > 0)[1h:])"}],
     "fieldConfig": {"defaults": {"unit": "s"}}, "gridPos": {"h": 7, "w": 24, "x": 0, "y": 16}}
  ]
}
```

- [ ] **Step 3: 17-chaos-split-brain-detector.json**

```json
{
  "title": "Chaos Split-Brain Detector",
  "uid": "wippy-chaos-17",
  "tags": ["chaos", "wippy", "split-brain"],
  "timezone": "browser", "refresh": "5s", "time": {"from": "now-30m", "to": "now"}, "schemaVersion": 38, "version": 1,
  "panels": [
    {"id": 1, "title": "Number of nodes claiming leader", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "count(raft_state == 2)"}],
     "fieldConfig": {"defaults": {"thresholds": {"mode": "absolute", "steps": [{"color": "red", "value": 0}, {"color": "green", "value": 1}, {"color": "red", "value": 2}]}}},
     "gridPos": {"h": 7, "w": 8, "x": 0, "y": 0}},
    {"id": 2, "title": "Distinct membership views", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "count(count by (kubernetes_pod_name) (gossip_members{state=\"alive\"}))"}],
     "gridPos": {"h": 7, "w": 8, "x": 8, "y": 0}},
    {"id": 3, "title": "Fence rejection spikes/sec", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(rate(pg_fence_rejection_total[1m]))"}],
     "fieldConfig": {"defaults": {"thresholds": {"mode": "absolute", "steps": [{"color": "green", "value": 0}, {"color": "yellow", "value": 1}, {"color": "red", "value": 10}]}}},
     "gridPos": {"h": 7, "w": 8, "x": 16, "y": 0}},
    {"id": 4, "title": "Leader claims per node over time", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "raft_state == 2", "legendFormat": "{{node}}"}], "gridPos": {"h": 9, "w": 12, "x": 0, "y": 7}},
    {"id": 5, "title": "Membership disagreement (alive count by pod)", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum by (kubernetes_pod_name) (gossip_members{state=\"alive\"})", "legendFormat": "{{kubernetes_pod_name}}"}],
     "gridPos": {"h": 9, "w": 12, "x": 12, "y": 7}}
  ]
}
```

- [ ] **Step 4: Validate**

Run:
```bash
for f in /opt/workspace/wippy/monkey/manifests/observability/dashboards/1[5-7]*.json; do
  python3 -c "import json,sys; json.load(open('$f'))" && echo "ok: $f" || (echo "fail: $f"; exit 1)
done
```
Expected: 3 ok lines.

---

## Task 17: Cross-cutting Dashboards 18–20

**Files:**
- Create: `monkey/manifests/observability/dashboards/18-runtime-overview.json`
- Create: `monkey/manifests/observability/dashboards/19-otel-pipeline-health.json`
- Create: `monkey/manifests/observability/dashboards/20-pod-resources.json`

- [ ] **Step 1: 18-runtime-overview.json**

```json
{
  "title": "Runtime Overview",
  "uid": "wippy-overview-18",
  "tags": ["overview", "wippy"],
  "timezone": "browser", "refresh": "5s", "time": {"from": "now-15m", "to": "now"}, "schemaVersion": 38, "version": 1,
  "panels": [
    {"id": 1, "title": "Pods running", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(kube_pod_status_phase{namespace=\"wippy-runtime\",phase=\"Running\"})"}],
     "gridPos": {"h": 6, "w": 6, "x": 0, "y": 0}},
    {"id": 2, "title": "Raft leader", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "max by (node) (raft_state == 2)", "legendFormat": "{{node}}"}],
     "gridPos": {"h": 6, "w": 6, "x": 6, "y": 0}},
    {"id": 3, "title": "PG ops/sec", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(rate(pg_op_duration_seconds_count[1m]))"}],
     "gridPos": {"h": 6, "w": 6, "x": 12, "y": 0}},
    {"id": 4, "title": "Gossip alive", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(gossip_members{state=\"alive\"})"}],
     "gridPos": {"h": 6, "w": 6, "x": 18, "y": 0}},
    {"id": 5, "title": "Error rate (PG ops)", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(rate(pg_join_total{result=\"err\"}[1m]) + rate(pg_leave_total{result=\"err\"}[1m]) + rate(pg_broadcast_total{result=\"err\"}[1m]))"}],
     "gridPos": {"h": 9, "w": 24, "x": 0, "y": 6}},
    {"id": 6, "title": "Cluster health components", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [
       {"expr": "raft_state", "legendFormat": "raft state {{node}}"},
       {"expr": "sum(gossip_members{state=\"alive\"})", "legendFormat": "alive"},
       {"expr": "raft_voters", "legendFormat": "voters"},
       {"expr": "raft_non_voters", "legendFormat": "non-voters"}
     ], "gridPos": {"h": 10, "w": 24, "x": 0, "y": 15}}
  ]
}
```

- [ ] **Step 2: 19-otel-pipeline-health.json**

```json
{
  "title": "OTel Pipeline Health",
  "uid": "wippy-otel-19",
  "tags": ["otel", "wippy", "pipeline"],
  "timezone": "browser", "refresh": "5s", "time": {"from": "now-30m", "to": "now"}, "schemaVersion": 38, "version": 1,
  "panels": [
    {"id": 1, "title": "Collector up", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "up{job=\"otel-collector\"}"}], "gridPos": {"h": 7, "w": 6, "x": 0, "y": 0}},
    {"id": 2, "title": "Jaeger up", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "up{job=\"jaeger\"}"}], "gridPos": {"h": 7, "w": 6, "x": 6, "y": 0}},
    {"id": 3, "title": "Receiver accepted points/sec", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(rate(otelcol_receiver_accepted_metric_points[1m]))"}], "gridPos": {"h": 7, "w": 6, "x": 12, "y": 0}},
    {"id": 4, "title": "Exporter dropped points/sec", "type": "stat", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "sum(rate(otelcol_exporter_send_failed_metric_points[1m]))"}],
     "fieldConfig": {"defaults": {"thresholds": {"mode": "absolute", "steps": [{"color": "green", "value": 0}, {"color": "red", "value": 1}]}}},
     "gridPos": {"h": 7, "w": 6, "x": 18, "y": 0}},
    {"id": 5, "title": "Exporter queue size", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "otelcol_exporter_queue_size"}], "gridPos": {"h": 9, "w": 12, "x": 0, "y": 7}},
    {"id": 6, "title": "Collector CPU", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "rate(otelcol_process_cpu_seconds[1m])"}], "gridPos": {"h": 9, "w": 6, "x": 12, "y": 7}},
    {"id": 7, "title": "Collector memory", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "otelcol_process_memory_rss"}], "fieldConfig": {"defaults": {"unit": "bytes"}},
     "gridPos": {"h": 9, "w": 6, "x": 18, "y": 7}},
    {"id": 8, "title": "Spans accepted/dropped", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [
       {"expr": "sum(rate(otelcol_receiver_accepted_spans[1m]))", "legendFormat": "accepted"},
       {"expr": "sum(rate(otelcol_receiver_refused_spans[1m]))", "legendFormat": "refused"},
       {"expr": "sum(rate(otelcol_exporter_send_failed_spans[1m]))", "legendFormat": "failed export"}
     ],
     "gridPos": {"h": 9, "w": 24, "x": 0, "y": 16}}
  ]
}
```

- [ ] **Step 3: 20-pod-resources.json**

```json
{
  "title": "Pod Resources (wippy-runtime)",
  "uid": "wippy-pods-20",
  "tags": ["pods", "wippy", "resources"],
  "timezone": "browser", "refresh": "5s", "time": {"from": "now-30m", "to": "now"}, "schemaVersion": 38, "version": 1,
  "panels": [
    {"id": 1, "title": "CPU per pod", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "rate(container_cpu_usage_seconds_total{namespace=\"wippy-runtime\"}[1m])", "legendFormat": "{{pod}}"}],
     "gridPos": {"h": 9, "w": 12, "x": 0, "y": 0}},
    {"id": 2, "title": "Memory per pod", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "container_memory_working_set_bytes{namespace=\"wippy-runtime\"}", "legendFormat": "{{pod}}"}],
     "fieldConfig": {"defaults": {"unit": "bytes"}}, "gridPos": {"h": 9, "w": 12, "x": 12, "y": 0}},
    {"id": 3, "title": "Net RX per pod", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "rate(container_network_receive_bytes_total{namespace=\"wippy-runtime\"}[1m])", "legendFormat": "{{pod}}"}],
     "fieldConfig": {"defaults": {"unit": "Bps"}}, "gridPos": {"h": 9, "w": 12, "x": 0, "y": 9}},
    {"id": 4, "title": "Net TX per pod", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "rate(container_network_transmit_bytes_total{namespace=\"wippy-runtime\"}[1m])", "legendFormat": "{{pod}}"}],
     "fieldConfig": {"defaults": {"unit": "Bps"}}, "gridPos": {"h": 9, "w": 12, "x": 12, "y": 9}},
    {"id": 5, "title": "Pod restarts (cumulative)", "type": "timeseries", "datasource": {"type": "prometheus", "uid": "PBFA97CFB590B2093"},
     "targets": [{"expr": "kube_pod_container_status_restarts_total{namespace=\"wippy-runtime\"}", "legendFormat": "{{pod}}"}],
     "gridPos": {"h": 9, "w": 24, "x": 0, "y": 18}}
  ]
}
```

- [ ] **Step 4: Validate all 20**

Run:
```bash
ls /opt/workspace/wippy/monkey/manifests/observability/dashboards/ | wc -l
for f in /opt/workspace/wippy/monkey/manifests/observability/dashboards/*.json; do
  python3 -c "import json,sys; json.load(open('$f'))" && echo "ok: $f" || (echo "fail: $f"; exit 1)
done
```
Expected: count `20`, plus 20 ok lines.

---

## Task 18: Materialize ConfigMap from dashboard files

The placeholder ConfigMap from Task 12 needs the actual JSON inlined under `data:`. Add a small shell script to materialize it deterministically.

**Files:**
- Modify: `monkey/manifests/observability/grafana-dashboards-configmap.yaml`
- Create: `monkey/scripts/build-grafana-dashboards-configmap.sh`

- [ ] **Step 1: Write the materializer script**

`monkey/scripts/build-grafana-dashboards-configmap.sh`:

```sh
#!/usr/bin/env sh
# SPDX-License-Identifier: MPL-2.0
#
# Materialize grafana-dashboards-configmap.yaml from dashboards/*.json.
# Run from repo root or any cwd.
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="$ROOT/manifests/observability/dashboards"
OUT="$ROOT/manifests/observability/grafana-dashboards-configmap.yaml"

cat > "$OUT" <<'EOF'
# SPDX-License-Identifier: MPL-2.0
# Auto-generated by scripts/build-grafana-dashboards-configmap.sh
# Do not edit by hand. Edit dashboards/*.json and re-run.
apiVersion: v1
kind: ConfigMap
metadata:
  name: grafana-dashboards-pg-observability
  namespace: observability
  labels:
    grafana_dashboard: "1"
    app: grafana
data:
EOF

for f in "$SRC"/*.json; do
  name=$(basename "$f")
  printf '  %s: |\n' "$name" >> "$OUT"
  sed 's/^/    /' "$f" >> "$OUT"
  printf '\n' >> "$OUT"
done

echo "wrote $OUT"
```

- [ ] **Step 2: Make it executable + run it**

Run:
```bash
chmod +x /opt/workspace/wippy/monkey/scripts/build-grafana-dashboards-configmap.sh
/opt/workspace/wippy/monkey/scripts/build-grafana-dashboards-configmap.sh
```
Expected: `wrote /opt/workspace/wippy/monkey/manifests/observability/grafana-dashboards-configmap.yaml`.

- [ ] **Step 3: Verify the ConfigMap is valid YAML**

Run:
```bash
python3 -c "import yaml,sys; yaml.safe_load(open('/opt/workspace/wippy/monkey/manifests/observability/grafana-dashboards-configmap.yaml'))" && echo "valid YAML"
```
Expected: `valid YAML`.

- [ ] **Step 4: Verify line count is sane**

Run:
```bash
wc -l /opt/workspace/wippy/monkey/manifests/observability/grafana-dashboards-configmap.yaml
```
Expected: ~3000 lines (20 JSONs × ~150 lines each).

---

## Task 19: Update kustomization (if present)

If `monkey/manifests/observability/kustomization.yaml` exists, add the new ConfigMap; remove references to deleted files.

**Files:**
- Modify (or create): `monkey/manifests/observability/kustomization.yaml`

- [ ] **Step 1: Check for existing kustomization**

Run:
```bash
ls /opt/workspace/wippy/monkey/manifests/observability/kustomization.yaml 2>/dev/null && echo exists || echo missing
```

- [ ] **Step 2a: If `exists`, edit**

Open the file, in the `resources:` list:
- Remove any `grafana-dashboards.json`, `grafana-extra-dashboards.yaml`, `grafana-complete.yaml`, `prometheus-advanced-dashboard.yaml` entries.
- Add `- grafana-dashboards-configmap.yaml`.

- [ ] **Step 2b: If `missing`, create a minimal one**

`monkey/manifests/observability/kustomization.yaml`:

```yaml
# SPDX-License-Identifier: MPL-2.0
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - otel-collector.yaml
  - grafana-dashboards-configmap.yaml
```

- [ ] **Step 3: Verify**

Run:
```bash
python3 -c "import yaml; yaml.safe_load(open('/opt/workspace/wippy/monkey/manifests/observability/kustomization.yaml'))" && echo "valid"
```
Expected: `valid`.

---

## Task 20: Smoke E2E on local K3D + final runtime gate

Validate the full pipeline end-to-end: instrumented runtime → otel-collector → Prometheus + Jaeger → Grafana renders all 20 dashboards with live data during a chaos scenario.

**Files:** none (validation only).

- [ ] **Step 1: Confirm K3D is up**

Run:
```bash
k3d cluster list
```
Expected: at least one cluster `STATUS = running`. If not, follow `monkey/k3s/` bootstrap.

- [ ] **Step 2: Build + import the runtime image**

Run:
```bash
cd /opt/workspace/wippy/runtime && \
  docker build -f /opt/workspace/wippy/monkey/Dockerfile.runtime -t wippy-runtime:obs-test . && \
  k3d image import wippy-runtime:obs-test --cluster $(k3d cluster list -o json | python3 -c 'import json,sys;print(json.load(sys.stdin)[0]["name"])')
```
Expected: image imported into the cluster's containerd.

- [ ] **Step 3: Update the runtime Deployment image tag**

If `monkey/manifests/runtime/deployment.yaml` pins an image tag, update it to `wippy-runtime:obs-test` and `imagePullPolicy: Never`.

- [ ] **Step 4: Apply observability + runtime manifests**

Run:
```bash
kubectl apply -k /opt/workspace/wippy/monkey/manifests/observability && \
kubectl apply -k /opt/workspace/wippy/monkey/manifests/runtime
```
Expected: collector, grafana ConfigMap, runtime pods all reconciled.

- [ ] **Step 5: Wait for pods**

Run:
```bash
kubectl -n observability wait --for=condition=Ready pod -l app=otel-collector --timeout=60s
kubectl -n wippy-runtime  wait --for=condition=Ready pod -l app=runtime         --timeout=120s
```
Expected: ready.

- [ ] **Step 6: Run a chaos scenario**

Run:
```bash
kubectl apply -f /opt/workspace/wippy/monkey/scenarios/network-chaos.yaml
sleep 60
```

- [ ] **Step 7: Verify metrics flow into Prometheus**

Port-forward Prometheus:
```bash
kubectl -n observability port-forward svc/prometheus 9090:9090 >/dev/null 2>&1 &
```

Query each top-level series:
```bash
for q in pg_op_duration_seconds_count raft_state gossip_members; do
  echo "== $q =="
  curl -s "http://localhost:9090/api/v1/query?query=$q" | python3 -c 'import json,sys; d=json.load(sys.stdin); r=d.get("data",{}).get("result",[]); print(f"{len(r)} series")'
done
```
Expected: `> 0 series` for each.

- [ ] **Step 8: Verify Jaeger has spans**

Port-forward Jaeger UI:
```bash
kubectl -n observability port-forward svc/jaeger 16686:16686 >/dev/null 2>&1 &
```

Open `http://localhost:16686/` in a browser, select service `wippy-runtime`, confirm spans for `pg.broadcast`, `raft.append_entries`, `gossip.broadcast`.

- [ ] **Step 9: Verify Grafana renders all 20 dashboards**

Port-forward Grafana:
```bash
kubectl -n observability port-forward svc/grafana 3000:3000 >/dev/null 2>&1 &
```

Open `http://localhost:3000/dashboards` and confirm 20 dashboards with `wippy-` UIDs are listed. Open each one in turn and verify **no panel** shows "No data" for PG/Raft/Gossip series. (`pod-resources` may show some empty panels if not under load — acceptable.)

- [ ] **Step 10: Tear down chaos**

Run:
```bash
kubectl delete -f /opt/workspace/wippy/monkey/scenarios/network-chaos.yaml
```
Expected: experiment removed; cluster recovers.

- [ ] **Step 11: Final lint + test gate on the runtime**

Run:
```bash
cd /opt/workspace/wippy/runtime && \
  go test ./... && \
  golangci-lint run ./...
```
Expected: clean.

- [ ] **Step 12: Final summary commit on runtime branch**

> All commits already happened per task. This is a checkpoint to `git log --oneline` and confirm the branch is in a sane state — nothing to commit here.

```bash
cd /opt/workspace/wippy/runtime && git log --oneline -20
```

Expected output: each instrumentation task surfaces as one commit with the corresponding subject (`feat(pg): …`, `feat(raft): …`, `feat(gossip): …`, `feat(telemetrytest): …`).

---

---

## Phase 4 — End-to-end soak (Tasks 21–25)

**Goal:** After Tasks 1–20 are green, destroy the local K3D cluster, recreate the
entire stack **only through Makefile targets**, deploy a long-running
`pg-harness` workload alongside the runtime, run chaos continuously, and
verify objectively that chaos is perturbing the instrumented runtime
signals. All services must keep running indefinitely (the validation
window is bounded only by user interrupt).

This phase produces no commits in `runtime/`. The artifacts live in
`monkey/` and `pg-harness/`.

---

## Task 21: pg-harness long-running binary + Dockerfile

`pg-harness` today is a Go test harness (`testing.TB`-driven). Wrap it in a
small `cmd/runner` that drives an indefinite synthetic workload against an
in-process or peer-discovered `pg.Service`. Containerize it.

**Files:**
- Create: `pg-harness/cmd/runner/main.go`
- Create: `pg-harness/Dockerfile`
- Modify: `pg-harness/harness/harness.go` (export a `RunSynthetic(ctx, cfg)` helper that the test harness AND the runner share)

- [ ] **Step 1: Write the synthetic workload helper**

Add to `pg-harness/harness/harness.go` at the end of the file:

```go
// SyntheticConfig drives RunSynthetic.
type SyntheticConfig struct {
	Nodes        int           // 1..maxTestNodes
	Groups       int           // distinct PG groups to keep churning
	OpsPerSecond int           // approximate combined ops/sec across all groups
	Logger       *zap.Logger
}

// RunSynthetic continuously joins/leaves/broadcasts on a synced in-process
// cluster until ctx is cancelled. Exposed for the long-running runner used by
// chaos soaks; tests use NewTestCluster directly. Returns the last error.
func RunSynthetic(ctx context.Context, cfg SyntheticConfig) error {
	if cfg.Nodes <= 0 {
		cfg.Nodes = 3
	}
	if cfg.Groups <= 0 {
		cfg.Groups = 16
	}
	if cfg.OpsPerSecond <= 0 {
		cfg.OpsPerSecond = 100
	}
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}

	cl := newSyncedClusterForRunner(ctx, cfg.Nodes, cfg.Logger)
	defer cl.Stop()

	tick := time.NewTicker(time.Second / time.Duration(cfg.OpsPerSecond))
	defer tick.Stop()

	var ops uint64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			n := atomic.AddUint64(&ops, 1)
			node := cl.NodeAt(int(n) % cfg.Nodes)
			group := pgapi.Group(fmt.Sprintf("g%d", int(n)%cfg.Groups))
			p := pid.New("synthetic", node.ID)
			switch n % 3 {
			case 0:
				_ = node.Service.Join(group, p)
			case 1:
				_ = node.Service.Leave(group, p)
			case 2:
				_, _ = node.Service.Broadcast(p, group, "ping", payload.Payloads{})
			}
		}
	}
}
```

(The helper `newSyncedClusterForRunner` is a non-`testing.TB` analog of
`NewSyncedCluster`. Add it next to the existing constructor — same wiring,
no `t.Helper()`/`require.NoError`. Convert any `t.Fatalf` calls to
`return nil, err` and propagate up to `RunSynthetic` which logs and
restarts on error.)

- [ ] **Step 2: Write the runner main**

`pg-harness/cmd/runner/main.go`:

```go
// SPDX-License-Identifier: MPL-2.0

// Command runner is a long-running synthetic PG workload used by the chaos
// soak. It runs RunSynthetic in a loop and only exits on SIGTERM.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/wippyai/pg-harness/harness"
	"go.uber.org/zap"
)

func main() {
	nodes := flag.Int("nodes", envInt("NODES", 3), "in-process cluster size")
	groups := flag.Int("groups", envInt("GROUPS", 16), "distinct PG groups")
	ops := flag.Int("ops", envInt("OPS_PER_SECOND", 200), "approx ops/sec")
	flag.Parse()

	logger, _ := zap.NewProduction()
	defer logger.Sync()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg := harness.SyntheticConfig{
		Nodes:        *nodes,
		Groups:       *groups,
		OpsPerSecond: *ops,
		Logger:       logger,
	}
	logger.Info("pg-harness runner starting", zap.Int("nodes", *nodes), zap.Int("groups", *groups), zap.Int("ops", *ops))

	for ctx.Err() == nil {
		if err := harness.RunSynthetic(ctx, cfg); err != nil && ctx.Err() == nil {
			logger.Error("runner restart after error", zap.Error(err))
			time.Sleep(2 * time.Second)
			continue
		}
	}
	log.Println("runner exited")
	_ = os.Stdout.Sync()
}

func envInt(name string, def int) int {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
```

- [ ] **Step 3: Verify it builds**

Run:
```bash
cd /opt/workspace/wippy/pg-harness && go build ./cmd/runner
```
Expected: binary `runner` produced. Delete the artifact (`rm runner`) — the Dockerfile produces it.

- [ ] **Step 4: Write the Dockerfile**

`pg-harness/Dockerfile`:

```dockerfile
# SPDX-License-Identifier: MPL-2.0
FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/runner ./cmd/runner

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /out/runner /runner
USER nonroot:nonroot
ENTRYPOINT ["/runner"]
```

- [ ] **Step 5: Build the image (test only)**

Run:
```bash
cd /opt/workspace/wippy/pg-harness && docker build -t pg-harness:soak .
```
Expected: image `pg-harness:soak` built.

- [ ] **Step 6: Smoke-run the binary outside k8s**

Run:
```bash
docker run --rm pg-harness:soak --nodes=3 --groups=4 --ops=10 &
PID=$!
sleep 5
docker stop $(docker ps -q --filter ancestor=pg-harness:soak)
```
Expected: container starts, prints `pg-harness runner starting`, exits cleanly on SIGTERM.

---

## Task 22: pg-harness Kubernetes Deployment + Service

The Deployment runs the runner image as a long-running workload in the
cluster, configured to talk to runtime via the cluster's headless DNS, and
exports OTel metrics to the same collector.

**Files:**
- Create: `monkey/manifests/runtime/pg-harness-deployment.yaml`
- Modify: `monkey/manifests/runtime/kustomization.yaml` (add the new resource if a kustomization exists)

- [ ] **Step 1: Write the Deployment manifest**

`monkey/manifests/runtime/pg-harness-deployment.yaml`:

```yaml
# SPDX-License-Identifier: MPL-2.0
apiVersion: apps/v1
kind: Deployment
metadata:
  name: pg-harness
  namespace: wippy-runtime
  labels:
    app: pg-harness
spec:
  replicas: 1
  selector:
    matchLabels:
      app: pg-harness
  template:
    metadata:
      labels:
        app: pg-harness
    spec:
      containers:
        - name: runner
          image: pg-harness:soak
          imagePullPolicy: Never
          args:
            - "--nodes=3"
            - "--groups=24"
            - "--ops=200"
          env:
            - name: OTEL_EXPORTER_OTLP_ENDPOINT
              value: "http://otel-collector.observability.svc.cluster.local:4317"
            - name: OTEL_SERVICE_NAME
              value: "pg-harness"
            - name: OTEL_RESOURCE_ATTRIBUTES
              value: "service.name=pg-harness,wippy.role=harness"
          resources:
            requests:
              cpu: "200m"
              memory: "256Mi"
            limits:
              cpu: "1000m"
              memory: "512Mi"
          readinessProbe:
            exec:
              command: ["/bin/true"]
            initialDelaySeconds: 5
            periodSeconds: 10
          livenessProbe:
            exec:
              command: ["/bin/true"]
            initialDelaySeconds: 30
            periodSeconds: 30
      restartPolicy: Always
```

- [ ] **Step 2: If `monkey/manifests/runtime/kustomization.yaml` exists, add the new file**

Run:
```bash
ls /opt/workspace/wippy/monkey/manifests/runtime/kustomization.yaml 2>/dev/null && echo exists || echo missing
```

If exists, edit `resources:` to include `- pg-harness-deployment.yaml`. If missing, do not create — the Makefile target in Task 23 will `kubectl apply -f manifests/runtime/` which globs.

- [ ] **Step 3: Validate manifest YAML**

Run:
```bash
python3 -c "import yaml; yaml.safe_load(open('/opt/workspace/wippy/monkey/manifests/runtime/pg-harness-deployment.yaml'))" && echo ok
```
Expected: `ok`.

---

## Task 23: Rewrite `monkey/Makefile` as the single entry point

Every cluster lifecycle action — destroy, build images, recreate cluster,
deploy stack, run chaos, validate, soak, expose dashboards — goes through
`make`. No standalone shell scripts (the dashboard-materializer in Task 18
is invoked from the Makefile too).

**Files:**
- Modify: `monkey/Makefile` (replace stale targets and references to deleted python generators)

- [ ] **Step 1: Replace the Makefile**

Overwrite `monkey/Makefile`:

```makefile
# SPDX-License-Identifier: MPL-2.0
# Monkey — Chaos Engineering for Wippy Runtime.
# Single entry point. Every action is a make target. No standalone scripts.

K3D_CLUSTER             := monkey-chaos
RUNTIME_NAMESPACE       := wippy-runtime
CHAOS_NAMESPACE         := monkey-chaos
OBSERVABILITY_NAMESPACE := observability

RUNTIME_IMAGE     := wippy-runtime:soak
HARNESS_IMAGE     := pg-harness:soak

RUNTIME_REPO      := $(realpath ../runtime)
HARNESS_REPO      := $(realpath ../pg-harness)
DASHBOARDS_DIR    := manifests/observability/dashboards
DASHBOARDS_CONFIG := manifests/observability/grafana-dashboards-configmap.yaml

.DEFAULT_GOAL := help

# ----------------------------------------------------------------------------
# Help
# ----------------------------------------------------------------------------
.PHONY: help
help:
	@echo "Wippy Chaos — single-entry Makefile"
	@echo ""
	@echo "Lifecycle:"
	@echo "  make destroy          full wipe: chaos + runtime + observability + cluster"
	@echo "  make up               destroy + cluster + chaos-mesh + observability + runtime + harness"
	@echo "  make chaos            apply chaos experiments (network, IO, pod kill)"
	@echo "  make stop-chaos       remove chaos experiments, keep services running"
	@echo "  make validate         assert chaos is materially impacting runtime signals"
	@echo "  make soak             run chaos for SOAK_MINUTES (default 30) with periodic validate"
	@echo ""
	@echo "Build:"
	@echo "  make build-runtime    docker build runtime image and import into k3d"
	@echo "  make build-harness    docker build pg-harness image and import into k3d"
	@echo "  make dashboards       materialize ConfigMap from dashboards/*.json"
	@echo ""
	@echo "Inspect:"
	@echo "  make status           cluster + pod status"
	@echo "  make expose           port-forward grafana/prometheus/jaeger/chaos-mesh to localhost"
	@echo "  make logs-runtime     stream runtime logs"
	@echo "  make logs-harness     stream pg-harness logs"

# ----------------------------------------------------------------------------
# Lifecycle
# ----------------------------------------------------------------------------
.PHONY: destroy
destroy:
	@echo ">> destroying everything"
	-pkill -f "kubectl port-forward" 2>/dev/null || true
	-kubectl delete -f manifests/chaos/ --ignore-not-found=true 2>/dev/null || true
	-kubectl delete -f manifests/runtime/ --ignore-not-found=true 2>/dev/null || true
	-kubectl delete namespace $(RUNTIME_NAMESPACE) --ignore-not-found=true 2>/dev/null || true
	-kubectl delete -f manifests/observability/ --ignore-not-found=true 2>/dev/null || true
	-kubectl delete namespace $(OBSERVABILITY_NAMESPACE) --ignore-not-found=true 2>/dev/null || true
	-helm uninstall chaos-mesh -n $(CHAOS_NAMESPACE) 2>/dev/null || true
	-kubectl delete namespace $(CHAOS_NAMESPACE) --ignore-not-found=true 2>/dev/null || true
	-k3d cluster delete $(K3D_CLUSTER) 2>/dev/null || true
	@echo ">> destroyed"

.PHONY: up
up: destroy cluster chaos-mesh build-runtime build-harness dashboards observability runtime
	@echo ">> stack is up. Run 'make chaos' to start experiments."

.PHONY: cluster
cluster:
	@echo ">> creating k3d cluster $(K3D_CLUSTER)"
	@k3d cluster create $(K3D_CLUSTER) --servers 1 --agents 4 --wait
	@k3d kubeconfig merge $(K3D_CLUSTER) --kubeconfig-merge-default --kubeconfig-switch-context > /dev/null

.PHONY: chaos-mesh
chaos-mesh:
	@echo ">> installing Chaos Mesh"
	@kubectl create namespace $(CHAOS_NAMESPACE) 2>/dev/null || true
	@helm repo add chaos-mesh https://charts.chaos-mesh.org 2>/dev/null || true
	@helm repo update > /dev/null 2>&1 || true
	@if helm list -n $(CHAOS_NAMESPACE) | grep -q chaos-mesh; then \
		echo "OK: already installed"; \
	else \
		helm install chaos-mesh chaos-mesh/chaos-mesh \
			--namespace $(CHAOS_NAMESPACE) \
			--set chaosDaemon.runtime=containerd \
			--set chaosDaemon.socketPath=/run/k3s/containerd/containerd.sock \
			--set dashboard.create=true --set dashboard.securityMode=false \
			--wait; \
	fi

.PHONY: observability
observability:
	@echo ">> deploying observability stack"
	@kubectl create namespace $(OBSERVABILITY_NAMESPACE) 2>/dev/null || true
	@kubectl apply -f manifests/observability/otel-collector.yaml
	@kubectl apply -f $(DASHBOARDS_CONFIG)
	@kubectl -n $(OBSERVABILITY_NAMESPACE) wait --for=condition=Available deploy --all --timeout=180s || true

.PHONY: runtime
runtime:
	@echo ">> deploying runtime + pg-harness"
	@kubectl create namespace $(RUNTIME_NAMESPACE) 2>/dev/null || true
	@kubectl apply -f manifests/runtime/
	@kubectl -n $(RUNTIME_NAMESPACE) wait --for=condition=Available deploy --all --timeout=240s

# ----------------------------------------------------------------------------
# Chaos
# ----------------------------------------------------------------------------
.PHONY: chaos
chaos:
	@echo ">> applying chaos experiments"
	@kubectl apply -f manifests/chaos/

.PHONY: stop-chaos
stop-chaos:
	@echo ">> removing chaos experiments (keeping services up)"
	-kubectl delete -f manifests/chaos/ --ignore-not-found=true

# ----------------------------------------------------------------------------
# Validation
# ----------------------------------------------------------------------------
.PHONY: validate
validate:
	@echo ">> validating chaos impact on runtime signals"
	@./scripts/validate-chaos-impact.sh   # generated by Task 24

# ----------------------------------------------------------------------------
# Soak
# ----------------------------------------------------------------------------
SOAK_MINUTES ?= 30
.PHONY: soak
soak:
	@echo ">> running chaos soak for $(SOAK_MINUTES) minutes"
	@$(MAKE) chaos
	@end=$$(( $$(date +%s) + $(SOAK_MINUTES)*60 )); \
	while [ $$(date +%s) -lt $$end ]; do \
	  echo "--- $$(date) ---"; \
	  $(MAKE) -s validate || echo "validate failed (non-fatal during soak)"; \
	  sleep 60; \
	done
	@echo ">> soak finished. Services keep running. Use 'make stop-chaos' or 'make destroy'."

# ----------------------------------------------------------------------------
# Build images
# ----------------------------------------------------------------------------
.PHONY: build-runtime
build-runtime:
	@echo ">> building runtime image"
	@cd $(RUNTIME_REPO) && docker build -f $(realpath Dockerfile.runtime) -t $(RUNTIME_IMAGE) .
	@k3d image import $(RUNTIME_IMAGE) --cluster $(K3D_CLUSTER)

.PHONY: build-harness
build-harness:
	@echo ">> building pg-harness image"
	@cd $(HARNESS_REPO) && docker build -t $(HARNESS_IMAGE) .
	@k3d image import $(HARNESS_IMAGE) --cluster $(K3D_CLUSTER)

# ----------------------------------------------------------------------------
# Dashboards (materialize ConfigMap from JSONs)
# ----------------------------------------------------------------------------
.PHONY: dashboards
dashboards:
	@echo ">> materializing $(DASHBOARDS_CONFIG) from $(DASHBOARDS_DIR)/*.json"
	@printf '# SPDX-License-Identifier: MPL-2.0\n# Auto-generated. Edit JSONs in dashboards/ and re-run "make dashboards".\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: grafana-dashboards-pg-observability\n  namespace: $(OBSERVABILITY_NAMESPACE)\n  labels:\n    grafana_dashboard: "1"\n    app: grafana\ndata:\n' > $(DASHBOARDS_CONFIG)
	@for f in $(DASHBOARDS_DIR)/*.json; do \
	  name=$$(basename $$f); \
	  printf "  %s: |\n" "$$name" >> $(DASHBOARDS_CONFIG); \
	  sed 's/^/    /' $$f >> $(DASHBOARDS_CONFIG); \
	  printf "\n" >> $(DASHBOARDS_CONFIG); \
	done
	@python3 -c "import yaml,sys; yaml.safe_load(open('$(DASHBOARDS_CONFIG)'))" && echo "OK: valid YAML"

# ----------------------------------------------------------------------------
# Inspect
# ----------------------------------------------------------------------------
.PHONY: status
status:
	@echo ">> k3d clusters"; k3d cluster list
	@echo ""; echo ">> nodes"; kubectl get nodes -o wide
	@echo ""; echo ">> $(RUNTIME_NAMESPACE) pods"; kubectl get pods -n $(RUNTIME_NAMESPACE)
	@echo ""; echo ">> $(OBSERVABILITY_NAMESPACE) pods"; kubectl get pods -n $(OBSERVABILITY_NAMESPACE)
	@echo ""; echo ">> $(CHAOS_NAMESPACE) pods"; kubectl get pods -n $(CHAOS_NAMESPACE)

.PHONY: expose
expose:
	@-pkill -f "kubectl port-forward" 2>/dev/null || true
	@kubectl -n $(OBSERVABILITY_NAMESPACE) port-forward svc/grafana    3000:3000  > /dev/null 2>&1 &
	@kubectl -n $(OBSERVABILITY_NAMESPACE) port-forward svc/prometheus 9090:9090  > /dev/null 2>&1 &
	@kubectl -n $(OBSERVABILITY_NAMESPACE) port-forward svc/jaeger     16686:16686 > /dev/null 2>&1 &
	@kubectl -n $(CHAOS_NAMESPACE)         port-forward svc/chaos-dashboard 2333:2333 > /dev/null 2>&1 &
	@sleep 2
	@echo "  Grafana:    http://localhost:3000  (admin/admin)"
	@echo "  Prometheus: http://localhost:9090"
	@echo "  Jaeger:     http://localhost:16686"
	@echo "  Chaos Mesh: http://localhost:2333"

.PHONY: logs-runtime logs-harness
logs-runtime:
	@kubectl -n $(RUNTIME_NAMESPACE) logs -l app=runtime    --tail=100 -f
logs-harness:
	@kubectl -n $(RUNTIME_NAMESPACE) logs -l app=pg-harness --tail=100 -f
```

- [ ] **Step 2: Verify the Makefile syntax**

Run:
```bash
cd /opt/workspace/wippy/monkey && make help
```
Expected: help text printed; no errors.

- [ ] **Step 3: Verify `make dashboards` works**

Run:
```bash
cd /opt/workspace/wippy/monkey && make dashboards
```
Expected: `OK: valid YAML`. Confirms Task 18's behavior is now Makefile-driven (Task 18's standalone shell script becomes redundant — delete it):

```bash
rm -f /opt/workspace/wippy/monkey/scripts/build-grafana-dashboards-configmap.sh
```

---

## Task 24: Chaos-impact validation script

A Makefile-callable script that compares baseline metrics (pre-chaos
window) to current-window metrics and asserts that chaos is **materially**
impacting the runtime. Without it, "all panels show data" is not enough —
the data could be perfectly steady.

**Files:**
- Create: `monkey/scripts/validate-chaos-impact.sh`

- [ ] **Step 1: Write the script**

`monkey/scripts/validate-chaos-impact.sh`:

```sh
#!/usr/bin/env sh
# SPDX-License-Identifier: MPL-2.0
#
# Validate that chaos is impacting the runtime. Compares the last 5 minutes
# of selected metrics to a 30-min-old baseline window. Exits 0 if at least
# 3 of the 5 indicators show a meaningful perturbation (>= 50% deviation
# from baseline). Exits 1 otherwise.
set -eu

PROM=${PROM:-http://localhost:9090}

ensure_pf() {
  if ! curl -fs "$PROM/-/healthy" >/dev/null 2>&1; then
    echo "Prometheus port-forward missing; running 'make expose'..."
    (cd "$(dirname "$0")/.." && make expose >/dev/null 2>&1) || true
    sleep 3
  fi
}

q() { # q EXPR
  curl -fsG --data-urlencode "query=$1" "$PROM/api/v1/query" \
    | python3 -c 'import json,sys; d=json.load(sys.stdin); r=d.get("data",{}).get("result",[]); print(r[0]["value"][1] if r else "0")'
}

ratio_change() { # ratio_change CURRENT BASELINE  -> percent change as integer (0..*)
  python3 -c "import sys; cur=float(sys.argv[1]); base=float(sys.argv[2]); print(int(abs(cur-base)/max(base,1e-9)*100))" "$1" "$2"
}

ensure_pf

check() { # check NAME CURRENT_QUERY BASELINE_QUERY THRESHOLD_PCT
  name=$1; curq=$2; baseq=$3; thr=$4
  cur=$(q "$curq")
  base=$(q "$baseq")
  pct=$(ratio_change "$cur" "$base")
  printf "  %-32s cur=%s base=%s pct=%s%% thr=%s%%   " "$name" "$cur" "$base" "$pct" "$thr"
  if [ "$pct" -ge "$thr" ]; then echo "IMPACTED"; return 0; else echo "stable"; return 1; fi
}

passed=0
total=5

# 1. PG p99 op duration: chaos network latency should push p99 up.
check "pg_op_p99_seconds" \
  'histogram_quantile(0.99, sum by (le) (rate(pg_op_duration_seconds_bucket[5m])))' \
  'histogram_quantile(0.99, sum by (le) (rate(pg_op_duration_seconds_bucket[5m] offset 30m)))' \
  50 && passed=$((passed+1)) || true

# 2. Raft leader changes: under partition we expect at least one extra change.
check "raft_leader_changes_5m" \
  'increase(raft_leader_changes_total[5m])' \
  'increase(raft_leader_changes_total[5m] offset 30m)' \
  100 && passed=$((passed+1)) || true

# 3. Gossip suspect count: should rise during pod-kill / partition.
check "gossip_suspect" \
  'sum(gossip_members{state="suspect"})' \
  'avg_over_time(sum(gossip_members{state="suspect"})[5m:30s] offset 30m)' \
  100 && passed=$((passed+1)) || true

# 4. PG retries/sec: retry storm if breaker tripping.
check "pg_retries_per_sec" \
  'sum(rate(pg_retry_total[5m]))' \
  'sum(rate(pg_retry_total[5m] offset 30m))' \
  50 && passed=$((passed+1)) || true

# 5. Gossip probe failures: link-level chaos signal.
check "gossip_probe_failures" \
  'sum(rate(gossip_probe_failures_total[5m]))' \
  'sum(rate(gossip_probe_failures_total[5m] offset 30m))' \
  100 && passed=$((passed+1)) || true

echo ""
echo "indicators impacted: $passed / $total"
if [ "$passed" -ge 3 ]; then
  echo "VALIDATE: chaos is materially affecting the runtime"
  exit 0
fi
echo "VALIDATE: chaos has NOT meaningfully perturbed the runtime"
exit 1
```

- [ ] **Step 2: Make it executable**

Run:
```bash
chmod +x /opt/workspace/wippy/monkey/scripts/validate-chaos-impact.sh
```

- [ ] **Step 3: Quick syntax check**

Run:
```bash
sh -n /opt/workspace/wippy/monkey/scripts/validate-chaos-impact.sh && echo ok
```
Expected: `ok`.

---

## Task 25: Full destroy → up → chaos → validate → soak

The actual end-to-end test the user asked for. Every command goes through
`make`. Services stay up indefinitely after; only the operator decides
when to `make destroy`.

- [ ] **Step 1: Confirm Tasks 1–24 are done**

Run from `runtime/`:
```bash
cd /opt/workspace/wippy/runtime && go test ./... && golangci-lint run ./...
```
Expected: clean.

- [ ] **Step 2: Destroy any prior cluster state**

Run:
```bash
cd /opt/workspace/wippy/monkey && make destroy
```
Expected: cluster, namespaces, helm releases gone. Idempotent — succeeds even on a clean machine.

- [ ] **Step 3: Recreate everything**

Run:
```bash
cd /opt/workspace/wippy/monkey && make up
```
Expected: cluster created, Chaos Mesh installed, OTel collector + Grafana dashboards applied, runtime + pg-harness pods Available. The combined target chains `destroy → cluster → chaos-mesh → build-runtime → build-harness → dashboards → observability → runtime`.

- [ ] **Step 4: Sanity check status**

Run:
```bash
cd /opt/workspace/wippy/monkey && make status
```
Expected:
- `wippy-runtime` namespace shows `runtime` and `pg-harness` pods Running (replicas as configured).
- `observability` namespace shows otel-collector, prometheus, grafana, jaeger Running.
- `monkey-chaos` namespace shows chaos-mesh-controller-manager, chaos-daemon, chaos-dashboard Running.

- [ ] **Step 5: Establish a 30-minute baseline (services running, no chaos)**

Wait long enough for the validation script's `offset 30m` window to populate:

```bash
cd /opt/workspace/wippy/monkey && make expose
sleep 1800   # 30 min real time. While waiting, watch dashboards via http://localhost:3000.
```

(In practice, an operator can shorten this by re-tuning `offset 30m` in
`validate-chaos-impact.sh` to `offset 15m` and waiting 15 min instead.
Document the trade-off in the script header. The point is: a baseline
must exist before chaos starts, otherwise "perturbation" cannot be
asserted.)

- [ ] **Step 6: Start chaos**

Run:
```bash
cd /opt/workspace/wippy/monkey && make chaos
```
Expected: chaos experiments applied. Watch dashboard 15 (`Chaos Experiments Overlay`) — chaos start should annotate the timeseries; latency, leader-changes, gossip churn should diverge from baseline.

- [ ] **Step 7: Validate impact**

Run:
```bash
cd /opt/workspace/wippy/monkey && make validate
```
Expected: at least 3 of the 5 indicators flagged `IMPACTED`; script exits 0. If all 5 are `stable`, chaos is not actually reaching the runtime — investigate by checking `make logs-runtime`, `make logs-harness`, and Chaos Mesh dashboard at `http://localhost:2333`.

- [ ] **Step 8: Soak**

Run:
```bash
cd /opt/workspace/wippy/monkey && make soak SOAK_MINUTES=60
```
Expected: 60-minute loop, every minute prints validate output. **No service is restarted by the loop.** All pods continue running. Chaos remains active throughout.

- [ ] **Step 9: Confirm services are still alive after soak**

Run:
```bash
cd /opt/workspace/wippy/monkey && make status
```
Expected: same pods as Step 4, with elevated restart counts on runtime pods if pod-kill chaos fired (acceptable; resilience proven by them coming back).

- [ ] **Step 10: Stop chaos but keep stack up**

Run:
```bash
cd /opt/workspace/wippy/monkey && make stop-chaos
```
Expected: chaos experiments removed; runtime + pg-harness keep running; latency/leader/gossip metrics return to baseline within minutes.

- [ ] **Step 11: Operator decides when to tear down**

The plan is **complete** when `make stop-chaos` finishes and the operator
confirms via `make status` and Grafana that everything is healthy. Per the
user's requirement, services keep running indefinitely until the operator
runs `make destroy`. There is no automated teardown.

---

## Acceptance criteria for the soak (Task 25)

1. ✅ `make destroy` and `make up` are both idempotent and succeed from a cold start.
2. ✅ After `make up`, all pods in `wippy-runtime`, `observability`, and `monkey-chaos` are `Running`.
3. ✅ All 20 dashboards have live PG/Raft/Gossip data within 5 minutes of `make up`.
4. ✅ `make validate` returns exit 0 within 5 minutes of `make chaos`.
5. ✅ During `make soak`, runtime/harness pod count never drops below the configured replica count (pods may restart but the Deployment self-heals).
6. ✅ `make stop-chaos` returns the cluster to a quiet state without restarting runtime/harness.
7. ✅ No standalone shell script outside the Makefile is needed for any of the above.

---

## Spec coverage check

| Spec requirement | Implemented in |
|---|---|
| PG metrics: join/leave/broadcast totals + duration + recipients | Task 2 |
| PG metrics: queue depth, drops, circuit breaker, retry | Task 3 |
| PG metrics: dispatcher inflight, fence tokens, globalreg | Task 4 |
| PG spans (join/leave/broadcast/dispatch) | Task 5 |
| Raft metrics: state, term, leader, election | Task 6 |
| Raft metrics: commit, log lag, AE volume + latency | Task 7 |
| Raft metrics: voter ladder, snapshots; spans | Task 8 |
| Gossip metrics: members, joins, messages | Task 9 |
| Gossip metrics: probes, suspicion, convergence; spans | Task 10 |
| Cleanup of old monkey generators + ConfigMaps | Task 11 |
| ConfigMap shell + Dashboard 01 | Task 12 |
| Dashboards 02–06 (PG group) | Task 13 |
| Dashboards 07–11 (Raft group) | Task 14 |
| Dashboards 12–14 (Gossip group) | Task 15 |
| Dashboards 15–17 (Chaos group) | Task 16 |
| Dashboards 18–20 (Cross-cutting) | Task 17 |
| Materialized ConfigMap script | Task 18 |
| Kustomization wired | Task 19 |
| K3D smoke E2E + acceptance criteria | Task 20 |
| pg-harness long-running runner + Dockerfile | Task 21 |
| pg-harness Deployment manifest | Task 22 |
| Single-entry Makefile (destroy/up/chaos/validate/soak) | Task 23 |
| Automated chaos-impact validation script | Task 24 |
| Full destroy → up → chaos → validate → soak run | Task 25 |

Out-of-scope items (alerts, SLO definitions, log changes) are explicitly excluded per spec and produce no tasks.
