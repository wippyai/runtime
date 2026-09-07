# V1 review: Lua surface SDK and mesh

The surface feature is suitable for a controlled V1 agent/debugging workflow.
The core API covers producing, delegating, attaching, observing, input, resize,
revocation, and composition without exposing transport internals to producers.
Use [the Lua guide](LUA_GUIDE.md) for the cross-node workflow and its lifecycle.
The whole mesh should not yet be described as memory-bounded under arbitrary
outage and sustained overload.

## Evidence

- Eight independent runtimes on two Linux hosts, each connected to seven peers,
  passed 1,600 concurrent Lua/Bash command-to-screen checks under the Go race
  detector. Repeated runs showed per-node p95 varying roughly 13–34 ms. This is
  a functional load test, not an eight-host capacity result or latency guarantee.
- Populated 120×40-frame benchmarks cover 1, 8, and 32 observers. They verify
  eventual latest-revision convergence and report wire frames per presentation.
  The Go cached-snapshot read allocates zero bytes. These use an in-memory
  transport; Lua table materialization is measured separately below.
- The broader `go test -race ./cluster/...` suite passed, including membership,
  Raft, multiplexing, and cluster/test-network packages.
- Real-socket three-node, large-message, bidirectional, short-disruption,
  repeated-reconnection, and failed-write requeue tests passed three race runs.
  The existing test called HighThroughput sends only 100 paced messages; it is
  not a sustained throughput benchmark.
- Supervisor-controller tests exercise automatic owner/consumer restart through
  the process lifecycle barrier, old-reference rejection, and complete mount
  cleanup. They do not boot an entire registry-driven supervised application.
- Surface regressions cover peer/PID checks, separate rights, replay suppression,
  leases, queued cancellation, input-only revocation, stalled-peer isolation,
  and retry reserve capacity. New SDK tests check valid and invalid simulated
  events and selectable event channels.

## Lua observation cost

A local non-race benchmark on an AMD Ryzen 9 7950X3D measured the shared Lua
snapshot materialization path, with populated 120×40 rows:

| Operation | Approximate time/call | Allocation/call |
|---|---:|---:|
| Full `snapshot()`, unchanged row content, warmed cache | 0.79 µs | 1,239 bytes, 5 allocations |
| `snapshot(last_revision)` when unchanged | 149 ns | 7 bytes amortized; allocations/op rounds to zero |

The viewport reuses boxed Lua strings for unchanged rows. This reduced repeated
full reads from 1,879 bytes / 45 allocations and about 1.03 µs. Each changed row
still needs a new string wrapper; the Go string conversion shares its backing
bytes. Each call returns independent mutable snapshot and row tables, so caller
edits cannot corrupt later reads or retained snapshots. The cache holds only one
screen of row values, replaces its storage on row-count changes, and clears on
close. It does not pool returned tables or change the Lua API.

The unchanged check avoids building a screen table. Prefer update-driven reads
and keep the last revision; do not poll full snapshots in a tight loop. These
numbers do not include rendering, network RTT, or application command execution.

```bash
GOWORK=off go test ./runtime/lua/modules/tty -run '^$' \
  -bench BenchmarkLuaViewportSnapshot -benchmem
```

## Remaining limits in the shared mesh

1. **Reliable queues can grow during a long outage.** In
   `cluster/internode/state_manager.go`, zero-capacity queues for process
   broadcast and Raft traffic are deliberately unbounded while the peer remains
   managed. This preserves accepted messages but does not bound memory if
   producers keep sending to an unreachable peer. Surface admission is bounded;
   that bound does not cover the other classes. A separate shared-mesh change
   needs byte accounting and backpressure coordinated with those producers.
2. **Control traffic has strict priority.** Application classes alternate fairly
   with each other, after control/Raft/gossip traffic. Sustained control-plane
   saturation can delay application traffic. The tests do not establish an
   interactive latency bound during a Raft snapshot or membership storm.
3. **Input acknowledgement stops at the broker.** End-to-end application
   completion needs an observed marker or application acknowledgement. General
   process messaging is not made exactly-once by surface RPC deduplication.
4. **Overload/disconnection may end an attachment.** Bounded admission and RPC
   timeouts intentionally fail rather than accumulate indefinitely. Reattachment
   needs a fresh reference; there is no durable input log or automatic command
   replay across process/node restarts.

These shared-mesh policies predate the surface feature. Changing their delivery
or priority semantics should remain separate from the surface PR. Before claiming
larger production-scale robustness, add long-outage memory measurements,
mixed Raft/process/surface saturation tests, and tests on more physical hosts.
No new shared-mesh delivery failure was observed in the checks above.
