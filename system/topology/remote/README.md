# Native remote monitoring

Work in progress. The grant authority and control codec are implemented; no boot component, remote
monitor receiver, Lua grant API or Bee client integration is enabled by it.
Transport authentication identifies an immediate node. The target owner must
also authorize the particular watcher and target actors.

## Boundary

The native receiver uses `node:topology`, separate from the existing
`node:control` supervisor mailbox. It uses owned relay registration so retiring
an old receiver cannot unregister a replacement. The receiver closes admission
before unregistering and cleans up its own relations; registration release alone
does not drain dispatched calls.

`control.go` carries version-1 monitor/release requests and correlated admission
results. A package contains exactly one JSON control message. Actor identities
are fully qualified and canonical. The decoder checks immediate-peer integrity,
authentication, live connection and matching envelope coordinates before exposing
a typed control. It does not authorize a grant or mutate topology. It borrows the
package; the receiving service owns release after successful relay admission.

Requests originate at the watcher actor's address and target the native topology
host on the target node. Replies originate at that native host and target the
watcher node's native topology host. Replies are useful only when an outstanding
request matches the grant, actor tuple and connection. A PID is an address, not
proof that the peer's actor may monitor the target.

## Implemented grant authority

`NewAuthority(localNode, capacity)` constructs a native host-owned grant table.
`Grant(GrantSpec)` mints a 256-bit opaque token for one remote watcher, one local
target and a fixed expiry. Grants have no wildcard scope. `Acquire` validates
the immediate peer and binds the grant to the first authorized connection;
reconnection needs a new grant. It returns a lease, not an installed monitor.

The receiver must perform its bounded topology mutation inside `Lease.Use`.
That gate checks expiry, revocation, authority closure and the actual connection
lifetime. A callback already admitted may finish; Revoke and every concurrent
Close caller wait for it. Callbacks must not perform I/O or reenter the authority.
A callback panic propagates while still releasing the gate. `Lease.Done` signals
retirement after the grant's capacity slot has been reclaimed. The owner must
retire its corresponding monitor state when Done closes.

Each grant owns one expiry timer and, after binding, at most one connection
watcher. Revocation, expiry, connection loss and authority closure reclaim grants.
The table capacity bounds these resources. Tokens are capabilities and must not
be placed in telemetry, process results or registry metadata.

## Receiver integration requirements

These are requirements for the remaining implementation, not current guarantees:

- Target owners issue bounded, expiring grants for exact watcher/target tuples.
  Enrollment does not issue these grants. Supervisor admission identifies the
  actual actors that need observation; multiple actors observing one physical
  client need separately authorized relationships.
- Admission validates the full envelope, acquires the exact grant and connection
  gate, then installs a relation. Replays under a request ID must have identical
  content and replay the stored result. Queued transport data is not an installed
  monitor. Bound both active relations and receipt retention.
- Establish local target observation without admitting arbitrary remote controls
  into an application mailbox. A dedicated local native service observer may
  multiplex local target EXITs to its authorized remote relationships; this must
  preserve local topology's target-exit/install ordering and missing-target
  behavior. Use service addresses for services, not invented process identities.
- Remote EXITs use the same native boundary and installed relationship, including
  grant, correlation, target identity and exact connection. Only then does the
  origin receiver deliver a local lifecycle event to its watcher. Never accept
  an arbitrary remote `@pid/events` payload as evidence of a monitored exit.
- Actual task results need a reviewed bounded representation. A value that cannot
  be transferred must be explicitly unavailable, never silently replaced with a
  successful empty result. Admission control is not the EXIT-result codec.
- Target disappearance after authorization is a terminal missing-target outcome.
  Authorization denial must not disclose whether the actor exists.
- Revocation, expiry and connection loss retire relations and outstanding work.
  Loss of the ability to observe is distinct from observed actor completion;
  reconnect cannot revive a retired grant or manufacture an EXIT.
- Remote links remain unsupported until bilateral authorization, installation
  and compensation are implemented. This one-way monitor protocol cannot grant
  failure coupling by implication.

## Acceptance still required

Prove real native mesh installation followed by actor completion while the node
remains connected. Check missing targets, forged EXITs, duplicate and conflicting
requests, lost receipts, late frames after revocation/reconnect, controller and
observer cleanup, bounds, result handling and native/Lua caller cancellation.
Use the same implementation in normal runtime boot and compiled physical clients.
The Bee proof must observe the exact actor and outcome, not merely a FIFO barrier
or any event arriving on the lifecycle topic.
