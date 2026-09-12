# Control admission checkpoint

Status: locally implemented and tested; not committed, pushed, merged or production-approved. The broader cluster candidate remains incomplete. This checkpoint closes the current phase at the user's request; it does not certify the entire dirty worktree.

## Completed behavior

Reliable outbound admission protects coordination from ordinary application backlog at both peer and node scope. Defaults reserve16 entries/1MiB per peer and256 entries/16MiB per node, carved out of existing total budgets. Control may borrow free ordinary capacity but cannot exceed the hard total. Accepted payloads remain charged through queueing, writing and retry; cancellation before admission transfers no ownership. Configuration validates reserves before startup; explicit zero disables them.

The implementation is connected through boot configuration, manager defaults, peer/generation lifetime accounting and admission. It adds no transport, Lua API, timer or worker. Wire dispatch and per-class FIFO are unchanged.

Compatibility detail: the default largest ordinary retained frame is now511MiB, while the wire maximum remains512MiB. Ordinary entry capacity is240 per peer and3840 per node. Operators lowering total limits must adjust reserves too. These limits describe retained payload bytes and entries, not total process heap.

## Verified evidence

- Eight peer/node, entry/byte, queued/inflight protection cases pass.
- Default aggregate saturation admits3840 ordinary entries followed by256 control entries; further admission refuses at the total.
- Actual TCP/TLS with signed node identity and cluster key: a held application callback creates admission pressure; a canceled waiter takes no ownership; control admits; resumption delivers exact ordered application bytes and control, then releases all credits.
- Full internode race suite passes: `/tmp/control-phase-suite.log`.
- Focused configuration and TLS tests pass: `/tmp/control-phase-focus.log`.
- Selected Raft tests and the native multi-process boot/recovery harness pass: `/tmp/control-phase-native-raft.log`.
- Disabling protected eligibility via a research overlay makes all eight protection cases fail: `/tmp/control-phase-negative.log`.
- `git diff --check` passes.

## Limits and next phase

Raft snapshots still share control admission with ordinary Raft RPC. The shared receive callback can block on snapshot consumption, and a large frame cannot be preempted on the current connection. This phase protects admission against application backlog; it does not guarantee control latency under snapshot or wire saturation. Initial default capacities have not been sized against sustained100-node cluster-service load.

Resume with snapshot/coordination scheduling and receive isolation. Preserve the existing authenticated transport, receiver dispatch, per-flow ordering and joined lifecycle. Do not treat successful queue admission or local TLS flush as remote delivery or durable receipt.

Other original goal gates remain: canonical cross-node Streams; remaining naming, monitor-delivery and lifecycle work; sustained mixed/toxic-network validation; consumer documentation and Rodrigo review. See `RECOVERY-20260911.md` for the broader history and `TRAFFIC-ISOLATION.md` for transport constraints. Later journal entries supersede earlier intermediate statements about opt-in reserves or unwired primitives.
