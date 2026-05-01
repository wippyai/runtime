# Distributed Coordination Layer — Implementation Spec

Status: draft v1
Target: hardened via adversarial codex audit rounds before implementation
Goal: provide the foundational coordination primitives for the Wippy runtime — name registry, process groups, distributed KV, leader election, distributed locks, cluster singletons — such that user application code never reinvents distributed consensus.

This spec is **grounded in the existing Wippy codebase**. Every construct references either existing code paths or explicit new files to be added. No speculative APIs.

---

## 1. Goals And Non-Goals

### 1.1 Goals

1. **Three coordination primitives with one mental model:** eventual KV, strong KV, process groups. Every primitive shares the same replication transport, same host-based declaration pattern, same security model, same lifecycle.
2. **Erlang-level guarantees for the process model:** after a process name is registered, every reachable node can route to that process. No stale pointers during steady state. No split-brain during partitions.
3. **Reuse existing infrastructure:** memberlist for discovery, internode TCP mesh for transport, topology for monitors/links, event bus for notifications. No parallel network stacks.
4. **Production-ready operational semantics:** persistent state where required, snapshotting, metrics, membership changes via consensus, observability hooks.
5. **Correct at any cluster size:** 1 node, 2 nodes, 3+ nodes. Each size has explicit documented semantics and failure modes.
6. **Deterministic user experience:** errors are named and actionable. Modes are explicit, not inferred. Configuration surfaces are minimal but complete.

### 1.2 Non-Goals

1. **Replacing Postgres for durable workflow state.** The strong KV stores coordination state only (names, leases, membership). Durable application state goes to Postgres via the workflow engine (separate spec).
2. **Geo-distributed active-active clusters.** Multi-datacenter with cross-region writes is out of scope for v1. Single-region clusters only.
3. **Byzantine fault tolerance.** Nodes are assumed to be honest but may crash, be slow, or be partitioned.
4. **Multi-Raft / per-scope consensus groups.** One Raft group handles all strong coordination state in v1. Multi-Raft is future work if throughput demands it.
5. **User-facing Super Strong KV.** The "no stale reads" tier is reserved for internal process naming. Users get Strong (Raft-backed) or Eventual KV, both of which tolerate bounded staleness.

### 1.3 Measurable Success Criteria

1. **Correctness:** Jepsen-style property tests pass for split-brain, asymmetric partitions, leader failover, and membership changes. Linearizability checker validates Strong KV under random network nemesis.
2. **Latency:** Eventual KV write returns in <1ms on local node. Strong KV write returns in <5ms on a 3-node LAN cluster. Name registration (Super Strong) returns in <10ms on a 5-node LAN cluster.
3. **Throughput:** Strong KV sustains ≥5k writes/sec on commodity hardware (per Raft group). Eventual KV sustains ≥50k writes/sec per node.
4. **Fault tolerance:** 3-node cluster tolerates 1 node failure with zero data loss and <2s failover. 5-node cluster tolerates 2 node failures. 1-node and 2-node clusters behave per the documented mode selection.

---

## 2. Architecture Overview

### 2.1 The Four Primitives

```
┌──────────────────────────────────────────────────────────────┐
│                   User-facing primitives                       │
├───────────────────┬────────────────┬────────────────────────┤
│  Name Registry    │  Process Groups│  Eventual KV            │
│  (internal)       │  (pg.host)     │  (kv.host)              │
├───────────────────┴────────────────┴────────────────────────┤
│                       Strong KV                                │
│                   (internal, optional user exposure via        │
│                    strong_kv.host in future)                   │
├──────────────────────────────────────────────────────────────┤
│             Backends                                           │
│  Eventual: discover/sync/delta over internode mesh             │
│  Strong:   hashicorp/raft + custom transport over internode    │
├──────────────────────────────────────────────────────────────┤
│           Existing Infrastructure                              │
│  memberlist │ internode │ topology │ event bus                 │
└──────────────────────────────────────────────────────────────┘
```

**Primitive table:**

| Primitive         | Backend                   | Fresh reads?      | Uniqueness | User-facing? |
|-------------------|---------------------------|-------------------|------------|--------------|
| Eventual KV       | Discover/sync/delta       | No (ms lag)       | Optional   | Yes          |
| Strong KV         | Raft + local reads        | Brief lag         | Yes        | Yes (v2)     |
| Super Strong KV   | Raft + sync-all-reachable | Always            | Yes        | No           |
| Process Groups    | Discover/sync/delta       | No (ms lag)       | No         | Yes          |

Name Registry is a specific use of Super Strong KV. It is never exposed as a generic "Super Strong" API — it is wrapped by `api/names/` which takes pid.PID values and integrates with topology.

### 2.2 Shared Patterns

Two patterns repeat across every service in this spec; the canonical descriptions live here and later sections just reference them.

**Host-manager pattern.** Every `*.host` service (`kv.host`, `pg.host`, `names.host`, `raftkv`) is realized as a `service/*/manager.go` that implements `registry.EntryListener`. The manager subscribes to registry events for a specific `Kind`, spawns a backend instance per entry, hands each instance its ID, config, and backend deps, and tears instances down on entry removal. See `service/host/manager.go` and `service/store/memory/manager.go` for the two reference implementations — every new manager in this spec follows that exact shape.

**Lua `*.open` pattern.** Lua modules expose a single `module.open(id)` constructor that runs a security check (`security.IsAllowed(ctx, "{module}.open", id, {pid = senderPID.String()})`), resolves the entry through the host manager, and returns a typed userdata with the service-specific methods bound to that instance. Reference: `runtime/lua/modules/store/store.go:90-141`. Later sections (§4.5, §5.5, §9.6) only show the service-specific method surface, not the `open` scaffolding.

### 2.3 Naming Conventions

| Layer       | Package                        | Purpose                                        |
|-------------|--------------------------------|------------------------------------------------|
| Interface   | `api/cluster/kv.go` (exists)   | Low-level KV interface, string keys/byte values |
| Interface   | `api/cluster/kv_errors.go` (exists) | Sentinel errors                           |
| Interface   | `api/store/store.go` (exists)  | User-level store interface, registry.ID keys   |
| Interface   | `api/pg/pg.go` (PR 241)        | Process groups interface                       |
| Interface   | `api/names/` (new)             | Process name registry interface                |
| Interface   | `api/service/kv/config.go` (new) | kv.host registry entry config                |
| Interface   | `api/service/pg/config.go` (new) | pg.host registry entry config                |
| Interface   | `api/service/names/config.go` (new) | names.host registry entry config          |
| System impl | `system/kv/` (exists: memory backend) | Local KV state machine                  |
| System impl | `system/dds/` (new)            | Shared replication transport                   |
| System impl | `system/raftkv/` (new)         | Raft-backed strong KV                           |
| System impl | `system/pg/` (PR 241)          | Process groups service                          |
| System impl | `system/names/` (new)          | Name registry service                           |
| Service     | `service/kv/` (new)            | kv.host Manager (registry.EntryListener)        |
| Service     | `service/pg/` (new, replaces PR 241 singleton) | pg.host Manager            |
| Service     | `service/names/` (new)         | names.host Manager                              |
| Lua module  | `runtime/lua/modules/kv/`      | Lua API for KV                                  |
| Lua module  | `runtime/lua/modules/pg/` (PR 241) | Lua API for PG                              |
| Lua module  | `runtime/lua/modules/names/` (new) | Lua API for names                           |
| Boot        | `boot/components/system/`      | Boot components for system-level services      |

---

## 3. Interfaces (Low-Level)

### 3.1 `api/cluster/kv.go` — The Core KV Interface

Already exists (added during design). Defines the contract every KV backend must implement.

```go
// Version is a monotonically increasing revision number assigned to each
// key mutation. Version 0 means the key does not exist.
type Version = uint64

type Entry struct {
    Key     string
    Value   []byte
    Version Version
    LeaseID LeaseID
}

type LeaseID string

type KV interface {
    Get(key string) (Entry, error)
    Set(key string, value []byte) (Version, error)
    Delete(key string) error
    SetIfAbsent(key string, value []byte) (Version, bool, error)
    CompareAndSwap(key string, expect Version, value []byte) (Version, bool, error)
    Scan(prefix string, fn func(Entry) bool) error
    Watch(ctx context.Context, prefix string) (Watcher, error)
    GrantLease(ctx context.Context, ttl time.Duration) (Lease, error)

    // GetLease returns a handle for an existing lease. Used by consumers
    // that need to adopt a lease they previously granted — notably the
    // singleton supervisor on restart (§13.2), which must reuse its own
    // lease to avoid self-eviction during a local crash-and-restart.
    //
    // Returns ErrLeaseNotFound if the lease does not exist or has
    // already expired. Returns ErrLeaseExpired if the lease exists in
    // a tombstoned form but is no longer active.
    GetLease(ctx context.Context, id LeaseID) (Lease, error)

    SetWithLease(key string, value []byte, lease LeaseID) (Version, error)
    SetIfAbsentWithLease(key string, value []byte, lease LeaseID) (Version, bool, error)
}

type Lease interface {
    ID() LeaseID
    TTL() time.Duration
    KeepAlive(ctx context.Context) error
    Revoke(ctx context.Context) error
    Done() <-chan struct{}
}

type Watcher interface {
    Events() <-chan WatchEvent
    Close() error
}

type WatchEvent struct {
    Type     WatchEventType // WatchPut, WatchDelete, WatchExpired
    Current  *Entry         // nil on delete/expire
    Previous *Entry         // nil on create
}
```

**Design notes:**

- Reads (`Get`, `Scan`) take no `context.Context` — they are always local from the in-memory snapshot, always fast, never fail for network reasons.
- Writes and Watch take `context.Context` — they may block on consensus or subscriptions.
- `Scan` uses a callback instead of returning a slice to avoid allocations for large prefixes and to allow early termination (return false).
- `Version` is a type alias for `uint64` to simplify interop. A version of 0 is reserved for "not found" and used in `SetIfAbsent` semantics.
- Leases are separate from TTL on entries: TTL is per-entry auto-expiry, leases are a shared lifetime handle that multiple entries can bind to (node session leases).

### 3.2 Sentinel Errors

Every backend MUST return these exact sentinels for the named conditions. Implementations live in the indicated package; all constructors use `apierror.New(code, msg)` and mark retryable where noted.

| Sentinel | Package | Code | Retryable | Meaning |
|---|---|---|---|---|
| `ErrKeyNotFound` | `api/cluster/kv_errors.go` | NotFound | — | Key does not exist in backend |
| `ErrLeaseNotFound` | `api/cluster/kv_errors.go` | NotFound | — | Lease ID unknown |
| `ErrLeaseExpired` | `api/cluster/kv_errors.go` | Invalid | — | Lease already expired |
| `ErrVersionMismatch` | `api/cluster/kv_errors.go` | Invalid | — | CAS precondition failed |
| `ErrKVClosed` | `api/cluster/kv_errors.go` | Unavailable | — | Backend shut down |
| `ErrNoQuorum` | `api/cluster/strong_errors.go` | Unavailable | — | Raft cannot commit (quorum lost) |
| `ErrNotLeader` | `api/cluster/strong_errors.go` | Unavailable | — | Local node is not the leader |
| `ErrLeaderUnreachable` | `api/cluster/strong_errors.go` | Unavailable | — | Leader lost mid-operation |
| `ErrStaleEpoch` | `api/cluster/strong_errors.go` | Invalid | — | Fencing epoch is behind current |
| `ErrClusterUnformed` | `api/cluster/strong_errors.go` | Unavailable | — | Cluster not yet bootstrapped |
| `ErrStrictTimeout` | `api/cluster/strong_errors.go` | Unavailable | **yes** | `waitFollowersApply` did not confirm all reachable peers in time (§8.7) |
| `ErrSingletonHeldElsewhere` | `api/cluster/strong_errors.go` | AlreadyExists | **yes** | Singleton lease held by another node (§13) |
| `ErrNameTaken` | `api/names/errors.go` | AlreadyExists | **yes** | Name already registered in scope |
| `ErrNameNotFound` | `api/names/errors.go` | NotFound | — | Lookup missed |
| `ErrNotJoined` | `api/pg/errors.go` (PR 241) | NotFound | — | `Leave` called by non-member |
| `ErrPreBootstrapPeerTimeout` | `api/cluster/bootstrap_errors.go` | Unavailable | — | Guardrail 1 (§16.1) could not contact all voters |
| `ErrBootstrapLockContended` | `api/cluster/bootstrap_errors.go` | Unavailable | **yes** | Guardrail 2 lock held by another node; join-existing once winner finishes |

**Error contract:** callers must use `errors.Is(err, ErrX)` or `apierror.Kind(err) == apierror.Y`. Implementations must wrap underlying errors to preserve sentinel identity. Every error referenced anywhere in this spec must appear in one of the error files named above — if a new error shows up in prose or pseudocode without a declaration here, that is a spec bug.

### 3.3 `api/store/store.go` — User-Level Store (Existing, Unchanged)

The existing `store.Store`, `store.Scanner`, `store.Atomic` interfaces in `api/store/store.go` are NOT replaced. They serve a different purpose: **user-level application storage with registry.ID keys and payload.Payload values**. They wrap `cluster.KV` instances via encoding/decoding adapters.

The `api/store/` interface is for user Lua code that stores application data (sessions, config, app state). The `api/cluster/kv.go` interface is for coordination-layer primitives and system services.

Adapter layer: `system/store/clusterkv/` (new) wraps a `cluster.KV` to implement `store.Store` + `store.Scanner` + `store.Atomic`. Encoding:
- Key: `registry.ID.String()` (existing)
- Value: msgpack-encoded `payload.Payload` (using `payload.Transcoder`)

This adapter is how `service/store/memory/` and `service/store/sql/` can be unified in the future — by making them `cluster.KV` backends first. Not required for v1.

---

## 4. Eventual KV

### 4.1 Semantics

- **Writes:** local apply + async broadcast to all reachable nodes via internode mesh.
- **Reads:** local from atomic snapshot, O(1), always non-blocking.
- **Conflict resolution:** last-write-wins by version. Concurrent writes on partitioned sides produce conflicts on healing; the higher version wins.
- **Replication:** eventually consistent. After a write completes locally, all reachable nodes converge within bounded time (typically ms).
- **Uniqueness:** NOT guaranteed. `SetIfAbsent` succeeds locally even if another node has the key. Conflict resolved on healing.
- **Scope:** multiple named instances coexist on each node. Each instance has its own namespace and replication stream.

### 4.2 Implementation: `system/kv/` (Partially Exists)

Current files (exist):
- `system/kv/state.go` — mutable state + immutable snapshot
- `system/kv/lease.go` — lease handles + min-heap for TTL expiry
- `system/kv/service.go` — event loop, lock-free reads, event bus watch
- `system/kv/watcher.go` — event bus subscriber with prefix filtering
- `system/kv/service_test.go` — 23 tests passing

New files to add:
- `system/kv/replication.go` — eventual replication over internode mesh
- `system/kv/host.go` — registers as relay host, handles incoming replication messages
- `system/kv/config.go` — per-instance config (read-only defaults injected at construction)

### 4.3 Replication Protocol

Follows the PG pattern from PR 241 (`system/pg/protocol.go`). The shared transport is extracted into `system/dds/` (§6).

**Topics (inter-node messages):**
- `dds.discover` — "I exist, send me your state"
- `dds.sync` — "here is my full state"
- `dds.delta` — "here is an incremental change"
- `dds.tombstone_gc` — "tombstones up to version N can be deleted"

**Discover flow:**
1. On `cluster.NodeJoined` event, send `dds.discover` to new node.
2. New node responds with `dds.sync` containing full state snapshot.
3. Local node merges the remote state (LWW per key, tombstones win over live entries with higher version).

**Delta flow:**
1. Local write completes, event loop publishes a delta record.
2. Broadcast goroutine sends `dds.delta` to all known remote nodes in parallel, non-blocking (fire-and-forget with retry on failure).
3. Remote nodes apply the delta if local version is lower.

**Tombstones:** deleted entries become tombstones with a deletion version. Tombstones are kept for a grace period (default 24h) to ensure all partitioned nodes observe the deletion before the tombstone is GCed. `dds.tombstone_gc` propagates the high-water-mark version for garbage collection; each node independently GCs tombstones below this mark.

**Failure handling:** if a remote node is unreachable during broadcast, the delta is dropped (no persistent queue). On next `dds.discover` from the remote node, it receives the full current state which includes all missed deltas.

### 4.4 `api/service/kv/config.go` — User-Facing Config

```go
// Registry kind for user-declared eventual KV hosts.
const KVHost registry.Kind = "kv.host"

// Config for a user-declared eventual KV instance.
type Config struct {
    Lifecycle    supervisor.LifecycleConfig `json:"lifecycle"`
    Replication  cluster.ReplicationConfig  `json:"replication"`
    MaxKeys      int                        `json:"max_keys"`       // 0 = unlimited
    MaxValueSize int                        `json:"max_value_size"` // bytes, 0 = unlimited
}

// cluster.ReplicationConfig is shared between kv.host and pg.host.
// Defined in api/cluster/replication.go (new).
type ReplicationConfig struct {
    QueueSize       int `json:"queue_size"`        // event loop buffer (default 256)
    SyncBatchSize   int `json:"sync_batch_size"`   // entries per full sync message (default 1000)
    BroadcastBuffer int `json:"broadcast_buffer"`  // outgoing delta buffer (default 64)
}
```

### 4.5 Registry Entry Example

```yaml
- id: "kv:sessions"
  kind: "kv.host"
  max_keys: 1000000
  replication:
    queue_size: 512
  lifecycle:
    auto_start: true

- id: "kv:feature-flags"
  kind: "kv.host"
  max_keys: 10000
  max_value_size: 4096
  lifecycle:
    auto_start: true
```

Each entry creates:
1. A `system/kv.Service` instance (event loop + state + replication).
2. A relay host registration with `HostID = entry.ID.String()`.
3. A supervisor service entry (for lifecycle).
4. A resource provider entry (for Lua acquisition).
5. A security-gated Lua handle via `kv.open("kv:sessions")`.

### 4.6 Service Manager: `service/kv/manager.go` (New)

Follows the §2.2 host-manager pattern. `Add(entry)` constructs a `systemkv.Service` from the decoded `kvapi.Config`, wraps it in a `systemkv.Host` and registers it with internode under `HostID = entry.ID.String()`, then fires supervisor and resource registration events (same wire shape as `service/host/manager.go`). `Remove` tears the instance down symmetrically. Nothing in this manager is kv-specific beyond the concrete types.

### 4.7 Lease Semantics For Eventual KV

The eventual KV implements `cluster.Leasable` (`GrantLease`, `GetLease`, `SetWithLease`, `SetIfAbsentWithLease`) with semantics adapted to eventual consistency.

**Lease record shape (in-memory, per-instance):**

```go
type eventualLease struct {
    id         cluster.LeaseID
    ttl        time.Duration
    ownerNode  pid.NodeID           // node that granted the lease
    createdAt  time.Time            // local wall clock at grant
    expiresAt  time.Time            // createdAt + ttl, refreshed on KeepAlive
    keys       map[string]struct{}  // attached keys
    done       chan struct{}        // closed when lease expires or is revoked
    revoked    atomic.Bool
}
```

**`GrantLease(ctx, ttl)`:**
1. Generate a unique `LeaseID` (local counter + node ID, e.g., `"node-a-42"`).
2. Create `eventualLease` with `expiresAt = now + ttl`.
3. Broadcast a `lease.grant` DDS message to all reachable nodes so they create a shadow copy of the lease metadata. This is eventual — if a remote node misses the broadcast, it will receive the lease state on next DDS sync.
4. Start a local keepalive timer. The grantor node is responsible for renewing the lease. Other nodes merely track the lease record; they do NOT renew it.
5. Return a `cluster.Lease` handle wrapping `eventualLease`.

**`GetLease(ctx, id)`:**
1. Look up the lease in the local map.
2. If found AND `expiresAt > now` → return a handle. The `Done()` channel is the lease's own `done` channel.
3. If found AND `expiresAt <= now` → return `ErrLeaseExpired`. The lease's `done` channel is already closed.
4. If not found → return `ErrLeaseNotFound`. This can happen if DDS has not replicated the lease to this node yet, or if the lease genuinely does not exist.

**Eventual-specific caveat:** `GetLease` on a non-grantor node returns a read-only view. Calling `KeepAlive` on such a view is a no-op locally — the actual renewal must happen on the grantor node. This is fine for the singleton restart case (§13.2) because the restarting node IS the grantor (it's adopting its own previous lease).

**`SetWithLease(key, value, leaseID)`:**
1. Verify the lease exists and is not expired.
2. Local write to the KV state.
3. Register the key in the lease's `keys` set.
4. Broadcast the put via DDS as usual; include the `LeaseID` in the delta so remote replicas also attach the key to their shadow lease record.

**Lease expiry behavior (eventual):**
When a lease expires (grantor's keepalive timer fires without renewal), the grantor:
1. Marks the lease as revoked.
2. Deletes all attached keys from local state.
3. Broadcasts `lease.expired` via DDS so all remote replicas delete their copies of the attached keys.
4. Closes the lease's `done` channel.

If the grantor node dies, the lease is not explicitly expired by other nodes — it will become stale and be cleaned up during the next DDS sync when the dead node's absence is detected. This is acceptable because eventual KV makes no uniqueness guarantees; slightly stale keys are the nature of the tier.

**For strict uniqueness-bound singletons**, use the strong KV (§8), which has different lease semantics (next section).

---

## 5. Process Groups (pg.host)

### 5.1 Current State (PR 241)

PR 241 (`feature/pg-process-groups`) implements a single-instance PG service. The spec here supersedes it with multi-instance support via the `pg.host` registry kind. See `feature/pg-process-groups` PR review comment for the migration path.

### 5.2 Semantics (Unchanged From PR 241)

- **Join:** a process joins a named group. Same process can join multiple times (multi-join).
- **Leave:** removes one join occurrence. Returns `ErrNotJoined` if not a member.
- **Members:** returns all processes in the group across all reachable nodes.
- **LocalMembers:** returns only processes on the local node.
- **WhichGroups:** enumerates all groups with at least one member.
- **Broadcast:** sends a message to all group members. Broadcast to local members only is a separate API.
- **Monitor/Events:** subscribe to join/leave events for a specific group or all groups.
- **Auto-cleanup:** when a process dies, topology fires exit events, PG removes the process from all its groups.

### 5.3 Multi-Instance Support

PR 241 hardcodes `HostID = "pg"` and creates a singleton service at boot. This spec replaces that with registry-driven instances via the §2.2 host-manager pattern.

**Migration:**
1. `api/pg/pg.go` — keep `ProcessGroups` unchanged. Deprecate the singleton context accessor (`GetProcessGroups`) to return the default instance.
2. `system/pg/service.go` — parameterize `HostID` and `Service.name` (currently hardcoded `"pg"`).
3. `api/service/pg/config.go` — new file defining `Config` and `PGHost registry.Kind = "pg.host"`.
4. `service/pg/manager.go` — host-manager from §2.2.
5. `runtime/lua/modules/pg/module.go` — add `pg.open(name)` per the §2.2 Lua pattern.

### 5.4 Registry Entry Example

```yaml
- id: "pg:chat"
  kind: "pg.host"
  lifecycle:
    auto_start: true

- id: "pg:presence"
  kind: "pg.host"
  lifecycle:
    auto_start: true
```

### 5.5 Lua API (Updated)

Follows the §2.2 `*.open` pattern. Instance method surface:

```lua
local chat = pg.open("pg:chat")       -- security-gated, see §2.2
chat:join("room:123")
chat:broadcast("room:123", "message", {topic = "new_msg"})
local members = chat:get_members("room:123")
```

Each `pg.open(id)` is a separate resource, so operators can apply different security policies per named instance.

### 5.6 Config

```go
const PGHost registry.Kind = "pg.host"

type Config struct {
    Lifecycle   supervisor.LifecycleConfig `json:"lifecycle"`
    Replication cluster.ReplicationConfig  `json:"replication"`
}
```

---

## 6. Shared Replication Transport: `system/dds/`

### 6.1 Purpose

PG and Eventual KV share the same replication pattern (discover/sync/delta over relay mesh). Instead of duplicating the protocol, extract it into `system/dds/` (distributed data sync) as a reusable behavior. Both services implement callbacks; the transport handles message dispatch, node tracking, and failure handling.

### 6.2 Interface

```go
// api/cluster/dds.go (new)

// ReplicationTransport provides discover/sync/delta-broadcast over the
// internode mesh. Each consumer (PG, Eventual KV) instantiates one
// transport per scope and implements the Callbacks interface.
type ReplicationTransport interface {
    // Start begins the transport. Subscribes to cluster events and
    // initiates discover for existing nodes.
    Start(ctx context.Context) error

    // Stop shuts down the transport gracefully.
    Stop(ctx context.Context) error

    // BroadcastDelta sends an incremental change to all known remote
    // nodes. Non-blocking; failures are logged and retried on next
    // discover.
    BroadcastDelta(msg *relay.Message) error

    // RegisteredNodes returns the list of remote nodes currently in
    // the replication group.
    RegisteredNodes() []pid.NodeID
}

// ReplicationCallbacks is implemented by the service consuming the
// transport (PG, KV).
type ReplicationCallbacks interface {
    // EncodeState serializes the consumer's local state for a full sync
    // to a remote node. Called when a remote node requests sync.
    EncodeState() (*relay.Message, error)

    // ApplySync merges incoming full state from a remote node. Called
    // when this node receives a sync message.
    ApplySync(fromNode pid.NodeID, msg *relay.Message) error

    // ApplyDelta merges an incremental update from a remote node.
    ApplyDelta(fromNode pid.NodeID, msg *relay.Message) error

    // OnNodeLeft handles cleanup when a remote node departs.
    OnNodeLeft(nodeID pid.NodeID) error
}
```

### 6.3 Implementation

`system/dds/transport.go` (new) — implements `ReplicationTransport`. Logic migrated from `system/pg/protocol.go` (PR 241):
- Subscribe to `cluster.NodeJoined` / `cluster.NodeLeft` events.
- On `NodeJoined`, send `discover` via relay.
- On receiving `discover`, call `callbacks.EncodeState()` and send `sync` via relay.
- On receiving `sync`, call `callbacks.ApplySync(fromNode, msg)`.
- On receiving `delta`, call `callbacks.ApplyDelta(fromNode, msg)`.
- On `NodeLeft`, call `callbacks.OnNodeLeft(nodeID)`.

`system/dds/host.go` — implements `relay.Receiver` for incoming replication messages. Each named scope (e.g., "pg:chat", "kv:sessions") registers its own relay host with `HostID = scope.ID.String()`. Messages from other nodes targeted at that host are dispatched by topic (`dds.discover`, `dds.sync`, `dds.delta`) to the transport callbacks.

### 6.4 Integration With PG

`system/pg/service.go` becomes a `ReplicationCallbacks` implementer:

```go
func (s *Service) EncodeState() (*relay.Message, error) {
    // Serialize s.state.allLocalPids() as a sync message
}

func (s *Service) ApplySync(fromNode pid.NodeID, msg *relay.Message) error {
    // Decode state, call s.state.syncRemote(fromNode, state)
}

func (s *Service) ApplyDelta(fromNode pid.NodeID, msg *relay.Message) error {
    // Dispatch by topic: join / leave / etc.
}

func (s *Service) OnNodeLeft(nodeID pid.NodeID) error {
    // s.state.removeNode(nodeID)
}
```

`system/pg/protocol.go` is simplified — the transport layer is gone, replaced by calls to `transport.BroadcastDelta()`.

### 6.5 Integration With Eventual KV

`system/kv/service.go` adds the same `ReplicationCallbacks` implementation. The callbacks operate on `state.go`'s map of entries instead of PG's group state.

---

## 7. Cluster Service Addressing

### 7.1 The Problem

PR 241's PG uses synthetic PIDs to route inter-node service messages:

```go
func pgPID(nodeID pid.NodeID) pid.PID {
    return pid.PID{Node: nodeID, Host: "pg", UniqID: "pg"}
}
```

These are not real processes. They pollute the PID space. Topology could accidentally monitor them. They conflate service routing with process addressing.

### 7.2 The Fix: `NewServicePackage`

`api/relay/relay.go` adds a constructor for service-level messages:

```go
// NewServicePackage creates a relay.Package targeted at a host on a node,
// not a specific process. Used for cluster service routing (DDS
// replication, Raft transport, etc.).
func NewServicePackage(
    sourceNode pid.NodeID, sourceHost pid.HostID,
    targetNode pid.NodeID, targetHost pid.HostID,
    topic string, payloads ...payload.Payload,
) *Package {
    return &Package{
        Source: pid.PID{Node: sourceNode, Host: sourceHost}, // empty UniqID
        Target: pid.PID{Node: targetNode, Host: targetHost},
        Messages: []*Message{{Topic: topic, Payloads: payloads}},
    }
}
```

Verified: `system/relay/node.go:71-86` routes purely by `pkg.Target.Host` and `pkg.Target.Node`. The `UniqID` is ignored for host-level dispatch. `cluster/internode/codec.go` serializes PIDs via `p.String()` which handles empty UniqID correctly (format: `{node@host|}`).

PG, DDS, and the Raft transport use `NewServicePackage` instead of fabricating process PIDs.

---

## 8. Strong KV (Raft-Backed)

### 8.1 Decision Rationale

After adversarial review of a custom quorum protocol (see §17 for the review transcript), the design is:

- **Use `hashicorp/raft` for the strong KV consensus.**
- **Implement a custom `raft.Transport` over the existing internode mesh.**
- **Single Raft group per cluster handles all strong coordination state.**
- **Multi-Raft (one group per namespace) is future work.**

Reasoning:
1. `hashicorp/raft` is production-validated in Vault, Nomad, Consul, rqlite, NATS Streaming, InfluxDB Enterprise.
2. Rolling our own consensus is a year of engineering for known-buggy results.
3. Custom transport over existing mesh is a supported pattern (Transport interface exists for this).
4. One Raft group keeps operational complexity low. Multi-Raft is an optimization for throughput, not correctness.

### 8.2 Package Layout: `system/raftkv/` (New)

```
system/raftkv/
├── service.go      # top-level Service (supervisor.Service)
├── fsm.go          # raft.FSM implementation (key-value state machine)
├── transport.go    # raft.Transport over internode mesh
├── host.go         # relay.Receiver for incoming Raft RPCs
├── storage.go      # LogStore + StableStore (bbolt-backed)
├── snapshot.go     # FSMSnapshot + FileSnapshotStore setup
├── bootstrap.go    # initial cluster bootstrap logic
├── autopilot.go    # voter auto-replacement
├── leader.go       # leader state tracking, forwarding
├── kv.go           # cluster.KV interface wrapper around the FSM
└── superstrong.go  # sync-all-reachable write mode for name registry
```

### 8.3 FSM Design: `system/raftkv/fsm.go`

The FSM holds the current state of the strong KV. Applies log entries deterministically in order. Snapshot returns a point-in-time copy.

```go
// Op types applied to the FSM.
type opType int

const (
    opSet opType = iota
    opDelete
    opSetIfAbsent
    opCompareAndSwap
    opSetWithLease
    opSetIfAbsentWithLease
    opGrantLease
    opRevokeLease
    opRenewLease

    // opDeleteIfOwner is a CAS-style delete used by the name registry
    // (§9). The FSM deletes the entry ONLY IF the stored value contains
    // an owner record matching (ExpectedOwnerPID, ExpectedOwnerEpoch).
    // This prevents delayed topology exit events from clobbering a
    // newer registration that took over the same name.
    //
    // See §9.4 for the name-registry value shape and §9.5 for the
    // race-analysis that motivates this op type.
    opDeleteIfOwner
)

// Op is the serialized command applied to the FSM.
type op struct {
    Type            opType
    Key             string
    Value           []byte
    ExpectVer       cluster.Version
    LeaseID         cluster.LeaseID

    // LeaseTTLMs is the nominal TTL for a new lease. Used ONLY in
    // opGrantLease. It is NOT used to compute expiry on followers.
    LeaseTTLMs      int64

    // LeaseExpiresAtUnixNanos is the absolute expiry time as decided
    // by the leader at grant time. Encoded as unix nanoseconds. Every
    // follower uses this exact value when applying the log entry —
    // they do NOT add TTL to their local clock.
    //
    // This is the ONLY source of lease expiry. A follower that applies
    // this log entry 5 minutes late still sees the same expiry time,
    // so if the lease has elapsed by the time it's applied, the lease
    // is already expired when it's created — which is correct.
    LeaseExpiresAtUnixNanos int64

    // Owner-scoped delete fields. Used ONLY by opDeleteIfOwner. The
    // FSM compares the stored nameEntry.PID + nameEntry.OwnerEpoch
    // against these expected values and applies the delete only if
    // BOTH match. Mismatch is a silent no-op (not an error) — the
    // caller's expected owner has already been replaced by a newer
    // registration.
    ExpectedOwnerPID   pid.PID
    ExpectedOwnerEpoch uint64

    Epoch      uint64     // fencing epoch (deprecated: use FSM.epoch stamped on Apply)
    Origin     pid.NodeID // originating node (for lease ownership tracking)
}

// FSM implements raft.FSM.
type FSM struct {
    mu     sync.RWMutex
    state  map[string]*fsmEntry
    leases map[cluster.LeaseID]*fsmLease
    epoch  uint64 // global fencing epoch, incremented on every write
    log    *zap.Logger
}

type fsmEntry struct {
    value   []byte
    version cluster.Version
    leaseID cluster.LeaseID
    epoch   uint64
}

type fsmLease struct {
    id        cluster.LeaseID
    ttl       time.Duration // original nominal TTL (for KeepAlive decisions)

    // expiresAtUnixNanos is the absolute expiry time from the log entry.
    // Every replica has the same value. Expiry decisions compare this
    // against wall-clock time. Clock skew across nodes is bounded by
    // NTP — the clock-skew tolerance is documented in §8.3.1 below.
    expiresAtUnixNanos int64

    ownerNode pid.NodeID
    keys      map[string]struct{}
}

func (f *FSM) Apply(log *raft.Log) interface{} {
    var cmd op
    if err := msgpack.Unmarshal(log.Data, &cmd); err != nil {
        return &applyResult{Err: err}
    }

    f.mu.Lock()
    defer f.mu.Unlock()

    f.epoch++ // every applied entry advances the fencing epoch

    switch cmd.Type {
    case opSet:
        return f.applySet(cmd)
    case opDelete:
        return f.applyDelete(cmd)
    case opSetIfAbsent:
        return f.applySetIfAbsent(cmd)
    case opCompareAndSwap:
        return f.applyCompareAndSwap(cmd)
    case opSetWithLease:
        return f.applySetWithLease(cmd)
    case opSetIfAbsentWithLease:
        return f.applySetIfAbsentWithLease(cmd)
    case opGrantLease:
        return f.applyGrantLease(cmd)
    case opRevokeLease:
        return f.applyRevokeLease(cmd)
    case opRenewLease:
        return f.applyRenewLease(cmd)
    case opDeleteIfOwner:
        return f.applyDeleteIfOwner(cmd)
    default:
        return &applyResult{Err: fmt.Errorf("unknown op type: %d", cmd.Type)}
    }
}

func (f *FSM) Snapshot() (raft.FSMSnapshot, error) {
    f.mu.RLock()
    defer f.mu.RUnlock()
    return &fsmSnapshot{
        state:  deepCopyState(f.state),
        leases: deepCopyLeases(f.leases),
        epoch:  f.epoch,
    }, nil
}

func (f *FSM) Restore(r io.ReadCloser) error {
    // Decode snapshot and replace state atomically.
    // MUST also reset lastAppliedIdx to the snapshot's index, so
    // ApplyCheck waiters that were blocking on a higher index
    // correctly observe "we jumped ahead" via the log-replay notifier.
}
```

**`applyResult` struct (shared return type for all FSM operations):**

Every `Apply` dispatch path returns an `*applyResult`. The same type is
consumed by `raft.Apply().Response()` casts in the caller (leader-side)
and by `handleApplyCheck` responses (follower-side). Fields are
populated based on op type; unused fields are zero.

```go
// applyResult is the deterministic return value from every FSM
// Apply dispatch. Populated by the FSM and consumed by ApplyStrict
// (§8.7), Register (§9.4), and other callers that need to know the
// outcome of a committed log entry.
type applyResult struct {
    // OK is true if the op's CAS-style precondition succeeded. For
    // opSet / opDelete (unconditional), OK is always true. For
    // opSetIfAbsent, opCompareAndSwap, opDeleteIfOwner, OK is false
    // when the precondition fails (key already exists, version mismatch,
    // owner mismatch). A false OK is NOT an error — it's a successful
    // log apply that reports "precondition did not hold."
    OK bool

    // Version is the FSM version of the affected entry AFTER the
    // apply. Monotonically increasing. Equal to f.epoch at apply time.
    Version cluster.Version

    // OwnerEpoch is the stamped epoch for name registry entries
    // (opSetIfAbsent on /names/* keys). Callers use this as the
    // CAS key for later opDeleteIfOwner.
    OwnerEpoch uint64

    // Err is non-nil only for genuine failures (malformed op payload,
    // FSM corruption, missing lease referenced by opSetWithLease).
    // Precondition failures use OK=false, NOT Err.
    Err error
}
```

**`FSM.lastAppliedIdx` and the apply notifier (exemption to "no side effects"):**

The FSM tracks the last-applied log index so that `ApplyCheck` (§8.7.1)
can block until a follower catches up to a specific commit index. This
is implemented via a field on the FSM and a per-index signal:

```go
type FSM struct {
    mu     sync.RWMutex
    state  map[string]*fsmEntry
    leases map[cluster.LeaseID]*fsmLease
    epoch  uint64
    log    *zap.Logger

    // lastAppliedIdx is the Raft log index of the most recent entry
    // applied to this FSM. Updated by Apply() under f.mu. Read by
    // ApplyCheck waiters via LastAppliedIdx() (atomic-safe under
    // RLock).
    lastAppliedIdx uint64

    // appliedCond is signaled after every successful Apply. ApplyCheck
    // waiters block on this condition and re-check lastAppliedIdx when
    // woken. Broadcast (not Signal) so multiple waiters waiting for
    // different indices all wake on every apply and re-check.
    appliedCond *sync.Cond // uses f.mu as its Locker
}

// LastAppliedIdx returns the most recently applied log index. Safe to
// call concurrently with Apply (takes f.mu.RLock under the hood).
func (f *FSM) LastAppliedIdx() uint64 {
    f.mu.RLock()
    defer f.mu.RUnlock()
    return f.lastAppliedIdx
}

// WaitAppliedAtLeast blocks until lastAppliedIdx >= target OR ctx is
// cancelled. Used by the ApplyCheck receiver (§8.7.1). Returns nil if
// the target was reached, ctx.Err() on cancellation.
func (f *FSM) WaitAppliedAtLeast(ctx context.Context, target uint64) error {
    // Use a helper goroutine to convert ctx.Done() into a cond
    // broadcast, so we can use cond.Wait without losing ctx semantics.
    done := make(chan struct{})
    defer close(done)
    go func() {
        select {
        case <-ctx.Done():
            f.mu.Lock()
            f.appliedCond.Broadcast()
            f.mu.Unlock()
        case <-done:
        }
    }()

    f.mu.Lock()
    defer f.mu.Unlock()
    for f.lastAppliedIdx < target {
        if ctx.Err() != nil {
            return ctx.Err()
        }
        f.appliedCond.Wait() // releases f.mu, re-acquires on wake
    }
    return nil
}
```

And the Apply path updates the index + signals the condition:

```go
func (f *FSM) Apply(log *raft.Log) interface{} {
    var cmd op
    if err := msgpack.Unmarshal(log.Data, &cmd); err != nil {
        return &applyResult{Err: err}
    }

    f.mu.Lock()
    defer f.mu.Unlock()

    f.epoch++
    f.lastAppliedIdx = log.Index // update tracker

    var result *applyResult
    switch cmd.Type {
    case opSet:
        result = f.applySet(cmd)
    // ... other cases ...
    default:
        result = &applyResult{Err: fmt.Errorf("unknown op type: %d", cmd.Type)}
    }

    // Signal any ApplyCheck waiters. This is the ONLY side effect
    // permitted inside Apply (see key design point below). It is
    // idempotent (cond.Broadcast is safe to call repeatedly) and
    // deterministic (doesn't depend on wall clock or random state).
    f.appliedCond.Broadcast()

    return result
}
```

**Key design points:**

- FSM operations are **deterministic**. Any non-determinism (time, randomness) must come from the log entry itself. In particular:
  - `LeaseExpiresAtUnixNanos` is computed by the **leader** at grant time (`now + TTL`) and written into the log entry. Followers copy this value verbatim. They do NOT add TTL to their own clock. A follower applying a log entry 5 minutes late still sees the same absolute expiry, so a lease whose expiry has already elapsed is already expired when it's created. This is the correct behavior.
  - No randomness, no timestamps inside Apply. All time-sensitive values come from the log entry payload.
- **No side effects in Apply EXCEPT the appliedCond broadcast.** Watch notifications, event bus dispatches, relay sends all happen OUTSIDE Apply, from a separate goroutine that observes applied entries via a dedicated `appliedCh` on the FSM. Apply only mutates the in-memory state AND signals the cond var. The appliedCond broadcast is exempted from the "no side effects" rule because:
  1. It is deterministic (independent of clock/random).
  2. It is idempotent across log replay (waking up nobody if no waiters).
  3. It is required for ApplyCheck (§8.7.1) to work at all.
- The `appliedCh` for external observers (watch, event bus) is a separate mechanism — a buffered channel populated AFTER Apply returns from a goroutine that reads Raft's `FSMApplied` events. That goroutine IS subject to the "no side effects in Apply" rule because it runs outside the Apply call itself. This gives us both: safe external fan-out AND in-band waiter notification.
- **Fencing epoch** advances monotonically on every applied entry. It is maintained inside `FSM.epoch` and is the global sequence number used by fencing checks in downstream systems (e.g., Postgres for workflow writes). Every Apply increments it; snapshot restore reads it back from the snapshot header.
- **Lease expiry** is handled by a dedicated goroutine on the leader only. It scans `f.leases` for entries with `expiresAtUnixNanos <= now` and submits `opRevokeLease` log entries. Non-leader nodes do NOT scan — if leadership changes, the new leader's scanner picks up where the old one left off (the scan is idempotent). The scanner runs at most once per 500ms.

### 8.3.1 Clock Skew Tolerance

Lease expiry compares `expiresAtUnixNanos` against wall-clock time on each node. NTP typically keeps nodes within ~10ms of each other; virtualized environments can drift to ~100ms. The spec requires:

1. **Leader grant TTL floor:** minimum lease TTL is 1 second. Anything shorter is rejected. This bounds the relative impact of clock skew — a 10ms skew on a 1s lease is 1% error, acceptable.
2. **Leader-side grace window:** the leader's lease-expiry scanner uses `now - clockSkewGrace` instead of `now` when checking for expiry, where `clockSkewGrace` defaults to 200ms. This prevents the leader from revoking a lease that a follower still considers valid due to minor skew.
3. **NTP monitoring:** operators are expected to run NTP. Clock skew > 500ms between voters is flagged as a health metric (`raftkv.clock_skew_ms`) and surfaced to alerting.
4. **Lease-bound operations must use fencing epochs:** any downstream write gated by a lease also includes the fencing epoch (§15). The epoch is monotonic and clock-independent — it's the final safety net when clock skew causes ambiguity.

Clock skew cannot cause split-brain, dual holders, or lost writes — the fencing epoch on downstream writes prevents stale operations from committing. The worst case is a lease that expires slightly before or after its nominal TTL on different nodes.

### 8.4 Transport Design: `system/raftkv/transport.go`

Implements `raft.Transport` over the internode mesh. Registers as a relay host with `HostID = "raft"`. Incoming Raft RPCs arrive as relay messages; outgoing Raft RPCs are sent via relay.

```go
type internodeTransport struct {
    localAddr  raft.ServerAddress
    router     relay.Router
    consumerCh chan raft.RPC
    rpcPool    sync.Pool
    // ... additional state for request/response correlation
}

// raft.Transport interface
func (t *internodeTransport) Consumer() <-chan raft.RPC { return t.consumerCh }
func (t *internodeTransport) LocalAddr() raft.ServerAddress { return t.localAddr }

func (t *internodeTransport) AppendEntries(id raft.ServerID, target raft.ServerAddress,
    args *raft.AppendEntriesRequest, resp *raft.AppendEntriesResponse) error {
    // 1. Serialize args as relay.Message payload
    // 2. Use NewServicePackage to target "raft" host on target node
    // 3. Send via relay, wait for response via correlation ID
    // 4. Deserialize response into resp
}

func (t *internodeTransport) RequestVote(
    id raft.ServerID, target raft.ServerAddress,
    args *raft.RequestVoteRequest, resp *raft.RequestVoteResponse,
) error {
    // Symmetric to AppendEntries: serialize args, send via relay,
    // wait for response via correlation ID. RequestVote payloads are
    // small (hundreds of bytes) so a single relay package is fine.
}

// InstallSnapshot streams an FSM snapshot to a follower. Unlike
// AppendEntries / RequestVote, snapshots can be MANY megabytes and MUST
// use a chunked streaming protocol, NOT a single relay package.
//
// The hashicorp/raft API passes an io.Reader that yields the snapshot
// bytes. We wrap it in a chunk streamer that:
//   1. Establishes a dedicated streaming channel to the target node
//      via relay host "raftkv.snapshot" (separate from the main
//      "raftkv" host to avoid interfering with normal RPC traffic).
//   2. Sends a SnapshotBegin message with total size, term, index,
//      peers config, and a chunk sequence starting at 0.
//   3. Reads the source io.Reader in fixed-size chunks (default 256 KB)
//      and sends each as a SnapshotChunk relay message with
//      increasing sequence numbers. Each chunk is acked by the
//      receiver before the next is sent (backpressure).
//   4. Sends a SnapshotEnd message when the source EOFs.
//   5. Waits for the receiver to report snapshot applied (success
//      ack) OR failure (explicit nack).
//
// The receiver side (in Send(), topic "raft.install_snapshot"):
//   1. On SnapshotBegin, opens a new raft.SnapshotSink via the local
//      raft.Raft.InstallSnapshot() machinery.
//   2. For each SnapshotChunk in sequence, writes to the sink.
//      Out-of-sequence chunks are rejected (receiver requests resend
//      from the last-acked offset).
//   3. On SnapshotEnd, closes the sink and applies the snapshot.
//   4. Reports success or failure back to the sender via ack message.
//
// Chunk flow control:
//   - Sliding window of N outstanding unacked chunks (default 4),
//     configurable via SnapshotWindow.
//   - On ack of chunk K, sender can send chunk K+N.
//   - On timeout (default 30s per chunk), sender retries from last
//     acked offset. After 3 retries, sender fails the snapshot and
//     reports the error back to raft.
//
// Resumption:
//   - If the relay mesh connection is lost mid-transfer, the
//     snapshot is aborted on both sides. Raft retries the full
//     snapshot on the next replication cycle. We do NOT attempt to
//     resume from a partial transfer — too complex for little gain.
//
// Backpressure and congestion:
//   - Snapshot chunks use a LOWER priority queue than AppendEntries
//     on the relay mesh so ongoing replication and elections are not
//     starved by a large snapshot transfer.
//   - The sender blocks on ack for each chunk — it CANNOT fire-and-
//     forget. This is a deliberate departure from DDS delta semantics.
func (t *internodeTransport) InstallSnapshot(
    id raft.ServerID, target raft.ServerAddress,
    args *raft.InstallSnapshotRequest, data io.Reader,
    resp *raft.InstallSnapshotResponse,
) error {
    stream := t.openSnapshotStream(target)
    defer stream.Close()

    if err := stream.SendBegin(args); err != nil {
        return err
    }

    buf := make([]byte, t.cfg.SnapshotChunkSize) // default 256 KB
    var seq uint64
    for {
        n, err := data.Read(buf)
        if n > 0 {
            if sendErr := stream.SendChunk(seq, buf[:n]); sendErr != nil {
                return sendErr
            }
            seq++
        }
        if err == io.EOF {
            break
        }
        if err != nil {
            return err
        }
    }

    if err := stream.SendEnd(); err != nil {
        return err
    }

    // Wait for final ack from receiver with success/failure.
    return stream.AwaitCompletion(resp)
}

func (t *internodeTransport) EncodePeer(id raft.ServerID, addr raft.ServerAddress) []byte {
    // Serialize the raft.ServerID + raft.ServerAddress pair as a
    // relay.NewServicePackage target. Since raftkv addresses are
    // Node IDs (strings), this is just `string(id)`.
    return []byte(id)
}

func (t *internodeTransport) DecodePeer(data []byte) raft.ServerAddress {
    return raft.ServerAddress(data)
}

func (t *internodeTransport) SetHeartbeatHandler(cb func(rpc raft.RPC)) {
    // Raft's heartbeat fast path: when AppendEntries arrives with
    // no entries, it's a pure heartbeat. We bypass the normal
    // consumer channel and invoke cb directly. This reduces
    // heartbeat latency and avoids contention on the consumerCh.
    t.mu.Lock()
    defer t.mu.Unlock()
    t.heartbeatCb = cb
}

func (t *internodeTransport) TimeoutNow(
    id raft.ServerID, target raft.ServerAddress,
    args *raft.TimeoutNowRequest, resp *raft.TimeoutNowResponse,
) error {
    // Send TimeoutNow RPC — used by leadership transfer. Single
    // small relay package, same pattern as RequestVote.
}

// relay.Receiver for incoming Raft RPCs
func (t *internodeTransport) Send(pkg *relay.Package) error {
    for _, msg := range pkg.Messages {
        switch msg.Topic {
        case "raft.append_entries":
            t.handleAppendEntries(msg)
        case "raft.request_vote":
            t.handleRequestVote(msg)
        case "raft.install_snapshot":
            t.handleInstallSnapshot(msg)
        case "raft.timeout_now":
            t.handleTimeoutNow(msg)
        }
    }
    return nil
}
```

**Critical requirements (from codex review, §17):**

1. **Blocking/backpressure semantics:** `AppendEntries` must block when the mesh is congested, not drop messages. The relay mesh already provides this via its connection manager.
2. **Timeout separation:** Raft's heartbeat timeout (typically 150ms) must not be masked by the mesh's retry logic. Retries at the mesh layer can delay election detection and cause unnecessary leader churn. Set mesh retry timeout lower than Raft heartbeat.
3. **PreVote compatibility:** implement `raft.WithPreVote` so disruptive rejoins don't cause term inflation.
4. **AppendEntries pipelining:** hashicorp/raft has an `AppendEntriesPipeline` optimization for batch replication. Support it if possible (reduces latency under load).

### 8.5 Storage: `system/raftkv/storage.go`

Uses `hashicorp/raft-boltdb` for LogStore + StableStore. BoltDB is a file-backed key-value store, already embedded in many hashicorp/raft deployments.

```go
func NewStorage(dataDir string) (raft.LogStore, raft.StableStore, raft.SnapshotStore, error) {
    // raft-boltdb for log and stable store
    boltStore, err := raftboltdb.NewBoltStore(filepath.Join(dataDir, "raft.db"))
    if err != nil {
        return nil, nil, nil, err
    }

    // File-backed snapshot store
    snapshots, err := raft.NewFileSnapshotStore(dataDir, 3, nil)
    if err != nil {
        return nil, nil, nil, err
    }

    return boltStore, boltStore, snapshots, nil
}
```

Configuration:
- Log/stable store path: `{runtime_dir}/raft/raft.db`
- Snapshot path: `{runtime_dir}/raft/snapshots/`
- Snapshot retention: 3 snapshots
- Snapshot threshold: every 8192 log entries (configurable)

### 8.5.1 Lease Semantics For Raft KV

The Raft KV implements `cluster.Leasable` with STRONGER semantics than the eventual KV — leases are replicated through the Raft log, so every voter agrees on lease state and expiry.

**Lease record shape (inside the FSM, deterministically replicated):**

```go
type fsmLease struct {
    id                 cluster.LeaseID
    ttl                time.Duration     // nominal TTL for KeepAlive calculations
    expiresAtUnixNanos int64              // absolute expiry, leader-minted (§8.3)
    ownerNode          pid.NodeID         // the node that granted the lease
    keys               map[string]struct{}// keys bound to this lease
}
```

Every replica has an identical copy of this struct for every active lease. The `expiresAtUnixNanos` is set by the leader in the `opGrantLease` log entry (see §8.3) and is copied verbatim by all followers — no clock drift between replicas.

**`GrantLease(ctx, ttl)`:**
1. Leader-side only: `Apply` a log entry of type `opGrantLease` with `LeaseTTLMs: ttl.Milliseconds()` and `LeaseExpiresAtUnixNanos: time.Now().Add(ttl).UnixNano()`.
2. Non-leader callers forward to the leader via the normal Raft KV forwarding path (§8.6).
3. Once the log entry is committed and applied on this node, the FSM has the new lease in `f.leases[leaseID]`.
4. Return a `Lease` handle whose `Done()` channel is owned by the FSM (see below).

**`GetLease(ctx, id)`:**
1. Lookup `f.leases[id]` from the local FSM snapshot under RLock.
2. If found AND `expiresAtUnixNanos > time.Now().UnixNano()` → construct and return a `Lease` handle.
3. If found AND expired → return `ErrLeaseExpired`. The handle's `done` channel has already been closed by the lease-expiry scanner (§8.3 "Lease expiry" paragraph).
4. If not found → return `ErrLeaseNotFound`.
5. This is a LOCAL read. No Raft round-trip. Safe because the FSM snapshot is the authoritative state after commit.

**`KeepAlive` on a `Lease` handle:**
1. Leader-side: `Apply` a log entry of type `opRenewLease` with the lease ID and `LeaseExpiresAtUnixNanos: time.Now().Add(originalTTL).UnixNano()`.
2. Non-leader: forward to leader.
3. On commit + apply, the FSM updates `f.leases[id].expiresAtUnixNanos` and leaves everything else alone.
4. Return nil on success, `ErrLeaseExpired` if the lease has already expired and been swept.

**`Revoke` on a `Lease` handle:**
1. Leader-side: `Apply` a log entry of type `opRevokeLease` with the lease ID.
2. On commit + apply, the FSM deletes all keys attached to the lease AND removes the lease from `f.leases`.
3. The handle's `done` channel is closed as part of the apply (via the appliedCond notifier) — the signal path is the only side effect permitted in Apply (see §8.3).

**`Done()` channel ownership:**

The `Done()` channel on a `Lease` handle is owned by the FSM (in `fsmLease.done`, a field added to the struct above). When the lease is first granted, the FSM allocates the channel. When the lease expires or is revoked, the Apply path for `opRevokeLease` closes it. All handles returned by `GrantLease` or `GetLease` for the same lease ID share the same underlying channel — closing it once signals all waiters.

**Crash recovery:**

After a restart, the FSM is restored from the snapshot + log replay. Every committed lease is reconstructed with its original `expiresAtUnixNanos`. If the lease has already expired (wall clock has passed `expiresAtUnixNanos`), the lease-expiry scanner detects this during its next 500ms scan and submits an `opRevokeLease` log entry to clean it up properly. The `Done()` channels are re-allocated on restore — any old handles held by user code across a restart are pointing at a zero struct and must be re-acquired via `GetLease`.

**Singleton restart interaction (§13.2):**

The singleton supervisor's restart path reads the lease ID from the strong KV entry's `LeaseID` field (via `Get`), then calls `GetLease(ctx, leaseID)` to adopt the existing handle. Because the FSM still has the lease record (it hasn't expired between the crash and restart — typical backoff is shorter than the TTL), the `GetLease` returns a working handle and the restart completes without a failover.

### 8.6 Leader Identification And Forwarding

```go
// system/raftkv/leader.go

type leaderState struct {
    mu         sync.RWMutex
    leader     raft.ServerAddress
    isLeader   bool
    leaderCh   <-chan bool
    forwardCh  chan forwardRequest
}

// Observe raft.Raft.LeaderCh() and update state.
func (l *leaderState) loop(ctx context.Context, r *raft.Raft) {
    for {
        select {
        case <-ctx.Done():
            return
        case isLeader := <-r.LeaderCh():
            l.mu.Lock()
            l.isLeader = isLeader
            if isLeader {
                l.leader = r.Leader()
            }
            l.mu.Unlock()
            // Broadcast leadership change via event bus
            l.bus.Send(ctx, event.Event{
                System: cluster.System,
                Kind:   cluster.LeaderElected, // or LeaderLost
                Data:   &cluster.LeadershipEvent{IsLeader: isLeader, Leader: l.leader},
            })
        }
    }
}

// Forward a write operation to the current leader.
func (l *leaderState) Forward(ctx context.Context, op op) (applyResult, error) {
    l.mu.RLock()
    leader := l.leader
    l.mu.RUnlock()

    if leader == "" {
        return applyResult{}, cluster.ErrLeaderUnreachable
    }

    // Send the op to the leader's "raftkv" host via relay
    // Wait for response via correlation ID
}
```

Non-voter nodes (clients) forward all strong KV writes to the leader via relay. They learn the leader identity through `cluster.LeaderElected` events.

### 8.7 Super Strong Mode (Name Registry Only)

**Invariant.** When `ApplyStrict` returns success, every node in the **strict reachable view** has applied the log entry to its FSM. A stale read on any of those nodes after `ApplyStrict` returned is a correctness bug. Used only from the name registry (§9); user-facing strong KV writes use regular `Apply`.

**Strict reachable view.** Computed on every `ApplyStrict` call by `Service.StrictReachableView()` as the intersection of:

1. Listed in `raft.Configuration` (voters + non-voting followers — clients are never in the config).
2. Transport's per-peer `LastContact` (§8.7.2) is newer than `ReachableWindow` (default 2s) and the mesh session to that peer is currently live.
3. Peer is NOT in the autopilot exclusion set (probation/quarantine/operator — see §8.9).

Condition 3 is the critical filter. Autopilot demotion alone does not shrink the view — a demoted-but-reachable follower still satisfies `LastContact`. The exclusion set is owned by autopilot and consulted by `StrictReachableView()` on every call.

`StrictReachableView` is a method on `*raftkv.Service`, NOT on `*raft.Raft`. Hashicorp/raft exposes no per-ID `LastContact`, so `Service` relies on the transport, which receives every RPC as a `relay.Receiver` and stores timestamps itself — a clean, hashable source of truth that does not require parsing `raft.Stats()` strings.

**Leader-side flow (`ApplyStrict`).**

```
raft.Apply(op, timeout)                 # standard quorum commit
commitIdx := future.Index()
view := s.StrictReachableView()
waitFollowersApply(ctx, commitIdx, view)   # fan out ApplyCheck RPCs
```

`waitFollowersApply` fans an `ApplyCheck` RPC to each peer in `view` in parallel. On every response, update a `confirmed: map[ServerID]struct{}`; each successful response also calls `autopilot.MarkFollowerHealthy(peerID)` so a peer that blips for one commit is cleared from probation immediately. On `ctx.Done()`, everyone in `view` not in `confirmed` is a laggard. The laggard set is `[]raft.ServerID` (NOT addresses — it must match the autopilot exclusion key, or `Contains` silently misses). On leader loss mid-wait, return `ErrLeaderUnreachable`.

**Error contract.** `ApplyStrict` returns one of:
- `nil` — invariant holds, callers can return success.
- `*strictError{Kind: ErrStrictTimeout, LaggingPeers}` — timeout waiting on ≥1 reachable peer. Raft log entry is already committed (durable), but freshness is unverified. Caller retries or surfaces the failure. Leader feeds `LaggingPeers` to `autopilot.MarkLaggingFollowers` using `errors.As` (never a raw type assertion — a panic here would crash the leader).
- `ErrLeaderUnreachable` — lost leadership during the wait.
- Pass-through `raft.ErrLeadershipLost` / `raft.ErrNotLeader` from the initial `Apply`.

Timeouts are failures, not warnings. Swallowing them defeats the entire point of Super Strong (it exists specifically so name registration can promise zero stale reads on any reachable node).

**Chronic laggards.** The autopilot feedback loop is the mechanism that prevents one slow follower from blocking forever: first strict miss → `probation` + 30s TTL → the NEXT `ApplyStrict` excludes that peer. Sustained misses escalate to full quarantine. Recovery via a subsequent successful `ApplyCheck` clears probation instantly (see §8.9).

### 8.7.1 ApplyCheck RPC

Hashicorp/raft exposes per-peer `LastContact` but NOT per-peer `LastAppliedIndex`. `ApplyCheck` fills that gap as a minimal leader-only probe running over the same `raftkv` relay host as the main transport.

- **Relay host / topics:** `raftkv` · `raft.apply_check` · `raft.apply_check_response`.
- **Request:** `{CorrelationID, CommitIndex, Deadline}` — deadline is the caller's absolute unix-nanos.
- **Response:** `{CorrelationID, Success, AppliedIndex, Error}`.

**Sender (leader).** Allocate a correlation ID, register a pending slot, send via `NewServicePackage(peer.Address, "raftkv", topic)`, block on the pending slot OR `ctx.Done()`. On response, clean up and return `nil`/`applyCheckError`; on ctx expiry, return `ErrStrictTimeout`.

**Receiver (any Raft member).**
1. If not a current Raft member → `{Success: false, Error: "not a member"}`.
2. If InstallSnapshot is in progress (atomic flag set by the snapshot path) → `{Success: false, Error: "snapshot in progress"}`.
3. Else if `fsm.LastAppliedIdx() >= req.CommitIndex` → immediate `{Success: true, AppliedIndex}`.
4. Otherwise call `fsm.WaitAppliedAtLeast(ctx, req.CommitIndex)` (§8.3 `appliedCond`). On success → `{Success: true}`. On deadline → `{Success: false, AppliedIndex, Error: "apply deadline exceeded"}`.

Every request gets a response — the receiver never silently drops. `ApplyCheck` is NOT a replacement for `AppendEntries`; the receiver must already have the entries from normal log replication.

### 8.7.2 RPC → LastContact Coverage

Per the identifier contract, the transport's `lastContact: sync.Map[ServerID, time.Time]` is the single source of reachability truth. Every Raft-level RPC that proves a peer is alive MUST update it on both sides:

| RPC | Sender update | Receiver update |
|---|---|---|
| `raft.append_entries` / response | On successful response | On receipt |
| `raft.request_vote` / response | On successful response | On receipt |
| `raft.install_snapshot` (chunks) | On each chunk ack | On each chunk received |
| `raft.timeout_now` | On successful response | On receipt |
| `raft.apply_check` / response | On successful response | On receipt |

Receivers also record contact on chunk arrival and on every `ApplyCheck` request — even a "deadline exceeded" response counts as proof-of-life. Missing any of these paths causes followers that are actively receiving snapshots or doing leadership transfers to look dead to `StrictReachableView()`.

### 8.8 Bootstrap And Membership

**Bootstrap:**
- On first startup, if the `/cluster/config` key does not exist in the FSM, bootstrap is required.
- Operator sets `bootstrap: true` on exactly one node in config.
- That node calls `raft.BootstrapCluster` with the initial voter list.
- Other nodes join via `raft.AddVoter` (triggered by the leader after bootstrap).

**Membership changes:**
- Handled by `hashicorp/raft`'s `AddVoter` / `RemoveServer` / `AddNonvoter` APIs.
- Each change is a log entry — joint consensus handles safety.
- Autopilot (§8.9) automates voter replacement on detected failures.

**Config path:** cluster config is stored at a well-known key in the FSM itself (`/cluster/config`) and updated via Raft log entries. Operators see the current membership by reading this key.

### 8.9 Autopilot: Voter Health Management

Autopilot is a standalone goroutine inside `system/raftkv/` that drives voter membership changes and an **exclusion set** consumed by `StrictReachableView()` (§8.7). It operates as a state machine with a 1 Hz evaluate loop.

**Inputs.**
- Raft health: `raft.Stats()` + per-voter `LastContact` (authoritative signal; memberlist only provides hints to shorten detection for clean leaves).
- Strict-apply laggards: `MarkLaggingFollowers([]raft.ServerID)` called by `ApplyStrict` whenever an `ApplyCheck` times out.
- Strict-apply recoveries: `MarkFollowerHealthy(raft.ServerID)` called whenever an `ApplyCheck` succeeds for a previously-probationary peer.

**Exclusion set.** Thread-safe map keyed by `raft.ServerID` (addresses are explicitly wrong — the key must match the ID used everywhere else so `Contains` never silently misses). Two automatically-managed kinds:

| Kind | Lifetime | How it is added | How it is cleared |
|---|---|---|---|
| `probation` | `ProbationWindow` (30s) | `MarkLaggingFollowers` on first strict miss | `MarkFollowerHealthy`, or TTL expiry, or escalation |
| `quarantine` | `QuarantineDuration` (30m) | Accumulation of `MaxFlapsBeforeQuarantine` probation hits inside `FlapDetectionWindow`, OR flap detection in evaluate loop | TTL expiry **and** subsequent `ServerStabilizationTime` of sustained health |

Probation survives lossy `ApplyCheck` responses — one cycle of blip and recovery clears it immediately, avoiding the need to wait out the full TTL. Quarantine is resilient to spurious health signals; only TTL + stabilization releases it. A node quarantined twice within 24h requires operator action to auto-promote again; that admin path is deferred to post-v1.

**Guardrails (mandatory).**
- `ServerStabilizationTime` (10s): sustained unhealth required before any action. Healthy ping resets the clock.
- `MembershipCooldown` (60s): minimum interval between membership changes.
- `ConcurrentChangesMax` (1): hard cap on in-flight joint-consensus transitions.
- Flap detection per node: >`MaxFlapsBeforeQuarantine` (3) transitions inside `FlapDetectionWindow` (5m) → quarantine, regardless of current health.

**Outputs.**
- `raft.DemoteVoter` / `raft.RemoveServer` on sustained unhealth.
- `raft.AddVoter` on eligible replacement (only if `CleanupDeadServers` is true and a candidate exists).
- Exclusion-set mutations consumed by `StrictReachableView()`.

**Evaluate loop.** Tick @1Hz. If cooldown active or concurrent changes cap hit → skip. Otherwise collect sustained-unhealthy voters; flapping ones go to quarantine, the rest are demoted one at a time. At most one change per tick.

**Effect on ApplyStrict.** First strict miss on a slow follower → probation, so the **next** `ApplyStrict` excludes it. Recovery via a subsequent `ApplyCheck` success clears probation instantly. Repeated misses escalate to quarantine, and the follower stays out for the full `QuarantineDuration` even if it recovers mid-window.

**Metrics** (required): `raftkv.autopilot.quarantined_nodes`, `excluded_nodes`, `probation_entries`, `flap_events_total`, `membership_changes_rejected_total{reason}`, `strict_probation_triggers_total`, `strict_recoveries_total`.

---

## 9. Name Registry: `system/names/`

### 9.1 Semantics

The name registry is the most safety-critical coordination primitive. It binds human-readable names to process PIDs. Every node must be able to route to a registered name with zero stale lookups.

- **Register:** atomic, cluster-wide unique per scope. Returns success only after ALL reachable nodes have the name in their local replica (Super Strong mode on raftkv).
- **Lookup:** local read, always fresh (for reachable nodes).
- **Unregister:** removes the name. Processes that hold the name receive a name_conflict exit event ONLY if another process takes over the name (which requires Unregister + a new Register in Raft order).
- **Auto-cleanup:** when a process dies, topology fires exit events. The name registry removes the name on the node that owned the process, propagates the removal via Raft.
- **Scopes:** multiple named scopes coexist. Each scope is a prefix in the Raft FSM (`/names/{scope}/{name}`).

### 9.2 Package Layout

```
system/names/
├── service.go      # top-level Service (supervisor.Service)
├── registry.go     # high-level Register/Lookup/Unregister
├── scope.go        # scope (prefix) management
├── monitor.go      # topology integration for process death detection
└── config.go       # config
```

### 9.3 Interface: `api/names/names.go` (New)

```go
// Handle is the opaque token returned by Register. Callers must pass
// it back to Unregister so the delete can CAS on the exact owner
// (PID + epoch) that was bound. Without this, a stale exit event for
// a previous registration could clobber a newer one. (See R6-C1.)
//
// Handles are NOT forgeable from user code — the epoch is stamped by
// the FSM during Apply and the caller only receives it via a successful
// Register return value.
type Handle struct {
    Scope string
    Name  string
    PID   pid.PID
    Epoch uint64 // the OwnerEpoch returned by the FSM at Register time
}

// Registry provides cluster-wide unique name-to-PID mapping.
type Registry interface {
    // Register binds a name to a PID. Returns ErrNameTaken if the name
    // is already held by another process. Returns only after all
    // reachable nodes have applied the registration (Super Strong
    // semantics).
    //
    // On success, returns a Handle that the caller MUST retain and
    // pass to Unregister. The Handle contains the OwnerEpoch stamped
    // by the FSM, which is required for CAS-style delete.
    Register(ctx context.Context, scope, name string, p pid.PID) (Handle, error)

    // Unregister removes a name bound by a prior Register. The Handle
    // is matched against the current FSM entry via CAS — if the entry
    // has been replaced by a newer registration (different PID or
    // different Epoch), Unregister returns nil without touching
    // anything. This is the correct behavior: the registration this
    // Handle refers to is already gone, so from the caller's
    // perspective, Unregister has "succeeded" in its intent to release
    // a registration it no longer holds.
    //
    // Returns ErrNotFound only if the name never existed. Mismatched
    // owner is a silent no-op, NOT an error.
    Unregister(ctx context.Context, h Handle) error

    // Lookup returns the PID for a name, or ErrNotFound. Always a
    // local read from the raftkv FSM snapshot.
    Lookup(scope, name string) (pid.PID, error)

    // Scan enumerates all names in a scope.
    Scan(scope string, fn func(name string, p pid.PID) bool) error
}
```

**Lua API correspondence:**

```lua
local names = require("names")
local app = names.open("names:myapp")

-- Register returns a handle userdata; store it for later unregister
local handle, err = app:register("worker", process.pid())
if not handle then
    -- err.code is "name_taken", "no_quorum", "leader_unreachable", etc.
    error(err.message)
end

-- Handle can be passed to unregister; it's an opaque userdata
-- (encodes scope, name, pid, epoch internally)
app:unregister(handle)

-- Typical pattern: register in the process's init path, unregister
-- happens automatically on process death via the topology monitor
-- installed at Register time (§9.5). Explicit unregister is only
-- needed for graceful handoff scenarios.
```

**Typical usage (automatic cleanup):** the recommended pattern is to rely on topology-based auto-cleanup (§9.5) and never call `Unregister` directly. When the registered process dies, the topology exit handler (installed during `Register`) issues the CAS unregister with the correct Handle internally. User code only calls `Unregister` when it wants to release a registration WHILE the process is still alive — e.g., to hand off ownership without exiting.

### 9.4 Implementation

**Data model.** Each name lives at `/names/{scope}/{name}` and stores a msgpack `nameEntry{ PID, OwnerEpoch, RegisteredAt }`. `OwnerEpoch` is the fencing epoch (§15) the FSM assigned at registration — it is the identity stamp that makes delete-if-owner CAS correct.

**Register.** Build a `nameEntry` with `PID` + `RegisteredAt`, call `ApplyStrict(opSetIfAbsent, key, value)`. The FSM populates `OwnerEpoch` from `f.epoch` at apply time (deterministic across replicas) and returns the stamped epoch in `applyResult.OwnerEpoch`. The registry returns a `names.Handle{Scope, Name, PID, Epoch}` built from that result — callers MUST keep it, since Unregister rejects anything without a matching (PID, Epoch) pair. On success, the registry installs a topology monitor that issues `unregisterHandle(h)` when the process exits. If the monitor install fails (process already dead), the registry immediately runs the same CAS-unregister.

**Lookup.** Local `Get(key)` against the raftkv FSM snapshot; unmarshal and return the stored PID.

**Unregister / unregisterHandle.** `ApplyStrict(opDeleteIfOwner, key, ExpectedOwnerPID, ExpectedOwnerEpoch)`. The FSM deletes **only** when the stored `(PID, OwnerEpoch)` matches. A mismatch is a silent no-op, not an error — per the §9.3 contract, a stale exit event observing a superseded registration has already been invalidated by whoever registered after it.

**FSM apply rules.**
- `opSetIfAbsent` on a name key: if absent, stamp `ne.OwnerEpoch = f.epoch`, store the entry, return `{OK: true, OwnerEpoch: f.epoch}`. If present, return `{OK: false, Version: existing.version}`.
- `opDeleteIfOwner`: fetch entry; if missing, `{OK: false, Err: ErrKeyNotFound}`. If stored `(PID, OwnerEpoch)` matches the expected pair, delete + detach from lease + emit the watch delete + return `{OK: true}`. Otherwise `{OK: false}`.

Determinism: `f.epoch` advances identically on every replica (including snapshot restore), so the stamped `OwnerEpoch` is part of Raft state — not wall-clock state — and stale handles can never masquerade as current owners.

### 9.5 Process Death Handling

When a process dies:

1. Topology fires exit event on all nodes watching the process.
2. The exit handler registered at `Register` time (see §9.4) invokes `unregisterOwner(scope, name, pid, epoch)` with the epoch recorded when the name was originally bound.
3. `unregisterOwner` submits an `opDeleteIfOwner` op via `ApplyStrict`.
4. The FSM checks: is the stored owner still `(pid, epoch)`? If yes, delete. If no (name has been re-registered by another process), the delete is a silent no-op.
5. Net effect: delayed exit events cannot clobber newer registrations. Each exit only affects the exact registration it corresponds to.

**Node crash cleanup:** when memberlist reports `NodeLeft(nodeID)`, the Raft leader scans the FSM for name entries where `PID.Node == nodeID` and issues `opDeleteIfOwner` for each. Each delete is owner-scoped, so concurrent re-registrations from a restarting node are safe — the old (pid, epoch) is different from any new registration.

Leader-initiated only: followers do not run the node-crash cleanup loop. If the leader changes during cleanup, the new leader picks up where the old one left off via a watermark stored in the FSM (`/cleanup/last_node: $nodeID`, updated atomically after each batch of deletes).

**Race analysis — the exact scenario this prevents:**

```
t=0:    Node A registers name "X" bound to PID_A1 with OwnerEpoch 100
t=1:    PID_A1 crashes. Topology fires exit event.
t=2:    Exit handler queues unregisterOwner("X", PID_A1, 100) — NOT yet applied
t=3:    Node B registers name "X" bound to PID_B1 with OwnerEpoch 150
        (opSetIfAbsent sees the key is absent because nobody has
         applied the unregister yet — but wait, this scenario requires
         the unregister to have already run.)
```

Actually, the scenario where the race matters is different — and more subtle:

```
t=0:    Node A registers "X" → PID_A1, OwnerEpoch 100
t=10:   PID_A1 exits. Exit handler queued on Node A's local scheduler.
t=11:   Exit handler submits opDeleteIfOwner("X", A1, 100) to Raft.
t=12:   FSM applies the delete (successful, owner matches). Name is gone.
t=13:   Node C registers "X" → PID_C1, OwnerEpoch 200. Succeeds.
t=20:   Due to a slow scheduler, Node B (which was also watching A1)
        finally processes A1's exit event and submits opDeleteIfOwner("X", A1, 100).
t=21:   FSM applies: stored owner is (PID_C1, 200), expected is (PID_A1, 100).
        MISMATCH. Silent no-op. PID_C1's registration is untouched.
```

Without CAS-on-owner, step 21 would unconditionally delete, destroying PID_C1's registration. The spec now prevents this.

### 9.6 Lua API: `runtime/lua/modules/names/`

Follows the §2.2 `*.open` pattern. Instance method surface:

```lua
local app = names.open("names:myapp")
local handle, err = app:register("worker-pool", process.pid())
local pid, err   = app:lookup("worker-pool")
app:unregister(handle)
```

`register` returns a typed **handle** userdata wrapping `(scope, name, pid, epoch)`. The handle is non-serializable and cannot be forged — the epoch is stamped by the FSM during Apply, so user code cannot predict it and cannot clobber a newer registration. Typical apps keep the handle for graceful handoffs but drop it on crash: topology-driven auto-cleanup runs with the original handle captured inside the registry. Dropping the handle never leaks a registration; losing it just forfeits the ability to explicitly unregister.

---

## 10. 2-Node Cluster Modes

### 10.1 Mode Selection

```yaml
cluster:
  nodes: 2
  strong_kv:
    mode: "strict"              # default; alternative: "eventual"
```

v1 ships Mode Strict and Mode Eventual. Primary/secondary and witness-backed quorum are deferred to post-v1 (see §18, Phase 6).

### 10.2 Mode: Strict (Default)

- `raft.Configuration` has 2 voters; Raft requires 2/2 acks (full quorum).
- If either node is down, strong KV writes fail with `ErrNoQuorum` until the peer returns. Reads from local FSM may be stale.
- Eventual KV and PG are unaffected (they do not use Raft).

Clear, simple, correct. Use for production 2-node deployments that can tolerate downtime when a node is gone; otherwise run three voters.

### 10.3 Mode: Eventual

Explicitly unsafe for exclusive-ownership use cases. Provided for dev laptops and deployments that need always-on availability and can accept duplicates. System names flow through eventual KV; conflict resolution rides the cancel protocol in §12. There is no fencing epoch, no hard drain deadline, no guarantee that a losing process stops writing to downstream systems. Operators MUST monitor for duplicate singletons.

Do NOT use for anything involving money, user data, external side effects, or correctness-critical state. For production HA with 2 real machines, run a 3-voter cluster (third voter can live on the smallest possible host).

---

## 11. Cluster Voter Topology

### 11.1 Voters vs Clients

Cluster nodes come in two roles, declared in config (no auto-election — explicit roles follow the Consul/Nomad/Vault pattern and avoid bootstrap races + voter flapping):

- **Voters** (3-5): participate in Raft consensus.
- **Clients** (0+): non-voters. No local Raft, no FSM replica — pure forwarding to the current leader over relay.

```yaml
# Voter
cluster:
  node_name: "node-1"
  role: "server"
  nodes: 3
  voters: ["node-1", "node-2", "node-3"]

# Client
cluster:
  node_name: "node-worker-47"
  role: "client"
```

### 11.2 Client Forwarding

- **Writes** always forward to the leader; cost is one network hop + normal Raft commit latency.
- **Reads** default to a small bounded-TTL cache (default 100ms, disabled for name lookups) backed by leader forwarding on miss. `WithFreshRead()` bypasses the cache and uses Raft ReadIndex for linearizability.
- Client local caches are invalidated on writes the client issued.

Why not eventual replicas on clients: a lossy broadcast (DDS is fire-and-forget) means a missed delta serves stale reads forever, breaking Super Strong. If clients ever need log-backed replicas, promote them to non-voting Raft followers instead.

### 11.3 Clients And Super Strong

Clients are NOT in the strict reachable view (§8.7). They hold no FSM, so "apply" is undefined for them — they simply forward, and forwarded reads always hit the post-apply leader state. `ApplyStrict` therefore scales independently of client count.

---

## 12. Healing (Eventual KV + PG)

### 12.1 Scope

Healing applies only to eventual tier primitives (eventual KV, PG). The strong KV (Raft) does not need a custom healing protocol — Raft log replay handles partitioned nodes automatically when they reconnect.

### 12.2 Protocols

| Primitive | Merge rule | Notes |
|---|---|---|
| Eventual KV | Last-write-wins by version; tombstones win ties | DDS discover/sync, watchers fire for changed keys |
| PG | Differential sync from PR 241 | Computes diff since partition; emits join/leave only for real changes; duplicate members on both sides stay (PG has no uniqueness) |
| Eventual-name conflict | Deterministic winner (higher version, tiebreak lower node ID); losers receive `name_conflict` exit event via topology | Only runs under `strong_kv.mode: eventual`; supervisor sees `name_conflict` and does NOT restart. See §13.4 for the cancel protocol. |

With the Raft-backed strong KV (§8) name conflicts cannot occur, so the third row never fires.

---

## 13. Cluster Singletons

### 13.1 Pattern

Cluster singletons are processes that must have exactly one instance across the entire cluster. Examples: scheduler, metrics aggregator, cleanup worker.

Implementation uses the existing `process.service` kind with an added `singleton` flag:

```yaml
- id: "singleton:scheduler"
  kind: "process.service"
  process: "scheduler.lua"
  host: "worker"
  singleton: true              # new flag
  singleton_key: "scheduler"   # key in /singletons/{key}
  lifecycle:
    auto_start: true
    restart: always
```

### 13.2 Start() Modification: `service/supervisor/service.go`

**Invariant:** the lease that holds the singleton claim is alive only while the child process is in `Running` state. Crash, graceful exit, and restart-policy exhaustion all revoke it. A crash-looping singleton cannot keep its claim indefinitely — otherwise failover would block while the service is effectively down.

**Claim flow (extending `Start()` for `Singleton: true`):**

1. `Get(/singletons/{key})` on the strong KV.
   - Key missing → proceed to fresh claim (step 2).
   - Key holds our own `localNodeID` → **restart-adopt path**. Look up the lease via `GetLease(entry.LeaseID)` and reuse it. If the lease has disappeared between the two reads, fall through to step 2. Adopting avoids an unnecessary failover when the supervisor restarts faster than the lease TTL.
   - Key holds a different node → start `watchAndRetry` and return `ErrSingletonHeldElsewhere`.
2. Fresh claim: `GrantLease(SingletonLeaseTTL)` (default 10s), then `SetIfAbsentWithLease(key, localNodeID, leaseID)`.
   - On race loss, re-read the key: if it now belongs to us, reuse the just-granted lease; otherwise revoke the lease, start `watchAndRetry`, return `ErrSingletonHeldElsewhere`.
3. Stash the lease on the service. **Do NOT start the keepalive loop yet** — lifecycle hooks own that.

**Lifecycle coupling.** Keepalive runs inside a `singletonLifecycle` that wraps the supervisor's existing lifecycle:

- `OnStart` — child just reached `Running`. Start `keepAliveLoop(ctx, lease)` under a cancel context stored on the service.
- `OnComplete` — child exited (crash, graceful, terminate). Cancel keepalive. Lease now coasts to its TTL unless the supervisor restarts the child (in which case `OnStart` fires again and restarts keepalive).
- `releaseSingletonClaim()` — invoked when the supervisor's `lifecycle.restart.max_attempts` is exhausted. Explicitly calls `lease.Revoke(ctx)` so the key is deleted immediately and peers take over without waiting for TTL.

**Restart path.** After a crash, OnComplete stops keepalive and the supervisor waits its restart backoff. On retry, `Start()` → `acquireSingletonClaim` runs the adoption sequence above: short backoff → lease still alive, adopt it; long backoff → lease expired, grab a new one; peer moved in during the gap → return `ErrSingletonHeldElsewhere`.

`GetLease(leaseID)` on the strong KV (see §3.1 `Leasable`) is the single API making the adopt path possible — without it, every restart would race lease expiry and force a failover.

### 13.3 Failover

**Normal failover (winning node dies cleanly):**
1. Memberlist reports NodeLeft on other nodes.
2. Winning node's Raft session ends; its leases are revoked via the node-crash cleanup loop (§9.5).
3. Key deleted from strong KV via Raft log entry.
4. Watchers on other nodes fire; they compete for the claim.
5. One wins, spawns the process locally via the full `Start()` path.
6. New winner's supervisor uses the lifecycle-coupled keepalive from §13.2.

**Failover time:** lease TTL (default 10s) + memberlist detection (~5s) + Raft commit for the new claim (~5ms). Total: ~15s worst case.

**Crash-loop failover:** the winning node is alive but the child process crash-loops. After `lifecycle.restart.max_attempts` exhaustion, the supervisor revokes the lease explicitly. Failover kicks in immediately (no need to wait for lease TTL). Total: ~Raft commit latency.

**Partition failover:** the winning node is partitioned from the Raft leader. Its local lease keepalive may still succeed against its own local state, but the lease will fail to renew through Raft (no quorum). The lease expires naturally. The majority partition's Raft leader handles the lease expiry cleanup.

### 13.4 Process-Level Cancel On Name Conflict

If a process is running locally and the strong KV reports that a different process now holds the singleton key (via Watch event), the supervisor:
1. Sends a `name_conflict` exit event to the local process via topology.
2. Waits for the process to drain (bounded drain window, default 5s, configurable via `singleton_drain_timeout`).
3. After the drain window, calls `process.Terminate(pid)` on the local host.
4. Does NOT restart (the name is held elsewhere).
5. Starts `watchAndRetry` in case the new holder eventually releases the claim.

The drain window is a best-effort accommodation, not a guarantee. Processes that need graceful cleanup should handle the `name_conflict` event promptly (close connections, flush state, exit). Processes that ignore the event are terminated after the window expires.

**Why termination is OK here but not in Mode C (§10.4):**

In this path, the singleton key is held by a different process via the strong KV — the Raft log has a committed entry saying "this is no longer ours." The fencing epoch has advanced. Any downstream write from the old process will be rejected by fencing checks (§15). Terminating the local process is just tidying up; correctness is already guaranteed by the fencing layer.

In Mode C (§10.4), the KV is eventually consistent and there is no fencing authority. Terminating a process does not prevent it from having already corrupted state during the partition. That's why Mode C has weaker guarantees and is explicitly marked unsafe for strong consistency needs.

---

## 14. Security Model

### 14.1 Resource-Based Access Control

Every named instance (kv.host, pg.host, names.host) is a resource. Acquiring it from Lua requires a security check:

```go
// In runtime/lua/modules/kv/module.go (new)
func open(l *lua.LState) int {
    id := l.CheckString(1)

    // Security check on the instance ID
    if !security.IsAllowed(l.Context(), "kv.open", id, secAttrs) {
        return pushError(l, lua.PermissionDenied,
            fmt.Sprintf("not allowed to access kv: %s", id))
    }

    // Acquire via resource registry
    reg := resource.GetRegistry(l.Context())
    resID := registry.ParseID(id)
    res, err := reg.Acquire(l.Context(), resID, resource.ModeNormal)
    // ... wrap in userdata
}
```

Security policies gate access at the instance level. A process with access to `kv:sessions` cannot read or write `kv:feature-flags` keys.

### 14.2 Method-Level Checks

Some operations require additional checks beyond instance acquisition. Name registration is sensitive enough to warrant a per-name check:

```go
// In runtime/lua/modules/names/module.go
func register(l *lua.LState) int {
    scope := ... // from userdata
    name := l.CheckString(1)

    if !security.IsAllowed(l.Context(), "names.register",
        scope+"::"+name, secAttrs) {
        return pushError(l, lua.PermissionDenied,
            fmt.Sprintf("not allowed to register name: %s::%s", scope, name))
    }

    // ... register ...
}
```

This matches the PR 241 PG pattern of checking individual group names on join/leave/broadcast.

### 14.3 Process Send Security (Existing)

The existing `process.send` security check in `runtime/lua/modules/process/module.go:155-183` (`resolvePID`) gates who can send to whom. This is orthogonal to the KV security model and stays as-is.

---

## 15. Fencing Epochs And Downstream Safety

### 15.1 Epoch Source

The strong KV FSM maintains a global `epoch` counter (§8.3). Every applied Raft log entry increments this counter. The epoch is attached to every operation result and every lease handle.

### 15.2 Downstream Usage

When a workflow or other durable component wants to write to Postgres based on a strong KV decision, it includes the epoch in the write. The fencing check MUST be executed in a single transaction with the rest of the application update, under an isolation level that prevents two concurrent writers from both observing "my epoch is newer" against the same row.

**Required protocol:**

```sql
BEGIN ISOLATION LEVEL REPEATABLE READ;

-- The fencing update is a compare-and-update. The WHERE clause
-- checks the current fencing_epoch AND the application-level
-- preconditions in the same statement. At REPEATABLE READ,
-- concurrent writers will serialize on the row lock and one
-- will see the other's updated fencing_epoch.
UPDATE workflows
SET state = 'running',
    owner = $1,
    fencing_epoch = $3    -- update the stored epoch to our own
WHERE workflow_id = $2
  AND fencing_epoch < $3  -- only if our epoch is newer than stored
  -- AND other application preconditions, e.g.:
  AND state = 'pending'
RETURNING state, fencing_epoch;

-- If RETURNING yields zero rows, our write was rejected by the
-- fencing check. The caller MUST NOT proceed with any side-effect
-- that assumes the workflow is now "running".

COMMIT;
```

**Why REPEATABLE READ (or SERIALIZABLE) is required:**

At READ COMMITTED (the Postgres default), the following race is possible:
1. Writer A begins a transaction, sends `UPDATE ... WHERE fencing_epoch < 100`.
2. Postgres acquires a row lock and evaluates the WHERE clause. Stored epoch is 50, 50 < 100 → match. Update committed at epoch 100.
3. Writer B (with stale epoch 99) begins a new transaction, sends `UPDATE ... WHERE fencing_epoch < 99`.
4. Postgres evaluates against the NEW committed row (stored epoch 100). 100 < 99 → false. Update rejected.

Under READ COMMITTED, this actually works correctly for the single UPDATE statement above because Postgres re-reads the latest committed version after acquiring the row lock. **So READ COMMITTED is sufficient for a single `UPDATE ... WHERE fencing_epoch < $N` statement.**

However, if the caller combines the fencing check with additional SELECTs or side effects in the same transaction, READ COMMITTED exposes the caller to anomalies where a different writer commits between the SELECT and the UPDATE. For those cases, REPEATABLE READ (to get snapshot isolation) or SERIALIZABLE is required.

**Rules of thumb:**

- **Single UPDATE statement with fencing check in WHERE:** READ COMMITTED is sufficient. Keep the statement atomic.
- **SELECT-then-UPDATE patterns:** use REPEATABLE READ or SERIALIZABLE.
- **Multi-row updates gated by a single fencing decision:** wrap in a single transaction at REPEATABLE READ.
- **Application logic that depends on post-fencing state:** do NOT read the row back with a separate SELECT under READ COMMITTED — instead, use RETURNING to get the post-update state in the same statement.

**Invariants the caller MUST preserve:**

1. The fencing_epoch column is a monotonically increasing bigint, updated by every writer that passes the fencing check.
2. The WHERE clause MUST include `fencing_epoch < $N` (strictly less than, not equal). Equal epochs would allow the same epoch to write twice — a bug if the caller assumes uniqueness.
3. The RETURNING clause is used to confirm the write applied. Zero rows returned = fencing rejected = caller MUST NOT proceed.
4. The application must never bypass the fencing check. Any direct UPDATE without the WHERE guard is a correctness bug.

If a partitioned node with a stale epoch tries to write, the Postgres write fails. This prevents zombie nodes from corrupting state after they've been replaced. The key assumption is that the fencing_epoch column is present on every durable row that can be gated by a strong KV decision — this must be enforced by schema review.

### 15.3 Epoch In Leases

Each `GrantLease` returns a lease with the current epoch. Lease-bound operations include the epoch. When a lease is revoked (explicit or expiry), the epoch advances, invalidating any pending operations tagged with the old epoch.

### 15.4 Why Not Per-Key Epochs

Per-key epochs would be more granular but require per-key consensus history. Using a global monotonic epoch is simpler and sufficient — the only downside is that unrelated operations on different keys serialize on the same epoch sequence, which is exactly what Raft provides anyway.

---

## 16. Operational Concerns

### 16.1 Bootstrapping

Fresh clusters must prevent split-brain from `bootstrap: true` misconfiguration. Two guardrails, both required in production:

1. **Pre-bootstrap peer contact** (`PreBootstrapContactWindow`, default 30s). A node with `bootstrap: true` contacts every configured voter via a `raftkv.PreBootstrap` RPC (registered on the host before Raft starts) and classifies each response:
   - `no cluster formed` → ok.
   - `voter in cluster X` → abandon bootstrap, switch to join-existing mode (`AddVoter` to the existing leader).
   - no response inside the window → abort with `ErrPreBootstrapPeerTimeout`.

2. **Shared bootstrap lock** — only one node may hold it. Backends: Postgres advisory lock (default when Postgres is configured), shared file CAS (fallback), or operator-run `wippy cluster bootstrap-confirm` token (isolated environments).

**Sequence:** load persistent Raft state; if non-empty, resume as normal voter (bootstrap flag ignored). Otherwise run guardrail 1, acquire guardrail 2, call `BootstrapCluster`, wait for leadership, write success marker, release lock. Other voters then join via `AddVoter`.

Escape hatches (`bootstrap_skip_peer_check`, `bootstrap_lock: "none"`) exist for single-node test use and are documented as UNSAFE. Subsequent restarts need no bootstrap flag — persistent state alone drives recovery.

### 16.2 Adding A Node

1. Operator deploys the new node with `cluster.role: "client"` initially.
2. New node joins via memberlist, becomes part of the relay mesh.
3. New node starts its `raftkv.Client` in forwarding-only mode. It does NOT sync an FSM replica — it forwards all strong KV operations to the leader via relay (§11.3). The optional bounded-TTL cache is empty at startup and fills lazily on first access.
4. To promote to voter: operator calls a runtime API `cluster.AddVoter(nodeID)`. This promotion converts the node from a `raftkv.Client` (forwarding) to a `raft.Server` (full Raft participant).
5. API sends `AddVoter` to the Raft leader.
6. Raft joint consensus transitions through two configurations safely.
7. The new voter catches up to the leader via standard Raft log replication (possibly including a snapshot transfer, §8.4). This is the FIRST time the node holds FSM state — as a client, it held nothing.
8. After catch-up, the node is a full voter and may be counted in the strict reachable view (§8.7).

### 16.3 Removing A Node

1. Graceful: operator calls `cluster.RemoveNode(nodeID)`. Sends `RemoveServer` to Raft leader. Joint consensus transitions.
2. Hard: node is dead and won't come back. Operator calls `cluster.RemoveNode(nodeID, force: true)`. Same Raft operation but memberlist is also notified to stop tracking.

### 16.4 Metrics

Every service exposes metrics via the existing Wippy metrics system. Key metrics:

- `raftkv.writes_total` (counter): total Raft log appends
- `raftkv.writes_failed_total` (counter, labeled by reason)
- `raftkv.apply_latency_seconds` (histogram): time from Apply() call to FSM execution
- `raftkv.follower_lag_seconds` (gauge, per node): time between leader commit and follower apply
- `raftkv.leadership_changes_total` (counter)
- `raftkv.current_leader` (gauge, labeled by node_id)
- `raftkv.log_size_bytes` (gauge)
- `raftkv.snapshot_duration_seconds` (histogram)
- `kv.operations_total` (counter, labeled by scope, op_type)
- `kv.watchers_active` (gauge, per scope)
- `kv.leases_active` (gauge, per scope)
- `pg.members_total` (gauge, per scope)
- `pg.broadcasts_total` (counter)
- `names.registered_total` (gauge)
- `names.register_latency_seconds` (histogram)
- `names.conflicts_total` (counter)

### 16.5 Debugging Endpoints

Admin API exposes:
- `/cluster/status` — membership, Raft state, leader, voter list
- `/cluster/raft/stats` — hashicorp/raft internal stats (last log, applied, commit index, etc.)
- `/cluster/config` — current voter configuration
- `/kv/{scope}` — dump scope contents (read-only, rate-limited)
- `/pg/{scope}` — dump group memberships
- `/names/{scope}` — dump name registrations

### 16.6 Backup And Restore

- Raft log: backed up via bbolt file copy (while the node is stopped or via bbolt's snapshot).
- FSM state: included in Raft snapshots via `FileSnapshotStore`.
- Eventual KV: not backed up. Ephemeral state.
- PG: not backed up. Ephemeral state.

Disaster recovery: restore Raft state from backup on a fresh node, bootstrap as a new cluster, re-add other nodes.

---

## 17. Known Risks And Open Questions

### 17.1 Open: Bootstrapping A Cluster Across DCs

The design targets single-region clusters. For multi-DC deployments, operators would run separate clusters with cross-DC eventual KV replication. This is out of scope for v1 but the eventual KV design already supports multiple independent scopes that could be glued together with cross-cluster DDS.

### 17.2 Risk: Raft Transport Over Mesh

Running Raft over a custom transport has documented gotchas (§8.4). Mitigation: extensive chaos testing with real memberlist partitions during Raft operations. Validate:
- Leader election during transport congestion
- AppendEntries under packet loss
- Snapshot transfer over the mesh
- TLS renegotiation timing

### 17.3 Risk: BoltDB On Slow Disks

BoltDB's fsync behavior can stall Raft commits on slow disks (spinning rust, shared NFS). Mitigation: require SSD-backed storage for Raft data directory. Add operational check that warns if the bbolt write latency exceeds a threshold.

### 17.4 Risk: Snapshot Size

If the FSM state grows large (e.g., 100k+ names with large values), snapshots take time to create and transfer. Mitigation: monitor `raftkv.log_size_bytes` and tune `SnapshotThreshold`. Use streaming snapshot encoding instead of in-memory.

### 17.5 Risk: Leader Flapping Under Load

If the Raft heartbeat timeout is too close to the relay mesh's retry delay, spurious leader elections can happen under load. Mitigation: set Raft heartbeat timeout to 3x the mesh's P99 latency. Test under load before shipping.

---

## 18. Implementation Plan

### Phase 1: Foundation (2 weeks)

1. Extract DDS transport from PG into `system/dds/` — migrate `system/pg/protocol.go` logic. Update PG to use DDS via callbacks.
2. Add `relay.NewServicePackage` constructor to `api/relay/relay.go`. Migrate PG to use service packages instead of synthetic PIDs.
3. Add `kv.host` Manager (`service/kv/manager.go`), config (`api/service/kv/config.go`), boot wiring (`boot/components/system/kv.go`).
4. Wire existing `system/kv/` as the backend for user-declared eventual KV instances.
5. Lua `kv` module (`runtime/lua/modules/kv/`) with `kv.open(id)` pattern.

### Phase 2: PG Multi-Instance (1 week)

1. Parameterize `system/pg/service.go` to accept `HostID` and scope name as constructor arguments (remove hardcoded `"pg"`).
2. Add `pg.host` Manager (`service/pg/manager.go`).
3. Update Lua PG module to use `pg.open(id)` pattern.
4. Migrate existing `pg.scope()` prefix API to work as a thin wrapper over instance.prefix (backwards compatibility).
5. Update playground tests to use the new API.

### Phase 3: Raft KV Core (3 weeks)

1. `system/raftkv/fsm.go` — KV state machine with deterministic Apply.
2. `system/raftkv/storage.go` — bbolt-backed log/stable store, file snapshots.
3. `system/raftkv/transport.go` — raft.Transport over internode mesh.
4. `system/raftkv/host.go` — relay.Receiver for incoming Raft RPCs.
5. `system/raftkv/bootstrap.go` — bootstrap logic.
6. `system/raftkv/leader.go` — leader tracking, forwarding for non-leaders.
7. `system/raftkv/service.go` — top-level wiring.
8. Unit tests + multi-node integration tests.

### Phase 4: Super Strong + Name Registry (2 weeks)

1. `system/raftkv/superstrong.go` — ApplyStrict with wait-for-all-reachable.
2. `system/names/` — name registry service on top of raftkv.
3. `api/names/` — interfaces.
4. `service/names/manager.go` — names.host manager.
5. Lua `names` module.
6. Topology integration for process death cleanup.

### Phase 5: Cluster Singletons (1 week)

1. Extend `service/supervisor/service.go` Start() to check singleton claim before spawning.
2. Add singleton fields to `api/service/supervisor/config.go`.
3. Watch-and-retry for losing nodes.
4. Integration tests with node failure injection.

### Phase 6: 2-Node Modes (1 week)

1. Implement Mode A (strict, default).
2. Implement Mode C (eventual) as fallback.
3. Document mode selection in operator docs.
4. Mode B (primary) and Mode D (witness) deferred to post-v1.

### Phase 7: Hardening (2 weeks)

1. Chaos testing with partitions, node failures, leader failovers.
2. Jepsen-style property tests for linearizability.
3. Load testing for throughput targets.
4. Metrics and observability endpoints.
5. Documentation and examples.

**Total estimated effort:** 12 weeks for one engineer, parallelizable to ~6 weeks with two engineers across Raft and non-Raft workstreams.

---

## 19. References

1. **hashicorp/raft:** https://pkg.go.dev/github.com/hashicorp/raft
2. **Raft paper:** Ongaro & Ousterhout, "In Search of an Understandable Consensus Algorithm" (2014)
3. **Derecho:** Birman et al., "Derecho: Fast State Machine Replication for Cloud Services" (2019) — inspiration for the all-reachable-apply semantics
4. **PG (PR 241):** https://github.com/wippyai/runtime/pull/241
5. **Erlang pg module:** https://www.erlang.org/doc/apps/kernel/pg.html — conceptual basis for multi-instance process groups
6. **Consul:** voter/client topology pattern
7. **Temporal:** RangeID fencing pattern, shard ownership via database CAS
8. **Chandra & Toueg:** "Unreliable Failure Detectors for Reliable Distributed Systems" (1996)
9. **FLP:** Fischer, Lynch, Paterson, "Impossibility of Distributed Consensus with One Faulty Process" (1985)
10. **CAP theorem:** Gilbert & Lynch, "Brewer's Conjecture and the Feasibility of Consistent, Available, Partition-Tolerant Web Services" (2002)

---

## Appendix A: Existing Code Reference

Files that already exist and are referenced in this spec:

| File                                             | Status    | Role                                          |
|--------------------------------------------------|-----------|-----------------------------------------------|
| `api/cluster/cluster.go`                         | exists    | Membership, NodeInfo, MessageCodec            |
| `api/cluster/events.go`                          | exists    | Cluster events (NodeJoined, etc.)             |
| `api/cluster/kv.go`                              | added     | Low-level KV interface                        |
| `api/cluster/kv_errors.go`                       | added     | KV sentinel errors                            |
| `api/store/store.go`                             | exists    | User-level store interface                    |
| `api/pg/pg.go`                                   | PR 241    | Process groups interface                      |
| `api/relay/relay.go`                             | exists    | Relay package, routing                        |
| `api/process/process.go`                         | exists    | Process, Host, Lifecycle interfaces           |
| `api/topology/topology.go`                       | exists    | Monitor, Link, exit events                    |
| `api/service/host/config.go`                     | exists    | process.host registry entry                   |
| `api/service/supervisor/config.go`               | exists    | process.service registry entry                |
| `system/kv/` (all files)                         | added     | In-memory KV backend                          |
| `system/pg/` (all files)                         | PR 241    | Process groups service (singleton)            |
| `system/relay/node.go`                           | exists    | Node routing by Host field                    |
| `system/topology/topology.go`                    | exists    | Sharded process registry                      |
| `system/eventbus/bus.go`                         | exists    | Single-goroutine dispatcher                   |
| `service/host/manager.go`                        | exists    | process.host Manager (pattern reference)      |
| `service/supervisor/service.go`                  | exists    | process.service lifecycle                     |
| `service/store/memory/manager.go`                | exists    | store.memory Manager (pattern reference)      |
| `cluster/membership/membership.go`               | exists    | memberlist integration                        |
| `cluster/internode/manager.go`                   | exists    | TCP mesh connection manager                   |
| `cluster/internode/codec.go`                     | exists    | Msgpack package codec                         |
| `runtime/lua/modules/store/store.go`             | exists    | Lua store module (pattern reference for open) |
| `runtime/lua/modules/pg/`                        | PR 241    | Lua PG module                                 |
| `runtime/lua/modules/process/module.go`          | exists    | Lua process module                            |

## Appendix B: New Code To Be Added

| File                                              | Purpose                                          |
|---------------------------------------------------|--------------------------------------------------|
| `api/cluster/replication.go`                      | `ReplicationConfig`, `ReplicationTransport` iface |
| `api/cluster/strong_errors.go`                    | Strong KV sentinel errors                        |
| `api/cluster/dds.go`                              | DDS transport + callbacks interface              |
| `api/names/names.go`                              | Name registry interface                          |
| `api/service/kv/config.go`                        | kv.host registry entry                           |
| `api/service/pg/config.go`                        | pg.host registry entry                           |
| `api/service/names/config.go`                     | names.host registry entry                        |
| `api/service/raftkv/config.go`                    | Raft KV config                                   |
| `system/dds/transport.go`                         | Shared replication transport                     |
| `system/dds/host.go`                              | relay.Receiver for DDS messages                  |
| `system/kv/replication.go`                        | Eventual KV replication via DDS                  |
| `system/kv/host.go`                               | relay.Receiver for KV replication                |
| `system/raftkv/service.go`                        | Top-level Raft KV service                        |
| `system/raftkv/fsm.go`                            | Raft FSM                                         |
| `system/raftkv/transport.go`                      | raft.Transport over internode                    |
| `system/raftkv/host.go`                           | relay.Receiver for Raft RPCs                     |
| `system/raftkv/storage.go`                        | BoltDB storage                                   |
| `system/raftkv/snapshot.go`                       | Snapshot store                                   |
| `system/raftkv/bootstrap.go`                      | Bootstrap logic                                  |
| `system/raftkv/leader.go`                         | Leader tracking and forwarding                   |
| `system/raftkv/autopilot.go`                      | Voter health management                          |
| `system/raftkv/kv.go`                             | cluster.KV wrapper                               |
| `system/raftkv/superstrong.go`                    | ApplyStrict (wait-for-all-reachable)             |
| `system/raftkv/client.go`                         | Client node forwarding                           |
| `system/names/service.go`                         | Name registry service                            |
| `system/names/registry.go`                        | Register/Lookup/Unregister                       |
| `system/names/monitor.go`                         | Topology integration for cleanup                 |
| `service/kv/manager.go`                           | kv.host Manager                                  |
| `service/pg/manager.go`                           | pg.host Manager (replaces PR 241 singleton)      |
| `service/names/manager.go`                        | names.host Manager                               |
| `service/raftkv/manager.go`                       | raftkv bootstrap + lifecycle                     |
| `runtime/lua/modules/kv/module.go`                | Lua `kv` module                                  |
| `runtime/lua/modules/names/module.go`             | Lua `names` module                               |
| `boot/components/system/kv.go`                    | Boot wiring for kv                               |
| `boot/components/system/pg.go` (update)           | Boot wiring for pg.host                          |
| `boot/components/system/names.go`                 | Boot wiring for names                            |
| `boot/components/system/raftkv.go`                | Boot wiring for raftkv                           |

---

End of spec. Next step: run adversarial codex audit rounds (§audit protocol, see other sessions for the pattern) to harden each section before implementation begins.
