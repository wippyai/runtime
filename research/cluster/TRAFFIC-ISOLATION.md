# Traffic isolation: admission and wire scheduling

Current candidate: fix/cluster-naming-recovery-20260911. This is an implementation decision record, not a claim of completed isolation.

## Verified boundaries

- `cluster/internode/class.go`: ClassRaftControl carries PG join/leave/discovery/sync. ClassRaftRPC contains votes, AppendEntries, replies, and snapshot chunks. Other relay packages use ClassPGBroadcast. Class names do not cleanly separate latency-sensitive traffic from bulk.
- `state_manager.go`: reliable admission shares aggregate and per-peer budgets; queued and in-flight frames consume the same reservation. Control can currently be refused behind application backlog.
- `state_manager.go:drainMessagesForGeneration`: fixed class preference selects a batch before writing. Selection cannot preempt a frame already owned by the writer.
- `connection.go:flushBatch`: complete frames serialize onto one TLS connection; a queued heartbeat cannot overtake an in-progress large frame. TCP packet scheduling cannot remedy application framing head-of-line blocking.
- `cluster/raft/internode_transport.go:handleSnapshotChunk`: synchronous io.Pipe.Write can also stall the shared receive loop. Sender prioritization alone cannot remedy that receiver stall.
- Successful local flush releases memory; it is not remote admission, delivery acknowledgement, or durable storage receipt. Failed batches are currently requeued whole, with the pre-existing possibility of duplicate remote delivery.

## Constraints for implementation

1. Protect a bounded amount of admission capacity for coordination at BOTH node and peer scope. Application allocation must not hold aggregate credits while waiting on a narrower limit. Limits must remain configurable and validated, including tiny-budget configurations. Protected capacity does not grant unlimited control traffic.
2. Identify scheduling flows explicitly at the native sender. Do not infer ordering from Lua payload inspection or assume one flow per node. Actor messages from a single source to a destination and causally related lifecycle signals must retain their required ordering. A higher-priority exit must not bypass data whose processing it terminates.
3. Separate bulk transfer from latency-sensitive RPC inside Raft: snapshot header/chunks/EOF belong to one ordered transfer. Votes, heartbeats and independent RPC replies should not wait on its consumer. Merely granting all ClassRaftRPC higher priority fails this requirement.
4. Bound frame scheduling granularity. Either use bounded multiplexed chunks on existing authenticated framing or independent authenticated lanes with flow affinity. Multiple lanes need an explicit common peer/incarnation and shutdown lifecycle; no per-transfer TLS sidecars. One source's ordered flow cannot opportunistically hop lanes without an ordering barrier.
5. Use consumption credit for bulk receives. Per-transfer workers, buffers and outstanding credit must have aggregate limits; cancellation/disconnect must unblock both credit waits and consumers and join workers. Do not solve the synchronous receive stall by spawning an unbounded goroutine per chunk.
6. Preserve uncertainty. A socket write or queue admission cannot become a durable receipt. Storage commits and returns its own receipt. Never transparently replay an ambiguous mutation as part of scheduling recovery.

## Proof required before selecting defaults

- At configured application saturation, small coordination traffic is still admitted within its protected budget; exhaustion of that budget refuses safely.
- Native TLS: a held snapshot/file consumer does not delay an independent heartbeat beyond the stated bound; establish a baseline and measure p50/p95/p99, not only aggregate throughput.
- Per-flow numbered payloads plus lifecycle events retain order under mixed lanes, reset, reconnect and cancellation. Cross-flow scheduling freedom must not weaken these assertions.
- 100 actual nodes: bounded idle activity, mixed transfer/coordination, asymmetric slow peers, loss/disconnect/partition and recovery; capture total RSS, goroutines, retained credits and tail latency.
- Accounting-only benchmarks quantify shared-lock and allocation cost separately. They cannot support a network throughput claim. Keep atomic aggregate bounds even if sharding or wake routing is needed to reduce contention.

No wire/lane change has been made by this record. W2 remains untouched.

## Confirmed admission starvation (2026-09-11)

`research/cluster/control_capacity_test.go.txt` is an intentionally failing desired-contract regression, run with a Go overlay (not silently added as a passing CI test). Eight cases cover peer versus aggregate exhaustion, entries versus retained payload bytes, and queued versus writer-owned frames. All eight refuse a one-byte RaftRPC frame with ErrQueueFull. Aggregate cases target a separate, empty peer: pressure from other peers is sufficient to block control admission. Log: `/tmp/control-capacity-regression.log`.

This is not an accounting leak: drained frames correctly retain their charge. Releasing their credit early would violate the memory bound and is not a fix. Scheduler priority also cannot help frames that cannot be admitted.

Caller audit: ordinary Raft RPC returns send refusal; sendReply logs refusal and drops the unadmitted reply, leaving the requesting RPC to time out. Snapshot sends use context-aware waits, but consume the same class/budget. Therefore protecting ClassRaftRPC alone only isolates Raft from application backlog; a stalled snapshot could still exhaust that protection. Keep that limitation explicit in implementation and tests.

Admission policy should distinguish scheduling purpose from wire dispatch class: changing a snapshot to ClassPGBroadcast would send it to the wrong receiver. A future bulk scheduling category must retain its Raft receiver and header/chunk/EOF order. It must not promote actor exit/lifecycle messages ahead of the data they terminate. Application, coordination and snapshot capacity must all remain finite at peer and node scope; define whether protection is carved out of or additional to the configured aggregate before publishing config defaults.

## Control-admission phase implementation (2026-09-11)

The manager default now reserves16 entries/1MiB per peer and256 entries/16MiB per node for control. These are carved out of existing256-entry/512MiB peer and4096-entry/2GiB node totals, not additional allocations. Defaults therefore permit240 ordinary entries per peer and3840 per node. The largest ordinary retained frame is511MiB under the peer byte limit; wire MaxMessageSize remains512MiB. Operators reducing total budgets must also reduce their reserves; invalid configurations fail startup. Explicit zero disables the corresponding reserve. Configuration keys live under cluster.internode.outbound.control.

Both PG membership/discovery control and Raft RPC use protected admission. Control can borrow unused ordinary capacity, but all traffic respects the total. Ordinary capacity waiting does not hold aggregate credit, accepted frames remain charged through drain/retry, and cancellation before acceptance does not transfer ownership. No wire dispatch or per-class FIFO change in this phase.

Evidence: eight integration-level accounting cases cover peer/node, bytes/entries, queued/inflight; default-budget test saturates3840 ordinary entries across16 peers then fills the256 control entries and confirms hard total refusal. A real localhost TCP/TLS test with signed identity and cluster key stalls an application callback, observes blocked admission, cancels that wait, admits a control message, then resumes and verifies exact ordered application bytes, control delivery and zero retained credits. This explicitly does NOT assert that control can bypass the stopped shared receiver. The negative eligibility-disabled overlay fails all eight accounting cases.

Next phase remains required: separate Raft bulk from coordination, receive-side snapshot isolation, bounded scheduling granularity on the wire and real sustained control latency under mixed traffic. Current defaults are bounded initial capacity policy, not100-node service-load sizing or adaptive scheduling. No general mesh readiness/performance claim follows from this admission phase.
