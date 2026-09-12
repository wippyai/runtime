# Naming participation across voter and client nodes

Status: implementation design; not a delivered capability.
Base: 07cf4a6f4bc051acde07afb794b23fbdd978a482 plus current recovery candidate.

## Confirmed blocker

Boot's StrongDeps.Membership includes every gossip node. Raft-free clients have
an empty local KV FSM and no Strong configuration or pending-exclusion feed.
They can resolve and forward Consistent names, but cannot acknowledge Strong
reservations. TestCharacterizationStrongCannotCompleteWithUnenrolledClient
reproduces the missing-client acknowledgement with three real Raft engines and
one forwarding client. It uses the in-process harness, not TLS sockets.

Removing clients from the required set alone is invalid: a client can still
admit LOCAL/EVENTUAL claims. Strong's exclusion promise must cover every node
admitted to that namespace, regardless of whether it votes in Raft.

## Required model

Separate transport discovery, Raft membership and naming participation. Keep the
small Raft voter group. A node hosting names joins a naming domain as an exact
incarnation. A discovery-only connection need not participate, but cannot use
that exemption to publish names in the domain. This is generic runtime policy;
no workspace, tab or Bee identity belongs in these records.

Authority must commit participation changes. Gossip is a discovery/failure hint,
not sufficient proof that an old participant can no longer admit conflicting
names. A restart cannot reuse an old acknowledgement solely because its node
name matches. Participation and acknowledgements need incarnation identity.

The joining node keeps cross-scope admission closed until it has installed an
authoritative snapshot of active and pending exclusions, resolved local conflicts,
and caught up with ordered updates through an agreed revision. Snapshot transfer
and subsequent updates must have a contiguous handoff, not independent scans
plus best-effort gossip. A gap or invalid update closes admission and requests
resynchronization. Failed reads never mean release.

Participant enrollment must serialize with creation of reservations. The
reservation transaction checks the participant revision it snapshots; promotion
checks the required participant/incarnation acknowledgements. A joining node
cannot become name-ready in a window where a reservation omitted it and it did
not learn that reservation. Existing all-required exclusion semantics stay intact.

An acknowledgement means the incarnation holds an exclusion and will refuse
competing weaker claims. It does not mean another process stopped executing.
Claim withdrawal, transport loss and owner cancellation remain separate from
execution completion and external-resource fencing.

## Transport and recovery

Carry bounded snapshot/update messages through the authenticated native runtime
mesh, reusing its lifecycle and encoding conventions. Bind admission to the
actual authenticated peer/incarnation, not a claimed node field in a payload.
Do not introduce a sidecar transport or assume the lost integration's
service_transport.go exists: it is absent on this recovered base.

Use explicit protocol versioning suitable for later W2 implementations, while
leaving W2 unchanged. Define limits on snapshot bytes, entries, queued updates
and outstanding requests. If a consumer falls behind, stop admission and restart
from a snapshot rather than accumulating an unbounded update queue.

Do not invent expiry constants to prove exclusion. Authority-side retirement
needs an explicit fencing model. If leases are chosen, specify their timing and
pause assumptions and enforce them at every relevant admission seam. A peer
suspected by gossip may still run and accept traffic; deleting its required vote
is not by itself a safe retirement protocol.

## Implementation sequence and proof

1. Specify authority records, participant incarnation lifecycle and enrollment
   transaction boundaries; audit current DropNode/prune behavior against them.
2. Implement revision-coherent snapshot plus resumable update enrollment with
   bounded storage and cancellation, first against an actual Raft engine.
3. Bind peer/incarnation identity and integrate client boot/readiness. Reuse the
   shared local admission guard and joined reconciler lifecycle already added.
4. Upgrade the characterization to an acceptance test: three voters plus a
   nonvoter client successfully claim Strong and every participant refuses a
   conflicting weaker claim. Preserve same-owner upgrade behavior.
5. Exercise snapshot cutover concurrent with claim creation/promotion/deletion;
   client crash/restart, stale acknowledgements and late messages; lost updates;
   slow/disconnected clients; authority failover; and explicit retirement.
6. Run independent CLI processes over native TLS, then toxic-network and 100-node
   load/idle/recovery measurements. An in-process green test is not that proof.

Outstanding acceptance question: how retirement proves an excluded incarnation
cannot continue weaker-scope admission after partition or a paused process
resumes. Resolve this before treating dynamic participant removal as safe.

## Retirement characterization

TestCharacterizationMembershipPruningDoesNotFenceClient now demonstrates the
failure mode with actual Raft-backed registries and a forwarding client. The
client first acquires a LOCAL name. A conflicting Strong claim remains pending
while the client is in the required set. The test removes the client only from
the authority's membership view; it does not kill the client. Survivor promotion
then succeeds while the client remains name-ready and retains the old LOCAL
binding. This is a simulated discovery-view change over the in-process harness,
not a native-network partition test.

This proves current pruning does not provide retirement evidence. It does not
justify automatically removing more participants to improve availability. The
replacement must define when a participant is fenced from the naming domain,
including admission and name-based routing after restart or pause. Explicit PID
routing and execution completion remain separate contracts.

The existing LookupContext preserves authority lookup errors, but the raw LOCAL
attestation accessor intentionally remains visible. Any domain-readiness/fencing
rule must apply at the public routing/admission seams without hiding raw evidence
from Strong conflict checks. CrossScopeChecker's bool-only LookupOther still
cannot express an authority error; audit that before treating enrollment closure
as proof that weaker claims cannot slip through.

## Discovery omission proof and retirement boundary

A second characterization, TestCharacterizationNewStrongOmitsLiveUndiscoveredClient,
removes the live client from the discovery view BEFORE creating the reservation.
Strong succeeds while the client's conflicting LOCAL binding remains ready.
Both characterizations pass under the race detector with actual three-node Raft
engines (in-process transport). These tests assert the current defect, not safety.
This rules out a fix confined to pruneDepartedRequired or DropNode: requiredNodes
must come from committed naming participation, including suspected participants.

The implementation must distinguish these transitions:

* Suspected/disconnected: retain the participant and its exclusion obligations.
  A transport observation grants no authority to delete claims or acknowledgements.
* Voluntary retirement: close local admission, join in-flight admission work,
  withdraw the participant's weaker bindings, and acknowledge retirement for the
  exact incarnation. Commit that acknowledgement before excluding the participant
  from future reservation sets. Restart requires a fresh enrollment; the retiring
  incarnation may not reopen based on a local reconnect alone.
* Unresponsive retirement: requires enforceable fencing, not a timeout renamed
  as a proof. Without that mechanism a Strong operation may expire unavailable;
  it must not report successful exclusion of an executing isolated participant.

This is also a performance boundary. Fast local admission and automatic forced
retirement under arbitrary pauses cannot both be guaranteed solely by gossip.
A lease alternative needs an explicit clock/pause model and checks covering
existing bindings/lookup as well as new registration. Checking only Register
would leave earlier conflicting bindings visible. Per-operation quorum checking
would change latency/availability of LOCAL semantics and is not an implicit fix.

Next design review must select and prove the fencing mechanism before enabling
unresponsive participant removal. Committed participation revision must be checked
in the same transaction that creates each pending reservation, preventing a join
from racing the required-set snapshot. No wire or runtime behavior changed by this
research checkpoint.

## Candidate simplification: snapshot replacement, not event replay

The earlier requirement for replaying every update is stronger than naming needs.
Evaluate an authoritative full-state refresh protocol before adding a retained
log. A watch/invalidation is a wakeup hint only. A client refreshes a complete
bounded snapshot of exclusions; it never applies partial scans as deletions.
The revision labels the captured state, not the highest event it happened to see.

The safety argument depends on committed participation (not present today):

1. An enrolled incarnation appears in the participant set used by every new
   pending reservation. Promotion requires its exact-incarnation acknowledgement.
2. The incarnation installs an exclusion before acknowledging that reservation.
   It retains the exclusion across transport loss and intermediate refreshes.
3. If a pending reservation was created and canceled between two snapshots,
   missing that intermediate state is harmless: it never became authoritative.
4. If it promoted, the participant must already hold its exclusion. A missed
   notification cannot cause it to admit a competing weaker claim.
5. A complete newer authority snapshot can release an exclusion absent from that
   snapshot. If a subsequent reservation reused the same name, it again needs
   the participant's acknowledgement before promotion. A delayed prior snapshot
   must not overwrite a newer locally installed reservation (existing generation
   guards matter here as well as the captured authority revision).
6. Enrollment closes weaker admission and installs the snapshot before its
   participation becomes usable. The commit validating readiness must serialize
   against reservation creation; clients cannot reopen from an unchecked stale
   snapshot. Joining cannot rely on gossip enumeration.

This permits coalescing arbitrary invalidations without retaining intermediate
updates. It does NOT permit arbitrary participant removal or trusting a partial,
unbarriered, malformed or old-incarnation snapshot. A revision alone also does
not prove a sender is the current authority; requests need native peer identity
and correlated enrollment identity.

Proposed resource contract for review:

* At most one refresh in flight per participant, plus one dirty bit. Writes
  never wait for a slow snapshot reader and do not enqueue one item per change.
* Snapshot capture uses one immutable authority view after the authority read
  barrier. Prefix scans must share that view; two independent scans are invalid.
* Bound entries, encoded bytes and concurrent snapshots explicitly. Exceeding a
  bound fails the entire refresh; it never returns a truncated authoritative set.
* A blocked/offline participant delays Strong availability, not Raft Apply or
  unrelated Consistent mutations. Existing exclusions remain conservative.
* Missing wakeups affect refresh latency only when a separate bounded refresh
  request mechanism exists. Do not claim that today's Watch provides this: its
  unbounded bus action queue must not sit on this path.
* No per-entry replay history or legacy protocol is necessary if the argument
  survives enrollment, deletion/recreation, authority failover and incarnation
  replacement tests. Wire format and implementation are still outstanding.

Review status: root proposed this simplification; independent counterexample
review requested from the Luna audit lane. This section is a candidate protocol,
not verified runtime behavior and not permission to weaken participation checks.

### Enrollment ordering refinement

A final readiness CAS may be avoidable by committing enrollment BEFORE snapshot
capture. This is a candidate refinement to step 6 above:

1. The new incarnation starts with weaker admission closed. Authority commits it
   into the required participant inventory immediately (including enrolling nodes).
2. Reservation creation reads this inventory and checks its exact KV version in
   the pending-creation transaction. If enrollment races that read, creation
   retries instead of committing an obsolete required set.
3. After enrollment acknowledgement, obtain a barriered complete snapshot.
   Install active AND pending exclusions and resolve existing local conflicts.
4. Open local admission only after successful installation for that incarnation.
   No separate transition may temporarily omit the incarnation from reservations.

Why reservations begun before enrollment remain covered: if still pending at
capture they are installed as exclusions, even if this incarnation is not in their
original required set. If promoted before capture, their active record is present.
If already terminal and absent, they cannot later promote. Any reservation begun
after enrollment includes the new incarnation and cannot promote without its ACK.
This depends on an atomic snapshot and version-guarded participant inventory;
it is not true of the existing gossip callback.

The entering incarnation may remain a required participant after failed snapshot
installation. Retry uses the same admitted incarnation or explicit retirement;
a failed RPC is not authority to forget its participation. This deliberately makes
failure visible as unavailable Strong operations instead of false success.

## Internal inventory implementation checkpoint

`kvbacked/participants.go` now supplies the staged committed inventory primitive:
exact `(NodeID, incarnation)` enrollment, no implicit replacement/removal, bounded
cardinality, context checks around reads, and no retry after a transaction error.
A snapshot returns the decoded participants with the exact inventory-version
condition to include in the reservation transaction.

`participant_reservation.go` prepares actual pending-key transaction operations
using that condition and a sorted node list plus RequiredIncarnations map. Tests
exercise a concurrent enrollment between preparation and commit: the stale
transaction writes neither pending nor active state; preparation from the newer
inventory succeeds with both incarnations.

These helpers are intentionally not wired into boot or current registration.
The pending decoder now validates incarnation maps. ACK/reject keys for those
records bind name, reservation epoch, node and incarnation without a NodeID-only
fallback. The local participant must match the expected incarnation to vote;
vote creation checks the live pending version, and promotion/expiry use the same
qualified keys. Active records retain the participant map. Gossip pruning and
automatic node cleanup preserve bound reservations/claims.

The existing unbound record path remains for current boot/tests during this
internal integration; it is not a negotiated older-node protocol. Boot still
must move to the inventory as one coordinated cutover. Do not enable inventory
reservation creation until admission/snapshot installation and authenticated
incarnation enrollment are connected. Explicit process-exit and administrative
removal still require incarnation/fencing review; only discovery cleanup has
been guarded here.

This checkpoint establishes the CAS boundary with a real standalone KV engine.
It does not establish wire authorization, client readiness, or multihost Strong
availability. No public Lua API or Bee concept was introduced.

## Native trust boundary verified during integration

The current cluster admits trusted native runtimes, not mutually distrustful
native tenants. Raft voters necessarily belong to this trust domain. Current
non-voter clients also hold raw forwarded KV access; their role avoids voting,
but does not impose a per-key security sandbox. Application Lua code is a separate
boundary: validated store namespaces exclude `_sys`, and physical key mapping
always prefixes the application namespace. It receives store resources, not the
raw node-wide engine.

The authority protocol must still derive the immediate peer from native receive
metadata and bind an enrollment incarnation to it. Claimed Source alone is not
that evidence. Current runtime forwarding replies are bound to the expected
immediate peer and storekv host. The forwarding host rejects non-KV domains,
ordinary application source hosts, and connection/source-node mismatches.
These are protocol boundaries within the native trust domain, not cryptographic
proof of a remote process's service privileges.

Do not introduce a generic prefix-policy registry merely to restate this trust
model. Preserve ordinary application store behavior and move enrollment/feed
operations through the naming authority as planned. If restricted native clients
become a supported product requirement, generic raw `_sys` forwarding must then
be denied to those principals and replaced by semantic authorized operations;
adding an incarnation token alone does not enforce that restriction. A native
client is not restricted simply because it is a Raft non-voter.

## Snapshot relay protocol integration contract

Use one versioned naming service payload inside existing native relay packages.
Requests carry a nonzero correlation and the entering incarnation, never a
caller-selected node identity. The receiving authority derives the node from the
immediate authenticated peer. Direct-to-authority requests are the initial path;
a follower response must not masquerade as an authoritative snapshot or invent
original-peer delegation. The caller can resolve the authority and retry reads;
uncertain enrollment writes retain their established uncertainty semantics.

The payload codec uses the runtime's MessagePack library, explicit compact field
names, version 1, mandatory fields, and rejects unknown versions and trailing
bytes. Snapshot replies carry one coherent authority revision and bounded key /
value / version / epoch records. Correlations bind to the selected peer and the
current client lifetime. A codec must not derive authentication from its fields.
No legacy negotiation or separate transport stack is needed.

Limits apply at separate points: request bytes before decode, declared entry
count before array allocation, encoded reply bytes and retained buffer capacity,
concurrent accepted requests, and queued replies. Existing internode frame limits
(current default 512 MiB) precede service decoding and are not reduced by a
service's smaller snapshot cap. Do not describe service limits as a complete
connection-level memory bound. Client feed cancellation must reach request waits
and SendContext admission; accepted remote work may still finish and must remain
bounded and owned. No detached goroutine may stand in for cancellation.
