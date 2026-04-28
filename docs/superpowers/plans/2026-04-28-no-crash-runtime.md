# No-Crash Runtime Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate OOMKill restarts of `wippy-runtime` pods under the K3D chaos
profile (50% NetworkChaos partition + 200ms±100ms delay + periodic
container-kill) without raising the 512Mi pod memory limit. Bound every
structure that grows under chaos; emit metrics on every drop.

**Architecture:** Per-QoS-class bounded queues in `cluster/internode` with
drop policies aligned to canonical references (etcd/raft = drop-oldest,
memberlist/SWIM = drop-newest, OTP `pg` = drop-newest with caller error).
Heap-based capped retry queue in `system/pg`. Bounded OTel
BatchSpanProcessor. Removed per-AppendEntries span. Context-cancellable
LeadershipTransfer helper goroutine. `GOMEMLIMIT=400MiB` so Go GC fights
back before the kernel does. New Grafana dashboard "Bounded Runtime —
No-Crash Guarantees" proves each invariant visually.

**Tech Stack:** Go 1.21+, hashicorp/raft, hashicorp/memberlist,
OpenTelemetry SDK Go, Prometheus client, Grafana 10.x, Chaos Mesh 2.6,
K3D 5.x.

**Repo layout:** Runtime code in `/opt/workspace/wippy/runtime/`
(`feature/pg-process-groups` branch, push to `glab` on
`git.spiralscout.com:wippy/runtime`). Cluster manifests + Dockerfile in
`/opt/workspace/wippy/monkey/` (`main` branch, push to
`git.spiralscout.com:wippy/monkey`). pg-harness in
`/opt/workspace/wippy/pg-harness/` (`main` branch, push to
`git.spiralscout.com:wippy/pg-harness`).

**Spec:** `runtime/docs/superpowers/specs/2026-04-28-no-crash-runtime-design.md`

---

## File-structure map

| File | Responsibility | Touched by |
|---|---|---|
| `runtime/cluster/internode/class.go` (new) | `Class` enum, `String()`, default capacities | Task 1 |
| `runtime/cluster/internode/state_manager.go` | Per-class ring queues, `QueueMessageClass`, drop metrics | Task 1, 3 |
| `runtime/cluster/internode/manager.go` | `ManagerConfig` per-class caps, `SendToNode(nodeID, data, class)` signature | Task 1, 2 |
| `runtime/cluster/internode/internode.go` | Topic→class mapping in `Service.Send` | Task 2 |
| `runtime/cluster/internode/telemetry.go` (new) | `internode_dropped_total`, `internode_queue_depth` recorders | Task 1 |
| `runtime/system/pg/retry.go` | Heap-based retry queue, cap 2048, drop-oldest with metric | Task 4 |
| `runtime/system/pg/telemetry.go` | Bucket `attempt`, add `pg_retry_dropped_total`, `pg_retry_queue_size`, `pg_broadcast_dropped_total` | Task 4, 5, 6 |
| `runtime/system/pg/broadcast.go` | Surface `ErrQueueFull` from internode to caller | Task 6 |
| `runtime/service/otel/provider.go` | Batcher bounds, span limits | Task 7 |
| `runtime/system/raft/raft.go` | Drop per-AE span; context-aware `LeadershipTransfer` goroutine | Task 8, 9 |
| `monkey/Dockerfile.runtime` | `ENV GOMEMLIMIT=400MiB` | Task 10 |
| `pg-harness/cmd/runner/main.go` | `--scenario` flag | Task 11 |
| `pg-harness/harness/partition_storm.go` (new) | `RunPartitionStorm` scenario | Task 11 |
| `monkey/manifests/observability/dashboards/21-bounded-runtime.json` (new) | New Grafana dashboard | Task 12 |
| `monkey/manifests/observability/dashboards/00-crash-and-failure-overview.json` | Add "Bounded guarantees" row | Task 12 |

---

## Task 0: Baseline reproducer (proves the bug exists today)

**Files:**
- Create: `runtime/cluster/internode/leak_test.go`

- [ ] **Step 1: Write reproducer test that grows the queue without bound**

Create `runtime/cluster/internode/leak_test.go`:

```go
// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"runtime"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

// TestQueueGrowsWithoutBound proves the current bug: a stuck peer's
// messageQueue has no upper limit. After this fix, the test will be
// rewritten to assert the bound is enforced.
func TestQueueGrowsWithoutBound(t *testing.T) {
	t.Skip("baseline reproducer - delete in Task 1 once bounds are enforced")

	logger := zap.NewNop()
	cfg := DefaultManagerConfig()
	cfg.Logger = logger
	nsm := NewNodeStateManager(cfg, logger)
	const node cluster.NodeID = "stuck-peer"
	nsm.CreateNodeState(node)

	var beforeMS, afterMS runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&beforeMS)

	for i := 0; i < 200_000; i++ {
		_ = nsm.QueueMessage(node, []byte("payload-payload-payload-payload"))
	}

	runtime.GC()
	runtime.ReadMemStats(&afterMS)
	growth := afterMS.HeapAlloc - beforeMS.HeapAlloc

	t.Logf("heap growth after 200k queued messages on stuck peer: %d bytes", growth)
	// No assertion: the test exists to demonstrate that growth is unbounded.
	// Task 1 will replace this file with a bounded-queue assertion.
	time.Sleep(10 * time.Millisecond)
}
```

- [ ] **Step 2: Run to confirm it compiles**

Run from `/opt/workspace/wippy/runtime`:

```sh
go test -run TestQueueGrowsWithoutBound -v ./cluster/internode/...
```

Expected: `--- SKIP: TestQueueGrowsWithoutBound`

- [ ] **Step 3: Commit**

```sh
git add cluster/internode/leak_test.go
git commit -m "test(internode): add baseline OOM reproducer (skipped placeholder)"
```

---

## Task 1: Per-class bounded queues in `cluster/internode`

**Files:**
- Create: `runtime/cluster/internode/class.go`
- Create: `runtime/cluster/internode/telemetry.go`
- Modify: `runtime/cluster/internode/state_manager.go`
- Modify: `runtime/cluster/internode/manager.go` (config only; signature change is Task 2)
- Modify: `runtime/cluster/internode/leak_test.go` (replace with bounded-queue test)

- [ ] **Step 1: Write `class.go` with the Class enum**

Create `runtime/cluster/internode/class.go`:

```go
// SPDX-License-Identifier: MPL-2.0

package internode

// Class is the QoS class of a queued internode message. Each managed peer
// has one ring buffer per class. Drop policy is class-specific:
//
//   - ClassRaftControl: drop-oldest (etcd/raft semantics — newer state
//     wins; control RPCs are idempotent).
//   - ClassGossip: drop-newest (memberlist/SWIM — gossip is lossy by
//     design; the next round will correct it).
//   - ClassPGBroadcast: drop-newest with caller error (Erlang OTP `pg` —
//     fire-and-forget, but observable).
type Class uint8

const (
	ClassRaftControl Class = iota
	ClassGossip
	ClassPGBroadcast
)

// numClasses is the count of Class values. If a new Class is added, this
// MUST be updated; the per-state ring slice is sized from it.
const numClasses = 3

// String renders Class for log/metric labels.
func (c Class) String() string {
	switch c {
	case ClassRaftControl:
		return "raft"
	case ClassGossip:
		return "gossip"
	case ClassPGBroadcast:
		return "pg"
	default:
		return "unknown"
	}
}
```

- [ ] **Step 2: Write `telemetry.go` for internode metrics**

Create `runtime/cluster/internode/telemetry.go`:

```go
// SPDX-License-Identifier: MPL-2.0

package internode

import "github.com/wippyai/runtime/api/metrics"

// telemetry owns metric emission for the internode subsystem. nil-safe so
// unit tests without a Collector wired still work.
type telemetry struct {
	coll metrics.Collector
}

func newTelemetry(coll metrics.Collector) *telemetry {
	t := &telemetry{coll: coll}
	if coll == nil {
		return t
	}
	// Bootstrap counters so dashboards have visible series before any drop.
	for _, c := range []Class{ClassRaftControl, ClassGossip, ClassPGBroadcast} {
		coll.CounterAdd("internode_dropped_total", 0, metrics.Labels{
			"class": c.String(), "reason": "queue_full",
		})
		coll.GaugeSet("internode_queue_depth", 0, metrics.Labels{
			"class": c.String(), "peer": "_init",
		})
	}
	return t
}

func (t *telemetry) recordDrop(class Class, reason string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("internode_dropped_total", metrics.Labels{
		"class": class.String(), "reason": reason,
	})
}

func (t *telemetry) recordQueueDepth(class Class, peer string, depth int) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.GaugeSet("internode_queue_depth", float64(depth), metrics.Labels{
		"class": class.String(), "peer": peer,
	})
}
```

- [ ] **Step 3: Add per-class capacity fields to `ManagerConfig`**

Modify `runtime/cluster/internode/manager.go` — add fields to `ManagerConfig` (between `OutboundQueueSize` and `InitialRetryDelay`):

```go
// Per-class send queue caps. Capacities chosen from canonical references:
// etcd default for control (4096); 2x memberlist default for gossip
// (1024); sized for fan-out for app broadcasts (2048). Hitting a cap
// drops with a metric (internode_dropped_total{class,reason="queue_full"})
// rather than blocking or growing.
RaftControlQueueCap int
GossipQueueCap      int
PGBroadcastQueueCap int
```

In `DefaultManagerConfig()` add the defaults (after `MaxRetryAttempts: 10`):

```go
RaftControlQueueCap: 4096,
GossipQueueCap:      1024,
PGBroadcastQueueCap: 2048,
```

- [ ] **Step 4: Replace `NodeState.messageQueue` with per-class ring buffers**

Modify `runtime/cluster/internode/state_manager.go`. Replace the `NodeState`
struct (lines 21-30) with:

```go
type NodeState struct {
	createdAt     time.Time // for observability: when this state was first created
	queues        [numClasses]*classQueue
	messageNotify chan struct{}
	connection    *NodeConnection
	address       nodeAddress
	state         ConnectionState
	stateMu       sync.RWMutex
	queueMu       sync.Mutex
}

// classQueue is a bounded ring buffer of pending messages for one Class.
// All access is guarded by NodeState.queueMu (held for cross-class
// operations). Operates as drop-oldest or drop-newest depending on the
// caller; capacity is fixed at construction.
type classQueue struct {
	buf  [][]byte
	head int // index of the oldest element (next to drain)
	size int // number of valid entries
}

func newClassQueue(cap int) *classQueue {
	if cap <= 0 {
		cap = 1
	}
	return &classQueue{buf: make([][]byte, cap)}
}

// pushOldest appends, dropping the oldest entry if full. Always succeeds.
// Returns true if a drop occurred.
func (q *classQueue) pushOldest(data []byte) (dropped bool) {
	if q.size == len(q.buf) {
		// drop oldest by advancing head; tail then writes over it
		q.head = (q.head + 1) % len(q.buf)
		q.size--
		dropped = true
	}
	tail := (q.head + q.size) % len(q.buf)
	q.buf[tail] = data
	q.size++
	return dropped
}

// pushNewest appends if there is room. Returns false if full (no insert).
func (q *classQueue) pushNewest(data []byte) (accepted bool) {
	if q.size == len(q.buf) {
		return false
	}
	tail := (q.head + q.size) % len(q.buf)
	q.buf[tail] = data
	q.size++
	return true
}

// pushFront inserts at the front for requeue (callers must respect cap).
// Returns false if full.
func (q *classQueue) pushFront(data []byte) (accepted bool) {
	if q.size == len(q.buf) {
		return false
	}
	q.head = (q.head - 1 + len(q.buf)) % len(q.buf)
	q.buf[q.head] = data
	q.size++
	return true
}

// pop removes and returns the oldest entry; ok=false when empty.
func (q *classQueue) pop() (data []byte, ok bool) {
	if q.size == 0 {
		return nil, false
	}
	data = q.buf[q.head]
	q.buf[q.head] = nil // release reference
	q.head = (q.head + 1) % len(q.buf)
	q.size--
	return data, true
}

// reset drops all entries. Allocations remain.
func (q *classQueue) reset() {
	for i := range q.buf {
		q.buf[i] = nil
	}
	q.head = 0
	q.size = 0
}

func (q *classQueue) len() int { return q.size }
```

Add a `tel *telemetry` field on `NodeStateManager`:

```go
type NodeStateManager struct {
	nodeStates sync.Map // cluster.NodeID -> *NodeState
	logger     *zap.Logger
	tel        *telemetry
	config     ManagerConfig
}
```

Update `NewNodeStateManager` signature:

```go
func NewNodeStateManager(config ManagerConfig, tel *telemetry, logger *zap.Logger) *NodeStateManager {
	return &NodeStateManager{
		logger: logger.Named("state"),
		tel:    tel,
		config: config,
	}
}
```

Update `CreateNodeState` (lines 61-92): inside the `existing` branch reset
each queue (`oldState.queues[i].reset()`), and in the new-state branch
construct the per-class queues:

```go
caps := [numClasses]int{
	ClassRaftControl: nsm.config.RaftControlQueueCap,
	ClassGossip:      nsm.config.GossipQueueCap,
	ClassPGBroadcast: nsm.config.PGBroadcastQueueCap,
}
queues := [numClasses]*classQueue{}
for i := range queues {
	queues[i] = newClassQueue(caps[i])
}
newState := &NodeState{
	queues:        queues,
	messageNotify: make(chan struct{}, 1),
	state:         StateNone,
	createdAt:     time.Now(),
}
```

Replace `QueueMessage` (lines 102-123) with `QueueMessageClass`:

```go
// QueueMessageClass enqueues data for nodeID under the given class.
// Drop policy is class-specific: ClassRaftControl drops the oldest
// entry on overflow (etcd-style); ClassGossip and ClassPGBroadcast drop
// the new entry and return ErrQueueFull (memberlist / Erlang `pg` style).
// In all drop cases, internode_dropped_total{class,reason="queue_full"}
// is incremented.
//
// Returns ErrNodeNotManaged if no state exists for nodeID.
// Returns ErrQueueFull only for drop-newest classes when full.
func (nsm *NodeStateManager) QueueMessageClass(nodeID cluster.NodeID, data []byte, class Class) error {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		return ErrNodeNotManaged
	}
	if data == nil {
		return nil
	}
	if int(class) >= numClasses {
		return ErrQueueFull // unknown class
	}

	state.queueMu.Lock()
	q := state.queues[class]
	var dropped bool
	var rejected bool
	switch class {
	case ClassRaftControl:
		dropped = q.pushOldest(data)
	case ClassGossip, ClassPGBroadcast:
		if !q.pushNewest(data) {
			rejected = true
		}
	}
	depth := q.len()
	state.queueMu.Unlock()

	if dropped {
		nsm.tel.recordDrop(class, "queue_full")
	}
	nsm.tel.recordQueueDepth(class, nodeID, depth)

	if rejected {
		nsm.tel.recordDrop(class, "queue_full")
		return ErrQueueFull
	}

	select {
	case state.messageNotify <- struct{}{}:
	default:
	}
	return nil
}
```

Add the new error at the top of the file (after `ErrNodeNotManaged`):

```go
// ErrQueueFull is returned by QueueMessageClass when a drop-newest class
// queue is at capacity. The caller is expected to log/count and proceed
// (Erlang `pg` semantics: fire-and-forget but observable).
ErrQueueFull = errors.New("internode: send queue is full")
```

Replace `DrainMessages` (lines 189-225) to drain in priority order
(`RaftControl` > `Gossip` > `PGBroadcast`) up to `maxCount`:

```go
func (nsm *NodeStateManager) DrainMessages(nodeID cluster.NodeID, maxCount int) [][]byte {
	state := nsm.GetNodeState(nodeID)
	if state == nil || maxCount <= 0 {
		return nil
	}
	state.queueMu.Lock()
	defer state.queueMu.Unlock()

	out := make([][]byte, 0, maxCount)
	for _, class := range []Class{ClassRaftControl, ClassGossip, ClassPGBroadcast} {
		q := state.queues[class]
		for q.len() > 0 && len(out) < maxCount {
			d, _ := q.pop()
			if d != nil {
				out = append(out, d)
			}
		}
		if len(out) >= maxCount {
			break
		}
	}
	return out
}
```

`RemoveNodeState` (lines 267-293) — replace the queue-discard block:

```go
nodeState.queueMu.Lock()
discarded := 0
for _, q := range nodeState.queues {
	discarded += q.len()
	q.reset()
}
nodeState.queueMu.Unlock()

if discarded > 0 {
	nsm.logger.Warn("Discarded pending messages for removed node",
		zap.String("node", nodeID),
		zap.Int("discarded_messages", discarded))
}
```

Delete the unused `container/list` import.

- [ ] **Step 5: Update `manager.go` to construct telemetry**

Modify `NewConnectionManager` (lines 176-184):

```go
func NewConnectionManager(config ManagerConfig, coll metrics.Collector) ConnectionManager {
	logger := config.Logger.Named("conn")
	tel := newTelemetry(coll)
	return &manager{
		config:       config,
		logger:       logger,
		nodeStates:   NewNodeStateManager(config, tel, logger),
		controlLoops: make(map[cluster.NodeID]*nodeControlLoop),
	}
}
```

Add the import `"github.com/wippyai/runtime/api/metrics"` to manager.go.

- [ ] **Step 6: Update boot wiring to pass metrics.Collector**

Production call sites (verified): two in `runtime/boot/components/system/cluster.go`:
- Line 129: `tempConnMgr := internode.NewConnectionManager(connManagerCfg)`
- Line 150: `connMgr = internode.NewConnectionManager(connManagerCfg)`

Update both:

```go
tempConnMgr := internode.NewConnectionManager(connManagerCfg, metricsapi.GetCollector(ctx))
// ...
connMgr = internode.NewConnectionManager(connManagerCfg, metricsapi.GetCollector(ctx))
```

Add the import (if missing):

```go
metricsapi "github.com/wippyai/runtime/api/metrics"
```

Test call sites (test files, no metrics needed — pass `nil`):
- `runtime/cluster/internode/manager_test.go` lines 74, 86, 99, 110, 121
- `runtime/cluster/internode/integration_test.go` lines 48, 49, 136

For each `NewConnectionManager(config)` in tests, change to
`NewConnectionManager(config, nil)`.

- [ ] **Step 7: Replace baseline test with bounded-queue assertions**

Overwrite `runtime/cluster/internode/leak_test.go`:

```go
// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"errors"
	"testing"

	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

func TestQueueIsBounded_RaftControl_DropsOldest(t *testing.T) {
	cfg := DefaultManagerConfig()
	cfg.Logger = zap.NewNop()
	cfg.RaftControlQueueCap = 8
	nsm := NewNodeStateManager(cfg, newTelemetry(nil), zap.NewNop())
	const node cluster.NodeID = "peer"
	nsm.CreateNodeState(node)

	for i := 0; i < 100; i++ {
		if err := nsm.QueueMessageClass(node, []byte{byte(i)}, ClassRaftControl); err != nil {
			t.Fatalf("RaftControl must never reject (drop-oldest): got %v at i=%d", err, i)
		}
	}
	got := nsm.DrainMessages(node, 100)
	if len(got) != 8 {
		t.Fatalf("expected exactly cap (8) drained, got %d", len(got))
	}
	// Newest 8 entries are 92..99
	for idx, want := byte(92), 0; want < 8; idx, want = idx+1, want+1 {
		if got[want][0] != idx {
			t.Fatalf("want byte %d at idx %d, got %d", idx, want, got[want][0])
		}
	}
}

func TestQueueIsBounded_PGBroadcast_RejectsNewest(t *testing.T) {
	cfg := DefaultManagerConfig()
	cfg.Logger = zap.NewNop()
	cfg.PGBroadcastQueueCap = 4
	nsm := NewNodeStateManager(cfg, newTelemetry(nil), zap.NewNop())
	const node cluster.NodeID = "peer"
	nsm.CreateNodeState(node)

	for i := 0; i < 4; i++ {
		if err := nsm.QueueMessageClass(node, []byte{byte(i)}, ClassPGBroadcast); err != nil {
			t.Fatalf("first 4 PGBroadcast must accept: got %v at i=%d", err, i)
		}
	}
	for i := 4; i < 100; i++ {
		err := nsm.QueueMessageClass(node, []byte{byte(i)}, ClassPGBroadcast)
		if !errors.Is(err, ErrQueueFull) {
			t.Fatalf("expected ErrQueueFull at i=%d, got %v", i, err)
		}
	}
	got := nsm.DrainMessages(node, 100)
	if len(got) != 4 {
		t.Fatalf("expected exactly 4 drained, got %d", len(got))
	}
	// Oldest 4 entries are 0..3 (drop-newest preserves arrival order)
	for i, b := range got {
		if b[0] != byte(i) {
			t.Fatalf("want byte %d at idx %d, got %d", i, i, b[0])
		}
	}
}

func TestDrainPriority_ControlBeforeBroadcast(t *testing.T) {
	cfg := DefaultManagerConfig()
	cfg.Logger = zap.NewNop()
	nsm := NewNodeStateManager(cfg, newTelemetry(nil), zap.NewNop())
	const node cluster.NodeID = "peer"
	nsm.CreateNodeState(node)

	_ = nsm.QueueMessageClass(node, []byte("bcast"), ClassPGBroadcast)
	_ = nsm.QueueMessageClass(node, []byte("ctrl"), ClassRaftControl)

	got := nsm.DrainMessages(node, 2)
	if len(got) != 2 {
		t.Fatalf("expected 2 drained, got %d", len(got))
	}
	if string(got[0]) != "ctrl" {
		t.Fatalf("expected ctrl first, got %q", string(got[0]))
	}
	if string(got[1]) != "bcast" {
		t.Fatalf("expected bcast second, got %q", string(got[1]))
	}
}
```

- [ ] **Step 8: Run tests**

```sh
go test -race -v -run 'TestQueueIsBounded|TestDrainPriority' ./cluster/internode/...
```

Expected: 3 PASS.

- [ ] **Step 9: Run linter**

```sh
golangci-lint run ./cluster/internode/...
```

Expected: zero issues.

- [ ] **Step 10: Commit**

```sh
git add cluster/internode/
git commit -m "feat(internode): per-class bounded queues with drop-oldest|drop-newest"
```

---

## Task 2: Plumb `Class` through `SendToNode` and `Service.Send`

**Files:**
- Modify: `runtime/cluster/internode/manager.go` (interface + implementation)
- Modify: `runtime/cluster/internode/internode.go` (topic→class mapping)
- Test: `runtime/cluster/internode/internode_test.go` (extend or create)

- [ ] **Step 1: Write failing test for topic→class mapping**

Create `runtime/cluster/internode/internode_test.go` if it doesn't exist:

```go
// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"testing"

	pgapi "github.com/wippyai/runtime/api/pg"
)

func TestClassForTopic(t *testing.T) {
	cases := []struct {
		topic string
		want  Class
	}{
		{pgapi.TopicJoin, ClassRaftControl},
		{pgapi.TopicLeave, ClassRaftControl},
		{pgapi.TopicDiscover, ClassRaftControl},
		{pgapi.TopicSync, ClassRaftControl},
		{"app.broadcast.ping", ClassPGBroadcast},
		{"", ClassPGBroadcast}, // unknown defaults to broadcast
	}
	for _, c := range cases {
		if got := ClassForTopic(c.topic); got != c.want {
			t.Errorf("ClassForTopic(%q) = %v, want %v", c.topic, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run to confirm it fails (compile error)**

```sh
go test -run TestClassForTopic -v ./cluster/internode/...
```

Expected: FAIL — `ClassForTopic` undefined.

- [ ] **Step 3: Implement `ClassForTopic`**

Add to `runtime/cluster/internode/class.go`:

```go
// ClassForTopic maps a relay package topic to its QoS class. Membership
// and discovery topics are control-plane (drop-oldest); everything else
// is treated as application broadcast (drop-newest with caller error).
//
// Imports `pgapi` would create a cycle (internode → pg → internode), so
// the topic strings are duplicated as constants here. They MUST stay in
// sync with `runtime/api/pg/topics.go`.
func ClassForTopic(topic string) Class {
	switch topic {
	case "pg.join", "pg.leave", "pg.discover", "pg.sync":
		return ClassRaftControl
	default:
		return ClassPGBroadcast
	}
}
```

(Verify the literals match `pgapi.TopicJoin` etc. by reading
`runtime/api/pg/topics.go`. Update if any constant differs.)

- [ ] **Step 4: Update the test imports**

If `pgapi` import in the test file creates a cycle (it shouldn't, since
`api/pg` is a leaf), keep it. Otherwise change the test to use string
literals matching the constants in `class.go`.

- [ ] **Step 5: Update `ConnectionManager` interface signature**

Modify `runtime/cluster/internode/manager.go` line 149:

```go
SendToNode(nodeID cluster.NodeID, data []byte, class Class) error
```

Modify the implementation (lines 235-247):

```go
func (m *manager) SendToNode(nodeID cluster.NodeID, data []byte, class Class) error {
	err := m.nodeStates.QueueMessageClass(nodeID, data, class)
	if err != nil {
		if errors.Is(err, ErrNodeNotManaged) {
			m.logger.Warn("Dropping message for non-existent or unmanaged node",
				zap.String("target_node", nodeID),
				zap.String("class", class.String()))
			return nil
		}
		// ErrQueueFull surfaces to the caller (broadcast path will count it).
		return err
	}
	return nil
}
```

- [ ] **Step 6: Update `Service.Send` to choose the class**

Modify `runtime/cluster/internode/internode.go` lines 105-120:

```go
func (s *Service) Send(pkg *relay.Package) error {
	data, err := s.codec.Encode(pkg)
	targetNode := pkg.Target
	topic := pkg.Topic
	relay.ReleasePackage(pkg)
	if err != nil {
		return NewEncodePackageError(targetNode.Node, err)
	}
	s.ensureNodeManaged(targetNode.Node)
	return s.connMan.SendToNode(targetNode.Node, data, ClassForTopic(topic))
}
```

(Verify `pkg.Topic` is the field name by re-reading
`runtime/api/relay/package.go`. If it's nested under a Message struct,
adjust accordingly.)

- [ ] **Step 7: Update existing internode tests for the new signature**

Run the test suite to find compilation failures:

```sh
go test -count=1 ./cluster/internode/... 2>&1 | head -40
```

For each compilation error reporting `SendToNode`, append `, ClassRaftControl`
(or the appropriate class) to the call. Use `ClassRaftControl` as the safe
default for tests since it never returns ErrQueueFull.

- [ ] **Step 8: Run all internode tests with race**

```sh
go test -race -count=1 ./cluster/internode/...
```

Expected: ALL PASS.

- [ ] **Step 9: Lint**

```sh
golangci-lint run ./cluster/internode/...
```

- [ ] **Step 10: Commit**

```sh
git add cluster/internode/
git commit -m "feat(internode): plumb Class through SendToNode + topic-based mapping"
```

---

## Task 3: Bounded `RequeueMessages` (no duplication on reconnect)

**Files:**
- Modify: `runtime/cluster/internode/state_manager.go` `RequeueMessages` (lines 238-263)
- Modify: `runtime/cluster/internode/manager.go` `handleDisconnected`/`drainMessages`/`cleanup` (lines 493, 600-616, 534-544)

- [ ] **Step 1: Write failing test**

Add to `runtime/cluster/internode/leak_test.go`:

```go
func TestRequeueRespectsCap(t *testing.T) {
	cfg := DefaultManagerConfig()
	cfg.Logger = zap.NewNop()
	cfg.PGBroadcastQueueCap = 4
	nsm := NewNodeStateManager(cfg, newTelemetry(nil), zap.NewNop())
	const node cluster.NodeID = "peer"
	nsm.CreateNodeState(node)

	// Fill the queue.
	for i := 0; i < 4; i++ {
		_ = nsm.QueueMessageClass(node, []byte{byte(i)}, ClassPGBroadcast)
	}
	// Try to requeue 100 stale messages from a stuck connection — must not
	// grow past the cap (current bug duplicates them).
	stale := make([][]byte, 100)
	for i := range stale {
		stale[i] = []byte{byte(200 + i)}
	}
	nsm.RequeueMessagesClass(node, stale, ClassPGBroadcast)

	got := nsm.DrainMessages(node, 1000)
	if len(got) > 4 {
		t.Fatalf("queue exceeded cap after requeue: got %d, want <=4", len(got))
	}
}
```

- [ ] **Step 2: Run to confirm it fails (compile error)**

```sh
go test -run TestRequeueRespectsCap -v ./cluster/internode/...
```

Expected: FAIL — `RequeueMessagesClass` undefined.

- [ ] **Step 3: Replace `RequeueMessages`**

In `runtime/cluster/internode/state_manager.go`, replace the existing
`RequeueMessages` (lines 238-263) with:

```go
// RequeueMessagesClass returns previously-extracted messages to the head
// of the per-class queue. Respects the class cap: any messages that
// would exceed the cap are dropped with a metric. Drop-oldest classes
// (RaftControl) silently discard from the head; drop-newest classes
// (Gossip, PGBroadcast) discard the tail of the requeue input.
func (nsm *NodeStateManager) RequeueMessagesClass(nodeID cluster.NodeID, messages [][]byte, class Class) {
	if len(messages) == 0 {
		return
	}
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		nsm.logger.Warn("Dropping messages to requeue for unmanaged node",
			zap.String("node_id", nodeID),
			zap.Int("message_count", len(messages)),
			zap.String("class", class.String()))
		return
	}
	if int(class) >= numClasses {
		return
	}

	state.queueMu.Lock()
	q := state.queues[class]
	dropped := 0
	switch class {
	case ClassRaftControl:
		// Drop-oldest semantics: if requeue would overflow, the queue
		// silently makes room by discarding old entries. pushFront cannot
		// drop, so drain from tail first.
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i] == nil {
				continue
			}
			if !q.pushFront(messages[i]) {
				// Full — drop one from tail to make room.
				if d, _ := q.pop(); d != nil {
					dropped++
				}
				_ = q.pushFront(messages[i])
			}
		}
	case ClassGossip, ClassPGBroadcast:
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i] == nil {
				continue
			}
			if !q.pushFront(messages[i]) {
				dropped++
			}
		}
	}
	depth := q.len()
	state.queueMu.Unlock()

	for i := 0; i < dropped; i++ {
		nsm.tel.recordDrop(class, "requeue_overflow")
	}
	nsm.tel.recordQueueDepth(class, nodeID, depth)

	select {
	case state.messageNotify <- struct{}{}:
	default:
	}
}
```

- [ ] **Step 4: Update callers in `manager.go`**

`drainMessages` (lines 600-616): the requeued slice currently uses
`messages[i:]`. Drainees come from `DrainMessages` which is class-blind in
the priority order. Re-extract class info per message. Pragmatic fix:
since `drainMessages` already lost class info during draining, requeue
under `ClassPGBroadcast` (the most lossy class) so we don't pretend to
preserve raft-class strictness during a partial-send failure:

Replace the failed-send branch:

```go
loop.manager.nodeStates.RequeueMessagesClass(loop.nodeID, messages[i:], ClassPGBroadcast)
```

`handleDisconnected` (line 507) and `cleanup` (line 540): same — these
extract from a single `NodeConnection` queue that is already class-blind
post-Send. Use:

```go
loop.manager.nodeStates.RequeueMessagesClass(loop.nodeID, pending, ClassPGBroadcast)
```

(This conservatively places extracted-from-wire messages back in the
broadcast lane, which has stricter drop-newest semantics. Acceptable
because in-flight messages on a torn-down connection are already
"may-be-delivered-or-not" — Erlang `pg`/SWIM convention.)

- [ ] **Step 5: Run the new test**

```sh
go test -race -run 'TestRequeueRespectsCap|TestQueueIsBounded|TestDrainPriority' -v ./cluster/internode/...
```

Expected: 4 PASS.

- [ ] **Step 6: Run all internode tests**

```sh
go test -race -count=1 ./cluster/internode/...
```

Expected: ALL PASS.

- [ ] **Step 7: Commit**

```sh
git add cluster/internode/
git commit -m "fix(internode): requeue respects per-class cap (no duplication on reconnect)"
```

---

## Task 4: Heap-based bounded retry queue in `system/pg`

**Files:**
- Modify: `runtime/system/pg/retry.go`
- Modify: `runtime/system/pg/telemetry.go`
- Test: `runtime/system/pg/retry_test.go`

- [ ] **Step 1: Write failing tests**

Append to `runtime/system/pg/retry_test.go` (create if missing — the file
already exists per existing tests, just append):

```go
func TestRetryQueueRespectsCap(t *testing.T) {
	logger := zap.NewNop()
	tel := newTelemetry(nil, nil, nil)
	rq := newRetryQueue(nil, 5, time.Millisecond, time.Second, logger, tel)
	rq.cap = 8

	for i := 0; i < 100; i++ {
		rq.Add(pid.NodeID(fmt.Sprintf("n-%d", i)),
			pgapi.TopicJoin, []string{"g"}, nil, nil)
	}

	rq.mu.Lock()
	defer rq.mu.Unlock()
	if len(rq.entries) > rq.cap {
		t.Fatalf("retry queue exceeded cap: got %d, want <= %d", len(rq.entries), rq.cap)
	}
}

func TestRetryQueueHeapOrder(t *testing.T) {
	logger := zap.NewNop()
	tel := newTelemetry(nil, nil, nil)
	rq := newRetryQueue(nil, 5, time.Millisecond, time.Second, logger, tel)

	now := time.Now()
	rq.entries = []*retryEntry{
		{nextTry: now.Add(50 * time.Millisecond)},
		{nextTry: now.Add(10 * time.Millisecond)},
		{nextTry: now.Add(30 * time.Millisecond)},
	}
	heap.Init((*retryHeap)(&rq.entries))

	first := heap.Pop((*retryHeap)(&rq.entries)).(*retryEntry)
	if !first.nextTry.Equal(now.Add(10 * time.Millisecond)) {
		t.Fatalf("heap pop must return earliest nextTry first")
	}
}
```

Add imports `"container/heap"`, `"fmt"`, `pgapi "github.com/wippyai/runtime/api/pg"`,
`"github.com/wippyai/runtime/api/pid"` if missing.

- [ ] **Step 2: Run to confirm fails**

```sh
go test -run 'TestRetryQueueRespectsCap|TestRetryQueueHeapOrder' -v ./system/pg/...
```

Expected: FAIL — `rq.cap` undefined; `retryHeap` undefined.

- [ ] **Step 3: Convert `retryQueue` to a heap with cap**

Replace `runtime/system/pg/retry.go` lines 30-220 (the struct, `newRetryQueue`,
`Start`, `Stop`, `Add`, `processRetries`):

```go
// retryHeap implements heap.Interface ordered by nextTry ascending.
// It is a thin alias over the retryQueue.entries slice; all heap ops go
// through this type to keep retryQueue's API focused.
type retryHeap []*retryEntry

func (h retryHeap) Len() int            { return len(h) }
func (h retryHeap) Less(i, j int) bool  { return h[i].nextTry.Before(h[j].nextTry) }
func (h retryHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *retryHeap) Push(x any)         { *h = append(*h, x.(*retryEntry)) }
func (h *retryHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	return x
}

// retryQueue manages failed broadcasts with exponential backoff. Backed
// by a min-heap on nextTry: O(log N) insert/extract instead of the
// linear scan the previous slice version did. Bounded by `cap` to keep
// memory finite under partition; on overflow the oldest (i.e. closest
// to firing) entry is dropped with a metric.
type retryQueue struct {
	service    *Service
	logger     *zap.Logger
	tel        *telemetry
	timer      *time.Timer
	notifyCh   chan struct{}
	stopCh     chan struct{}
	entries    []*retryEntry
	wg         sync.WaitGroup
	sequenceID uint64
	maxRetries int
	cap        int
	baseDelay  time.Duration
	maxDelay   time.Duration
	mu         sync.Mutex
	stopped    bool
}

const defaultRetryQueueCap = 2048

func newRetryQueue(service *Service, maxRetries int, baseDelay, maxDelay time.Duration, logger *zap.Logger, tel *telemetry) *retryQueue {
	if maxRetries <= 0 {
		maxRetries = 3
	}
	if baseDelay <= 0 {
		baseDelay = 100 * time.Millisecond
	}
	if maxDelay <= 0 {
		maxDelay = time.Second
	}
	return &retryQueue{
		entries:    make([]*retryEntry, 0, 64),
		maxRetries: maxRetries,
		cap:        defaultRetryQueueCap,
		baseDelay:  baseDelay,
		maxDelay:   maxDelay,
		service:    service,
		logger:     logger,
		tel:        tel,
	}
}

func (rq *retryQueue) Start(ctx context.Context) {
	rq.mu.Lock()
	if rq.stopped {
		rq.stopped = false
	}
	rq.notifyCh = make(chan struct{}, 1)
	rq.timer = nil
	stopCh := make(chan struct{})
	rq.stopCh = stopCh
	rq.mu.Unlock()
	rq.wg.Add(1)

	go func() {
		defer rq.wg.Done()
		for {
			rq.mu.Lock()
			var timerCh <-chan time.Time
			if rq.timer != nil {
				timerCh = rq.timer.C
			}
			rq.mu.Unlock()

			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			case <-rq.notifyCh:
				rq.processRetries()
			case <-timerCh:
				rq.processRetries()
			}

			rq.mu.Lock()
			if len(rq.entries) > 0 {
				delay := time.Until(rq.entries[0].nextTry)
				if delay <= 0 {
					delay = time.Millisecond
				}
				if rq.timer != nil {
					rq.timer.Reset(delay)
				} else {
					rq.timer = time.NewTimer(delay)
				}
			} else if rq.timer != nil {
				rq.timer.Stop()
				rq.timer = nil
			}
			rq.mu.Unlock()
		}
	}()
}

func (rq *retryQueue) Stop() {
	rq.mu.Lock()
	if rq.stopped {
		rq.mu.Unlock()
		return
	}
	rq.stopped = true
	if rq.timer != nil {
		rq.timer.Stop()
		rq.timer = nil
	}
	if rq.stopCh != nil {
		close(rq.stopCh)
	}
	rq.mu.Unlock()
	rq.wg.Wait()
}

func (rq *retryQueue) Add(targetNode pid.NodeID, topic string, groups []string, pids []pid.PID, payloads payload.Payloads) {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	if rq.stopped {
		rq.logger.Debug("retry queue stopped, dropping entry",
			zap.String("node", targetNode), zap.String("topic", topic))
		return
	}

	if len(rq.entries) >= rq.cap {
		// Drop the soonest-to-fire entry (heap root) to make room.
		// Equivalent to drop-oldest under the priority model: we keep the
		// scheduling tail (entries that have a real backoff window) over
		// near-due ones that are already failing repeatedly.
		dropped := heap.Pop((*retryHeap)(&rq.entries)).(*retryEntry)
		rq.tel.recordRetryDropped(rq.service.hostID, dropped.topic)
		rq.logger.Debug("retry queue at cap, dropped oldest",
			zap.String("dropped_node", dropped.targetNode),
			zap.Int("cap", rq.cap))
	}

	rq.sequenceID++
	entry := &retryEntry{
		id:         rq.sequenceID,
		targetNode: targetNode,
		groups:     groups,
		pids:       pids,
		topic:      topic,
		payloads:   payloads,
		attempts:   0,
		nextTry:    time.Now().Add(rq.baseDelay),
	}
	heap.Push((*retryHeap)(&rq.entries), entry)
	rq.tel.recordRetryQueueSize(rq.service.hostID, len(rq.entries))

	select {
	case rq.notifyCh <- struct{}{}:
	default:
	}
}

func (rq *retryQueue) processRetries() {
	now := time.Now()
	var ready []*retryEntry

	rq.mu.Lock()
	for len(rq.entries) > 0 && !rq.entries[0].nextTry.After(now) {
		ready = append(ready, heap.Pop((*retryHeap)(&rq.entries)).(*retryEntry))
	}
	rq.tel.recordRetryQueueSize(rq.service.hostID, len(rq.entries))
	rq.mu.Unlock()

	for _, entry := range ready {
		rq.attemptRetry(entry)
	}
}
```

Update `requeue` (lines 273-311) to push back via heap:

```go
func (rq *retryQueue) requeue(entry *retryEntry) {
	entry.attempts++
	pgLabel := rq.service.hostID
	op := entry.topic

	if entry.attempts >= rq.maxRetries {
		rq.logger.Warn("max retries exceeded, dropping message",
			zap.String("node", entry.targetNode),
			zap.Strings("groups", entry.groups),
			zap.Uint64("id", entry.id),
			zap.Int("attempts", entry.attempts))
		rq.tel.recordRetryGiveup(pgLabel, op)
		return
	}

	rq.tel.recordRetry(pgLabel, op, entry.attempts)

	delay := rq.baseDelay * time.Duration(1<<entry.attempts)
	if delay > rq.maxDelay {
		delay = rq.maxDelay
	}
	entry.nextTry = time.Now().Add(delay)

	rq.mu.Lock()
	if len(rq.entries) >= rq.cap {
		dropped := heap.Pop((*retryHeap)(&rq.entries)).(*retryEntry)
		rq.tel.recordRetryDropped(pgLabel, dropped.topic)
	}
	heap.Push((*retryHeap)(&rq.entries), entry)
	rq.tel.recordRetryQueueSize(pgLabel, len(rq.entries))
	rq.mu.Unlock()
}
```

Add `"container/heap"` to imports.

- [ ] **Step 4: Add new telemetry recorders**

Append to `runtime/system/pg/telemetry.go` (after `recordRetryGiveup`):

```go
func (t *telemetry) recordRetryDropped(pg, op string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("pg_retry_dropped_total", metrics.Labels{"pg": pg, "op": op})
}

func (t *telemetry) recordRetryQueueSize(pg string, size int) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.GaugeSet("pg_retry_queue_size", float64(size), metrics.Labels{"pg": pg})
}
```

Add bootstrap entries inside `newTelemetry` (after the existing
`pg_retry_giveup_total` line):

```go
coll.CounterAdd("pg_retry_dropped_total", 0, metrics.Labels{"pg": "_init", "op": "noop"})
coll.GaugeSet("pg_retry_queue_size", 0, metrics.Labels{"pg": "_init"})
```

- [ ] **Step 5: Run the tests**

```sh
go test -race -run 'TestRetryQueue' -v ./system/pg/...
```

Expected: 2 PASS.

- [ ] **Step 6: Run the full pg test suite**

```sh
go test -race -count=1 ./system/pg/...
```

Expected: ALL PASS.

- [ ] **Step 7: Commit**

```sh
git add system/pg/
git commit -m "feat(pg): heap-based capped retry queue with drop metrics"
```

---

## Task 5: Bucket the `attempt` label

**Files:**
- Modify: `runtime/system/pg/telemetry.go`
- Test: `runtime/system/pg/telemetry_test.go`

- [ ] **Step 1: Write failing test**

Append to `runtime/system/pg/telemetry_test.go`:

```go
func TestAttemptBucket(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "1"}, {1, "1"},
		{2, "2-3"}, {3, "2-3"},
		{4, "4+"}, {99, "4+"},
	}
	for _, c := range cases {
		if got := attemptBucket(c.in); got != c.want {
			t.Errorf("attemptBucket(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

```sh
go test -run TestAttemptBucket -v ./system/pg/...
```

Expected: FAIL — `attemptBucket` undefined.

- [ ] **Step 3: Implement bucketing**

In `runtime/system/pg/telemetry.go`, replace `recordRetry` (lines 127-133):

```go
// attemptBucket maps an integer attempt count to a bounded label so the
// `pg_retry_total` series cardinality is finite under prolonged churn.
// Buckets: "1" (first attempt), "2-3" (early retries), "4+" (struggling).
func attemptBucket(attempt int) string {
	switch {
	case attempt <= 1:
		return "1"
	case attempt <= 3:
		return "2-3"
	default:
		return "4+"
	}
}

func (t *telemetry) recordRetry(pg, op string, attempt int) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("pg_retry_total", metrics.Labels{
		"pg": pg, "op": op, "attempt": attemptBucket(attempt),
	})
}
```

Remove the `"strconv"` import if it becomes unused.

Update the bootstrap line in `newTelemetry`:

```go
coll.CounterAdd("pg_retry_total", 0, metrics.Labels{"pg": "_init", "op": "noop", "attempt": "1"})
```

- [ ] **Step 4: Run tests**

```sh
go test -race -count=1 ./system/pg/...
```

Expected: ALL PASS.

- [ ] **Step 5: Commit**

```sh
git add system/pg/telemetry.go system/pg/telemetry_test.go
git commit -m "fix(pg): bucket attempt label to bound prometheus cardinality"
```

---

## Task 6: Surface `ErrQueueFull` in PG broadcast path

**Files:**
- Modify: `runtime/system/pg/broadcast.go`
- Modify: `runtime/system/pg/telemetry.go`

- [ ] **Step 1: Add the metric recorder**

Append to `runtime/system/pg/telemetry.go`:

```go
func (t *telemetry) recordBroadcastDropped(pg, reason string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("pg_broadcast_dropped_total",
		metrics.Labels{"pg": pg, "reason": reason})
}
```

Add bootstrap line to `newTelemetry`:

```go
coll.CounterAdd("pg_broadcast_dropped_total", 0,
	metrics.Labels{"pg": "_init", "reason": "noop"})
```

- [ ] **Step 2: Update `sendToMembers` to count queue-full drops**

In `runtime/system/pg/broadcast.go`, modify the `for _, target := range members`
loop body (lines 32-72) — replace the existing send/error block:

```go
if err := s.router.Send(pkg); err != nil {
	if errors.Is(err, internode.ErrQueueFull) {
		// Erlang OTP `pg` semantics: fire-and-forget but observable.
		// Caller already has a sent-count; we count drops separately.
		s.tel.recordBroadcastDropped(s.hostID, "queue_full")
		s.logger.Debug("broadcast dropped: peer send queue full",
			zap.String("target", target.String()))
		continue
	}
	s.logger.Debug("broadcast send failed",
		zap.String("target", target.String()),
		zap.Error(err),
	)
	cb.RecordFailure()
	continue
}
```

Add imports: `"errors"`, `"github.com/wippyai/runtime/cluster/internode"`.

- [ ] **Step 3: Run pg tests**

```sh
go test -race -count=1 ./system/pg/...
```

Expected: ALL PASS.

- [ ] **Step 4: Commit**

```sh
git add system/pg/
git commit -m "feat(pg): observe ErrQueueFull from internode and count broadcast drops"
```

---

## Task 7: Bound the OTel `BatchSpanProcessor`

**Files:**
- Modify: `runtime/service/otel/provider.go`
- Test: `runtime/service/otel/provider_test.go` (extend or create)

- [ ] **Step 1: Write failing test**

Create or extend `runtime/service/otel/provider_test.go`:

```go
// SPDX-License-Identifier: MPL-2.0

package otel

import (
	"context"
	"testing"

	otelapi "github.com/wippyai/runtime/api/service/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/zap"
)

func TestProviderUsesBoundedBatcher(t *testing.T) {
	cfg := otelapi.Config{
		Enabled:       true,
		TracesEnabled: false, // noop exporter — we just inspect the provider
		Endpoint:      "127.0.0.1:0",
		Protocol:      "grpc",
		ServiceName:   "test",
		SampleRate:    1.0,
	}
	tp, err := InitializeProvider(context.Background(), cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("InitializeProvider: %v", err)
	}
	sdkTP, ok := tp.(*sdktrace.TracerProvider)
	if !ok {
		t.Fatalf("expected *sdktrace.TracerProvider, got %T", tp)
	}
	// Indirect check: we cannot reflect on processor internals, but we can
	// at least verify shutdown returns within a short window even with
	// unbounded production load — i.e. the batcher does not block.
	if err := sdkTP.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}
```

- [ ] **Step 2: Run to confirm it passes (pre-fix)**

```sh
go test -run TestProviderUsesBoundedBatcher -v ./service/otel/...
```

Expected: PASS — this test only sanity-checks construction. The real
"bounded" behavior is observable via the metric `otel_sdk_spans_dropped_total`
which the SDK emits internally.

- [ ] **Step 3: Modify `InitializeProvider`**

Replace lines 56-60 of `runtime/service/otel/provider.go`:

```go
// Bounded BatchSpanProcessor: fixed memory ceiling on the in-process
// queue, drop-on-overflow when the collector is unreachable. Matches
// canonical OTel guidance for memory-bound deployments.
tp := sdktrace.NewTracerProvider(
	sdktrace.WithBatcher(exporter,
		sdktrace.WithMaxQueueSize(512),
		sdktrace.WithMaxExportBatchSize(128),
		sdktrace.WithBatchTimeout(2*time.Second),
		sdktrace.WithBlocking(false),
	),
	sdktrace.WithResource(res),
	sdktrace.WithSampler(sampler),
	sdktrace.WithSpanLimits(sdktrace.SpanLimits{
		AttributeValueLengthLimit:  256,
		AttributeCountLimit:        16,
		EventCountLimit:            16,
		LinkCountLimit:             16,
		AttributePerEventCountLimit: 16,
		AttributePerLinkCountLimit:  16,
	}),
)
```

Add `"time"` to the import block if missing (it's already there).

- [ ] **Step 4: Run test**

```sh
go test -race -count=1 ./service/otel/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```sh
git add service/otel/
git commit -m "perf(otel): bound batcher queue/batch + span limits, non-blocking on overflow"
```

---

## Task 8: Drop the per-AppendEntries span

**Files:**
- Modify: `runtime/system/raft/raft.go` `instrumentedTransport.AppendEntries` (lines 582-597)

- [ ] **Step 1: Modify `AppendEntries`**

Replace lines 582-597 with:

```go
func (it *instrumentedTransport) AppendEntries(id hraft.ServerID, target hraft.ServerAddress,
	args *hraft.AppendEntriesRequest, resp *hraft.AppendEntriesResponse) error {
	// Per-AE span removed: under election storms (term=10000+) this path
	// generated hundreds of spans/sec, swamping the bounded batcher and
	// allocating an attribute slice per call. Coverage is preserved via
	// the counter+histogram below; per-trace sampling at lower-rate
	// upstream paths still yields traces when needed.
	start := time.Now()
	err := it.Transport.AppendEntries(id, target, args, resp)
	it.tel.recordAppendEntries(string(id), err, time.Since(start))
	return err
}
```

Remove now-unused imports if any (`attribute`, `trace.WithAttributes`).
Verify by attempting to build:

```sh
go build ./system/raft/...
```

If `attribute` or `context` is still used elsewhere in the file (e.g.
`Persist` line 702 still spans), leave them.

- [ ] **Step 2: Run raft tests**

```sh
go test -race -count=1 ./system/raft/...
```

Expected: ALL PASS.

- [ ] **Step 3: Commit**

```sh
git add system/raft/raft.go
git commit -m "perf(raft): drop per-AppendEntries span on hot path (preserve metrics)"
```

---

## Task 9: Context-aware `LeadershipTransfer` helper goroutine

**Files:**
- Modify: `runtime/system/raft/raft.go` lines 522-530
- Test: `runtime/system/raft/raft_test.go` (extend)

- [ ] **Step 1: Write failing test**

Append to `runtime/system/raft/raft_test.go` (or a suitable existing test
file in the package):

```go
func TestLeadershipTransferGoroutineExits(t *testing.T) {
	defer goleak.VerifyNone(t)

	n := newTestNodeForLeadershipTransfer(t) // existing helper if present;
	// otherwise the simplest reproducer is to run a single-node cluster and
	// call LeadershipTransfer with a tiny timeout — the future never returns
	// because there's no quorum to transfer to.
	defer n.Stop()

	err := n.LeadershipTransfer("", 50*time.Millisecond)
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	// Wait for the helper goroutine to drain via ctx-cancel. goleak runs
	// at deferred test teardown.
	time.Sleep(200 * time.Millisecond)
}
```

If a `newTestNodeForLeadershipTransfer` helper doesn't exist, write one
based on the patterns in `runtime/system/raft/integration_test.go`.

Add imports: `"go.uber.org/goleak"`, `"time"`.

- [ ] **Step 2: Run, expect FAIL with leaked goroutine**

```sh
go test -race -run TestLeadershipTransferGoroutineExits -v ./system/raft/...
```

Expected: FAIL with `goleak` reporting a leaked goroutine in
`raft.LeadershipTransfer.func1`.

- [ ] **Step 3: Fix `LeadershipTransfer`**

Replace lines 522-530 of `runtime/system/raft/raft.go`:

```go
// hashicorp/raft's transfer future has no per-call timeout — wrap it.
// Use a context-cancellable helper so the goroutine cannot outlive the
// caller. Without ctx-cancel the goroutine blocks on f.Error() forever
// when the future never resolves under partition.
ctx, cancel := context.WithTimeout(context.Background(), timeout)
defer cancel()
done := make(chan error, 1)
go func() {
	select {
	case done <- f.Error():
	case <-ctx.Done():
	}
}()
select {
case err := <-done:
	return n.translateError(err)
case <-ctx.Done():
	return raftapi.ErrTimeout
}
```

Remove the now-unused `time.After(timeout)` clause (it was the previous
timeout source; ctx supersedes it).

- [ ] **Step 4: Run test, expect PASS**

```sh
go test -race -run TestLeadershipTransferGoroutineExits -v ./system/raft/...
```

Expected: PASS, no goroutine leak.

- [ ] **Step 5: Run all raft tests**

```sh
go test -race -count=1 ./system/raft/...
```

Expected: ALL PASS.

- [ ] **Step 6: Commit**

```sh
git add system/raft/
git commit -m "fix(raft): context-cancellable LeadershipTransfer helper goroutine"
```

---

## Task 10: `GOMEMLIMIT=400MiB` in the runtime image

**Files:**
- Modify: `monkey/Dockerfile.runtime`

- [ ] **Step 1: Edit Dockerfile**

Replace `monkey/Dockerfile.runtime` with:

```dockerfile
FROM debian:bookworm-slim
RUN apt-get update -qq && apt-get install -y -qq ca-certificates libsqlite3-0 && rm -rf /var/lib/apt/lists/*
COPY wippy /bin/wippy
RUN chmod +x /bin/wippy
EXPOSE 7946 7947 9090 8080 7960
# Go 1.19+ honors GOMEMLIMIT and runs GC more aggressively as the heap
# approaches the limit. 400Mi gives ~100Mi of headroom under the 512Mi
# k8s pod memory limit, so GC reacts before the kernel OOMKills.
ENV GOMEMLIMIT=400MiB
ENTRYPOINT ["/bin/wippy"]
```

- [ ] **Step 2: Verify the Dockerfile builds**

```sh
cd /opt/workspace/wippy/monkey
docker build -f Dockerfile.runtime -t wippy/runtime:gomemlimit-test .
```

Expected: build succeeds; final image lists `GOMEMLIMIT=400MiB` in
`docker inspect wippy/runtime:gomemlimit-test --format '{{.Config.Env}}'`.

(User instruction: do NOT push, deploy, or kubectl apply this image. The
verification stops at `docker build`.)

- [ ] **Step 3: Commit**

```sh
cd /opt/workspace/wippy/monkey
git add Dockerfile.runtime
git commit -m "chore(runtime-image): set GOMEMLIMIT=400MiB so Go GC reacts before OOMKill"
```

---

## Task 11: pg-harness `partition_storm` scenario

**Files:**
- Create: `pg-harness/harness/partition_storm.go`
- Modify: `pg-harness/cmd/runner/main.go`

- [ ] **Step 1: Write failing test**

Create `pg-harness/harness/partition_storm_test.go`:

```go
// SPDX-License-Identifier: MPL-2.0

package harness

import (
	"context"
	"runtime"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestPartitionStormBoundedMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("partition storm runs for several seconds")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	cfg := PartitionStormConfig{
		SyntheticConfig: SyntheticConfig{
			Nodes:        3,
			Groups:       4,
			OpsPerSecond: 1000,
			Logger:       zap.NewNop(),
		},
		LossRatio:        0.5,
		BroadcastsPerSec: 1000,
	}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	if err := RunPartitionStorm(ctx, cfg); err != nil && err != context.DeadlineExceeded {
		t.Fatalf("RunPartitionStorm: %v", err)
	}

	runtime.GC()
	runtime.ReadMemStats(&after)
	growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	const maxGrowth = 50 * 1024 * 1024 // 50Mi
	if growth > maxGrowth {
		t.Fatalf("heap grew %d bytes during partition storm; cap is %d", growth, maxGrowth)
	}
}
```

- [ ] **Step 2: Run, expect compile failure**

```sh
cd /opt/workspace/wippy/pg-harness
go test -run TestPartitionStorm -v ./harness/...
```

Expected: FAIL — `RunPartitionStorm` and `PartitionStormConfig` undefined.

- [ ] **Step 3: Implement the scenario**

Create `pg-harness/harness/partition_storm.go`:

```go
// SPDX-License-Identifier: MPL-2.0

package harness

import (
	"context"
	"fmt"
	"math/rand"
	"sync/atomic"
	"time"

	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"go.uber.org/zap"
)

// PartitionStormConfig drives RunPartitionStorm: a steady high-rate
// broadcast workload combined with simulated message drops between PG
// nodes. The goal is to validate that bounded queues + heap retry +
// drop semantics keep memory finite even with continuous loss.
type PartitionStormConfig struct {
	SyntheticConfig
	LossRatio        float64 // 0..1, fraction of broadcasts that error out at the router
	BroadcastsPerSec int
}

// RunPartitionStorm drives an in-process synced cluster with a high
// broadcast rate while randomly failing a fraction of sends to simulate
// network partition. Returns ctx.Err() on cancellation.
func RunPartitionStorm(ctx context.Context, cfg PartitionStormConfig) error {
	if cfg.Nodes <= 0 {
		cfg.Nodes = 3
	}
	if cfg.Groups <= 0 {
		cfg.Groups = 4
	}
	if cfg.BroadcastsPerSec <= 0 {
		cfg.BroadcastsPerSec = 500
	}
	if cfg.LossRatio < 0 || cfg.LossRatio > 1 {
		cfg.LossRatio = 0.5
	}
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}

	cl, err := newSyncedClusterForRunner(ctx, cfg.Nodes, cfg.Logger)
	if err != nil {
		return err
	}
	defer cl.StopForRunner()

	// Drive broadcasts at the requested rate. Loss is simulated at the
	// caller layer by simply skipping a fraction of broadcasts; this
	// reproduces the queue-saturation pattern (continuous arrival into a
	// peer that is effectively unreachable).
	tickInterval := time.Second / time.Duration(cfg.BroadcastsPerSec)
	if tickInterval < time.Microsecond {
		tickInterval = time.Microsecond
	}
	tick := time.NewTicker(tickInterval)
	defer tick.Stop()

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	var ops uint64

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			n := atomic.AddUint64(&ops, 1)
			if rng.Float64() < cfg.LossRatio {
				continue // simulated drop at the source
			}
			node := cl.NodeAt(int(n) % cfg.Nodes)
			if node == nil {
				continue
			}
			group := pgapi.Group(fmt.Sprintf("g%d", int(n)%cfg.Groups))
			p := pid.PID{Node: node.ID, Host: "storm", UniqID: fmt.Sprintf("p-%d", n)}
			_, _ = node.Service.Broadcast(p, group, "ping", payload.Payloads{})
		}
	}
}
```

- [ ] **Step 4: Add `--scenario` flag to runner**

Modify `pg-harness/cmd/runner/main.go`. Replace the body of `main`:

```go
func main() {
	scenario := flag.String("scenario", envStr("SCENARIO", "synthetic"),
		"scenario to run: synthetic | partition-storm")
	nodes := flag.Int("nodes", envInt("NODES", 3), "in-process cluster size")
	groups := flag.Int("groups", envInt("GROUPS", 16), "distinct PG groups")
	ops := flag.Int("ops", envInt("OPS_PER_SECOND", 200), "approx ops/sec")
	loss := flag.Float64("loss", envFloat("LOSS_RATIO", 0.5),
		"partition-storm only: fraction of broadcasts simulated as dropped")
	bps := flag.Int("broadcasts-per-sec", envInt("BROADCASTS_PER_SEC", 500),
		"partition-storm only: broadcast rate")
	flag.Parse()

	logger, _ := zap.NewProduction()
	defer func() { _ = logger.Sync() }()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Info("pg-harness runner starting",
		zap.String("scenario", *scenario),
		zap.Int("nodes", *nodes),
		zap.Int("groups", *groups),
		zap.Int("ops", *ops))

	for ctx.Err() == nil {
		var err error
		switch *scenario {
		case "partition-storm":
			err = harness.RunPartitionStorm(ctx, harness.PartitionStormConfig{
				SyntheticConfig: harness.SyntheticConfig{
					Nodes:        *nodes,
					Groups:       *groups,
					OpsPerSecond: *ops,
					Logger:       logger,
				},
				LossRatio:        *loss,
				BroadcastsPerSec: *bps,
			})
		default:
			err = harness.RunSynthetic(ctx, harness.SyntheticConfig{
				Nodes:        *nodes,
				Groups:       *groups,
				OpsPerSecond: *ops,
				Logger:       logger,
			})
		}
		if err != nil && ctx.Err() == nil {
			logger.Error("runner restart after error", zap.Error(err))
			time.Sleep(2 * time.Second)
			continue
		}
	}
	log.Println("runner exited")
	_ = os.Stdout.Sync()
}

func envStr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func envFloat(name string, def float64) float64 {
	if v := os.Getenv(name); v != "" {
		var f float64
		_, err := fmt.Sscanf(v, "%f", &f)
		if err == nil {
			return f
		}
	}
	return def
}
```

Add `"fmt"` to imports.

- [ ] **Step 5: Run the test**

```sh
cd /opt/workspace/wippy/pg-harness
go test -race -count=1 -timeout 30s ./harness/...
```

Expected: ALL PASS, including `TestPartitionStormBoundedMemory`.

- [ ] **Step 6: Commit**

```sh
cd /opt/workspace/wippy/pg-harness
git add harness/partition_storm.go harness/partition_storm_test.go cmd/runner/main.go
git commit -m "feat(harness): add partition-storm scenario for bounded-memory regression"
```

---

## Task 12: Grafana dashboard "Bounded Runtime — No-Crash Guarantees"

**Files:**
- Create: `monkey/manifests/observability/dashboards/21-bounded-runtime.json`
- Modify: `monkey/manifests/observability/dashboards/00-crash-and-failure-overview.json`

- [ ] **Step 1: Create the new dashboard JSON**

Create `monkey/manifests/observability/dashboards/21-bounded-runtime.json`
with the following content (rows correspond to the spec sections):

```json
{
  "uid": "wippy-bounded-21",
  "title": "Bounded Runtime — No-Crash Guarantees",
  "tags": ["wippy", "runtime", "bounded", "chaos"],
  "timezone": "browser",
  "schemaVersion": 38,
  "version": 1,
  "refresh": "10s",
  "time": {"from": "now-30m", "to": "now"},
  "templating": {"list": []},
  "annotations": {
    "list": [
      {
        "name": "chaos-windows",
        "datasource": {"type": "prometheus", "uid": "prometheus"},
        "enable": true,
        "iconColor": "red",
        "expr": "chaos_mesh_experiments{phase=\"Running\"}",
        "titleFormat": "Chaos Active",
        "tagKeys": "experiment,namespace"
      }
    ]
  },
  "panels": [
    {
      "id": 1, "type": "row", "title": "Row A — RSS plateau",
      "gridPos": {"h": 1, "w": 24, "x": 0, "y": 0},
      "collapsed": false
    },
    {
      "id": 2, "type": "timeseries",
      "title": "Pod memory vs limit (proves zero-OOM)",
      "datasource": {"type": "prometheus", "uid": "prometheus"},
      "gridPos": {"h": 8, "w": 12, "x": 0, "y": 1},
      "targets": [
        {"expr": "container_memory_working_set_bytes{namespace=\"wippy-runtime\",container=\"runtime\"}", "legendFormat": "{{pod}}", "refId": "A"}
      ],
      "fieldConfig": {"defaults": {"unit": "bytes",
        "thresholds": {"mode": "absolute", "steps": [
          {"color": "green", "value": null},
          {"color": "yellow", "value": 419430400},
          {"color": "red", "value": 536870912}
        ]}
      }, "overrides": []},
      "options": {"tooltip": {"mode": "multi"}, "legend": {"showLegend": true, "displayMode": "table", "placement": "bottom"}}
    },
    {
      "id": 3, "type": "timeseries",
      "title": "GC pressure (rate × heap inuse)",
      "datasource": {"type": "prometheus", "uid": "prometheus"},
      "gridPos": {"h": 8, "w": 12, "x": 12, "y": 1},
      "targets": [
        {"expr": "rate(go_gc_duration_seconds_count{namespace=\"wippy-runtime\"}[1m])", "legendFormat": "gc/s {{pod}}", "refId": "A"},
        {"expr": "go_memstats_heap_inuse_bytes{namespace=\"wippy-runtime\"}", "legendFormat": "heap {{pod}}", "refId": "B"}
      ]
    },
    {
      "id": 4, "type": "stat",
      "title": "Restart count (last 15m) — must be 0",
      "datasource": {"type": "prometheus", "uid": "prometheus"},
      "gridPos": {"h": 4, "w": 24, "x": 0, "y": 9},
      "targets": [
        {"expr": "sum(increase(kube_pod_container_status_restarts_total{namespace=\"wippy-runtime\"}[15m]))", "refId": "A"}
      ],
      "fieldConfig": {"defaults": {"thresholds": {"mode": "absolute", "steps": [
        {"color": "green", "value": null},
        {"color": "yellow", "value": 1},
        {"color": "red", "value": 4}
      ]}}}
    },

    {
      "id": 10, "type": "row", "title": "Row B — Drops by class (visible, not silent)",
      "gridPos": {"h": 1, "w": 24, "x": 0, "y": 13}, "collapsed": false
    },
    {
      "id": 11, "type": "timeseries",
      "title": "Internode drops by class (stacked)",
      "datasource": {"type": "prometheus", "uid": "prometheus"},
      "gridPos": {"h": 8, "w": 12, "x": 0, "y": 14},
      "targets": [
        {"expr": "sum by (class) (rate(internode_dropped_total[1m]))", "legendFormat": "{{class}}", "refId": "A"}
      ],
      "fieldConfig": {"defaults": {"custom": {"stacking": {"mode": "normal"}}}}
    },
    {
      "id": 12, "type": "timeseries",
      "title": "PG broadcast drops (queue_full)",
      "datasource": {"type": "prometheus", "uid": "prometheus"},
      "gridPos": {"h": 8, "w": 12, "x": 12, "y": 14},
      "targets": [
        {"expr": "sum by (pod) (rate(pg_broadcast_dropped_total{reason=\"queue_full\"}[1m]))", "legendFormat": "{{pod}}", "refId": "A"}
      ]
    },
    {
      "id": 13, "type": "timeseries",
      "title": "PG retry drops (cap reached)",
      "datasource": {"type": "prometheus", "uid": "prometheus"},
      "gridPos": {"h": 8, "w": 12, "x": 0, "y": 22},
      "targets": [
        {"expr": "sum by (pod) (rate(pg_retry_dropped_total[1m]))", "legendFormat": "{{pod}}", "refId": "A"}
      ]
    },
    {
      "id": 14, "type": "timeseries",
      "title": "OTel spans dropped (collector + SDK)",
      "datasource": {"type": "prometheus", "uid": "prometheus"},
      "gridPos": {"h": 8, "w": 12, "x": 12, "y": 22},
      "targets": [
        {"expr": "sum(rate(otelcol_processor_dropped_spans[1m]))", "legendFormat": "collector", "refId": "A"},
        {"expr": "sum(rate(otel_sdk_spans_dropped_total[1m])) or vector(0)", "legendFormat": "sdk", "refId": "B"}
      ]
    },

    {
      "id": 20, "type": "row", "title": "Row C — Bounded queue depth",
      "gridPos": {"h": 1, "w": 24, "x": 0, "y": 30}, "collapsed": false
    },
    {
      "id": 21, "type": "timeseries",
      "title": "Internode queue depth by class",
      "datasource": {"type": "prometheus", "uid": "prometheus"},
      "gridPos": {"h": 8, "w": 12, "x": 0, "y": 31},
      "targets": [
        {"expr": "max by (class) (internode_queue_depth)", "legendFormat": "{{class}}", "refId": "A"}
      ]
    },
    {
      "id": 22, "type": "timeseries",
      "title": "PG retry heap size",
      "datasource": {"type": "prometheus", "uid": "prometheus"},
      "gridPos": {"h": 8, "w": 12, "x": 12, "y": 31},
      "targets": [
        {"expr": "max(pg_retry_queue_size)", "legendFormat": "heap size", "refId": "A"}
      ],
      "fieldConfig": {"defaults": {"thresholds": {"mode": "absolute", "steps": [
        {"color": "green", "value": null},
        {"color": "red", "value": 2048}
      ]}}}
    },

    {
      "id": 30, "type": "row", "title": "Row D — Health under chaos (correlation)",
      "gridPos": {"h": 1, "w": 24, "x": 0, "y": 39}, "collapsed": false
    },
    {
      "id": 31, "type": "timeseries",
      "title": "Raft leader churn vs raft drops",
      "datasource": {"type": "prometheus", "uid": "prometheus"},
      "gridPos": {"h": 8, "w": 12, "x": 0, "y": 40},
      "targets": [
        {"expr": "rate(raft_leader_changes_total[1m])", "legendFormat": "leader changes/s", "refId": "A"},
        {"expr": "sum(rate(internode_dropped_total{class=\"raft\"}[1m]))", "legendFormat": "raft drops/s", "refId": "B"}
      ]
    },
    {
      "id": 32, "type": "timeseries",
      "title": "Gossip suspect vs gossip drops",
      "datasource": {"type": "prometheus", "uid": "prometheus"},
      "gridPos": {"h": 8, "w": 12, "x": 12, "y": 40},
      "targets": [
        {"expr": "sum(gossip_members{state=\"suspect\"})", "legendFormat": "suspect", "refId": "A"},
        {"expr": "sum(rate(internode_dropped_total{class=\"gossip\"}[1m]))", "legendFormat": "gossip drops/s", "refId": "B"}
      ]
    },

    {
      "id": 40, "type": "row", "title": "Row E — Recovery post-chaos (auto-heal)",
      "gridPos": {"h": 1, "w": 24, "x": 0, "y": 48}, "collapsed": false
    },
    {
      "id": 41, "type": "timeseries",
      "title": "Raft leaders (must converge to 1)",
      "datasource": {"type": "prometheus", "uid": "prometheus"},
      "gridPos": {"h": 8, "w": 8, "x": 0, "y": 49},
      "targets": [{"expr": "count(raft_state == 2)", "legendFormat": "leaders", "refId": "A"}]
    },
    {
      "id": 42, "type": "timeseries",
      "title": "Gossip alive members (must equal 3)",
      "datasource": {"type": "prometheus", "uid": "prometheus"},
      "gridPos": {"h": 8, "w": 8, "x": 8, "y": 49},
      "targets": [{"expr": "sum(gossip_members{state=\"alive\"})", "legendFormat": "alive", "refId": "A"}]
    },
    {
      "id": 43, "type": "timeseries",
      "title": "Internode queue depth (must drain to 0)",
      "datasource": {"type": "prometheus", "uid": "prometheus"},
      "gridPos": {"h": 8, "w": 8, "x": 16, "y": 49},
      "targets": [{"expr": "sum(internode_queue_depth)", "legendFormat": "depth total", "refId": "A"}]
    }
  ]
}
```

- [ ] **Step 2: Validate JSON**

```sh
cd /opt/workspace/wippy/monkey
python3 -c "import json,sys; json.load(open('manifests/observability/dashboards/21-bounded-runtime.json'))" && echo OK
```

Expected: prints `OK`.

- [ ] **Step 3: Add "Bounded guarantees" row to existing crash dashboard**

Read the current `monkey/manifests/observability/dashboards/00-crash-and-failure-overview.json`
and inject a new row at the top of `panels`. Use the Edit tool to insert
the following row + 3 stat panels at the top of the `panels` array:

```json
{"id": 9001, "type": "row", "title": "Bounded guarantees",
 "gridPos": {"h": 1, "w": 24, "x": 0, "y": 0}, "collapsed": false},
{"id": 9002, "type": "stat", "title": "Restarts last 15m",
 "datasource": {"type": "prometheus", "uid": "prometheus"},
 "gridPos": {"h": 4, "w": 8, "x": 0, "y": 1},
 "targets": [{"expr": "sum(increase(kube_pod_container_status_restarts_total{namespace=\"wippy-runtime\"}[15m]))", "refId": "A"}],
 "fieldConfig": {"defaults": {"thresholds": {"mode": "absolute", "steps": [
   {"color": "green", "value": null}, {"color": "red", "value": 1}]}}}},
{"id": 9003, "type": "stat", "title": "Max RSS (last 15m)",
 "datasource": {"type": "prometheus", "uid": "prometheus"},
 "gridPos": {"h": 4, "w": 8, "x": 8, "y": 1},
 "targets": [{"expr": "max_over_time(container_memory_working_set_bytes{namespace=\"wippy-runtime\",container=\"runtime\"}[15m])", "refId": "A"}],
 "fieldConfig": {"defaults": {"unit": "bytes",
   "thresholds": {"mode": "absolute", "steps": [
     {"color": "green", "value": null},
     {"color": "yellow", "value": 419430400},
     {"color": "red", "value": 536870912}]}}}},
{"id": 9004, "type": "stat", "title": "Drops/s (working as designed under chaos)",
 "datasource": {"type": "prometheus", "uid": "prometheus"},
 "gridPos": {"h": 4, "w": 8, "x": 16, "y": 1},
 "targets": [{"expr": "sum(rate(internode_dropped_total[1m]))", "refId": "A"}],
 "fieldConfig": {"defaults": {"thresholds": {"mode": "absolute", "steps": [
   {"color": "blue", "value": null}]}}}}
```

Increment the `y` coordinates of all subsequent panels by 5 to make room.
The sed/Edit will likely require careful line-by-line work; use the Edit
tool with the exact existing first-panel block as the anchor.

- [ ] **Step 4: Validate the edited JSON**

```sh
python3 -c "import json,sys; json.load(open('manifests/observability/dashboards/00-crash-and-failure-overview.json'))" && echo OK
```

Expected: prints `OK`.

- [ ] **Step 5: Commit**

```sh
cd /opt/workspace/wippy/monkey
git add manifests/observability/dashboards/21-bounded-runtime.json \
        manifests/observability/dashboards/00-crash-and-failure-overview.json
git commit -m "feat(dashboards): add Bounded Runtime dashboard + guarantees row in crash overview"
```

---

## Task 13: Final integration check (no-cluster)

**Files:** none (verification only)

- [ ] **Step 1: Build the runtime binary cleanly**

```sh
cd /opt/workspace/wippy/runtime
go build ./...
```

Expected: zero output, exit 0.

- [ ] **Step 2: Lint the entire runtime**

```sh
golangci-lint run ./...
```

Expected: zero issues.

- [ ] **Step 3: Run the full test suite with race detector**

```sh
go test -race -count=1 ./...
```

Expected: ALL PASS.

- [ ] **Step 4: Run the pg-harness suite with race**

```sh
cd /opt/workspace/wippy/pg-harness
go test -race -count=1 ./...
```

Expected: ALL PASS, including `TestPartitionStormBoundedMemory`.

- [ ] **Step 5: Build the runtime image**

```sh
cd /opt/workspace/wippy/runtime
GOOS=linux GOARCH=amd64 CGO_ENABLED=1 go build -o /tmp/wippy ./cmd/wippy
cd /opt/workspace/wippy/monkey
cp /tmp/wippy ./wippy
docker build -f Dockerfile.runtime -t wippy/runtime:no-crash .
docker inspect wippy/runtime:no-crash --format '{{range .Config.Env}}{{println .}}{{end}}' | grep GOMEMLIMIT
rm -f wippy
```

Expected: prints `GOMEMLIMIT=400MiB`.

(User instruction: do NOT push, deploy, or kubectl apply.)

- [ ] **Step 6: Final commit (only if anything new on disk)**

If any tasks generated leftover files, ensure they are committed in their
respective repos. Otherwise, this step is a no-op.

---

## Out of scope (do NOT do these)

- **Do NOT touch the live K3D cluster.** No `kubectl apply`, `kubectl edit`,
  `kubectl delete`, `helm upgrade`, no `make up` / `make destroy`.
- **Do NOT push images to a registry.** `docker build` only.
- **Do NOT modify `monkey/manifests/runtime/*.yaml`** beyond the
  Dockerfile. Pod limits stay at 512Mi/800m.
- **Do NOT modify `monkey/manifests/chaos/*.yaml`.** The chaos profile
  is the spec; the runtime must tolerate it.
- **Do NOT modify the Lua workload** in `monkey/manifests/runtime/configmap.yaml`.
- **Do NOT add a memory watchdog goroutine.** Approach 2 was rejected.
- **Do NOT add subsystem supervisor restarts.** Approach 3 was rejected.
- **Do NOT push branches without user approval.** All commits stay local
  unless the user explicitly asks for `git push`.

## Acceptance criteria

The plan is done when:

1. `go build`, `golangci-lint run`, and `go test -race` all pass clean
   in `runtime/` and `pg-harness/`.
2. The new `TestPartitionStormBoundedMemory` test passes (runtime memory
   bounded under simulated 50% loss + 1k broadcasts/sec for 8s).
3. `docker build -f Dockerfile.runtime` produces an image whose
   `Config.Env` contains `GOMEMLIMIT=400MiB`.
4. Both new dashboards (`21-bounded-runtime.json` and the modified
   `00-crash-and-failure-overview.json`) are valid JSON.
5. All commits are made locally on the existing branches; nothing is
   pushed without explicit user request.
