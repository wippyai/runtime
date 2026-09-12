# Terminal monitor delivery: current evidence and implementation boundary

This is a design constraint record, not a claim that reliable delivery exists.

## Reproduced failure

`monitor_terminal_recovery_test.go.txt` exercises production Topology.Complete,
monitor admission, message codec, receiver endpoint and refresh. Run with the
`/tmp/monitor-terminal-recovery-overlay.json` Go overlay. A successfully admitted
observer loses the confirmed terminal result for all of:

- local consumer rejects admission;
- source transport refuses the send;
- source transport takes ownership, but delivery is lost before receipt.

After a simulated disconnect and exact-reference refresh, the source returns
`ErrPIDNotRegistered`. Complete removed the target state and its observer set
before sending, and retains no terminal delivery obligation. The existing
receiver retry test only proves that a manually retained package can be sent
again; it does not cover this lifecycle sender.

## Required ownership boundaries

1. Reserve bounded delivery capacity when admitting a monitor, before returning
   installation success. Complete has no error return and must not discover
   exhausted record capacity after the process has exited.
2. Retain source-confirmed terminal state separately from recycled process state.
   Key it by exact target lifetime, caller PID and reference. A stale ACK/release
   must not affect a replacement relationship. New observers cannot acquire an
   already completed target merely by guessing its PID.
3. Receiver acceptance ACK is a transfer of delivery responsibility, not a durable
   application receipt. A naming cleanup queue then owns its admitted retry work.
   Crash recovery needs a separate persistence/fencing contract; RAM on either
   side does not establish durability.
4. Local relay Send success is insufficient. ACK must come from the authenticated
   recipient node, bind all relevant identities and the exact reference, and be
   emitted only after exact bound destination acceptance or a defined terminal
   rejection of the obsolete relationship. Full/refused delivery leaves source
   responsibility intact. ACK loss must lead to safe duplicate delivery/ACK.
5. Use existing relay and topology handler. service/host/host.go intercepts
   monitor control before scheduler lookup: while the host lives, a completed
   actor does not prevent a retry reaching topology's retained record. Host
   teardown/replacement still needs a separately owned recovery path.
6. Retry work must be bounded, cancellable, joined and fair across peers. Timers
   schedule retry only; elapsed time cannot erase obligations or prove death.
   Reconnect refresh must consult retained state, not synthesize an exit from
   missing registration.

## Result bytes are an unresolved implementation constraint

api/runtime/task.go Result holds arbitrary payload.Payload plus error. Retaining
its pointer is neither a stable immutable snapshot nor a byte bound.
api/payload.SnapshotData does not deep-copy arbitrary pointer-bearing structs.
MessageCodec.Encode currently normalizes/encodes the full package into a buffer;
checking encoded length afterwards does not bound peak allocation.

Do not implement an unbounded result map, reserve only record counts, silently
truncate results, or convert a delivery/serialization error into task failure.
The implementation needs a bounded immutable encoding path and an explicit
policy for results that cannot be represented within the admitted limit. Large
results and their ownership must be reconciled with the canonical Stream work;
that does not justify adding a new high-level application API by accident.

## Gates for the actual implementation

The current failed regression must become a supported recovery proof. Add ACK
loss/duplication, wrong-peer/stale-reference ACK, saturation at admission,
release-vs-completion races, target/caller replacement, immutable result capture,
byte-limit behavior, stopped peer fairness, joined shutdown, and real native
process disconnect/reconnect. Count/byte bounds must include retained terminal
state and history, not only active monitors. Keep transient disconnect distinct
from confirmed process death. Whole-process crash remains a distinct gate.

## Implementation in progress

Private source files `system/topology/monitor_outbox.go` and
`monitor_completion_ack.go` now cover retained bytes and ACK identity. They are
not yet connected to monitor admission, Complete, the exchange dispatch path or
a retry worker. They therefore do not fix the reproduced delivery failure yet.

Outbox capacity includes identity string bytes and reserved notice bytes;
count capacity bounds record overhead. Key strings must be cloned only after
admission to avoid retaining oversized backing strings or allocating on ACK
lookup. Filled bytes are immutable; outgoing copies still need a separate
bounded worker/in-flight budget. A premature ACK cannot settle an unfilled
reservation. Explicit release is a separate state transition.

ACKs use the existing versioned topology control envelope and authenticated
physical ingress. They bind the source caller, destination target and exact
reference. The decoder alone does not authorize deletion: the retained outbox
must match the decoded key and have a completed notice.

The policy question for a result exceeding its admitted delivery limit was sent
to the user: explicit result-unavailable detail while preserving the actual task
outcome, or full-value retention via durable storage/retrieval. No answer has
been received and no dependent result-loss behavior has been implemented.
