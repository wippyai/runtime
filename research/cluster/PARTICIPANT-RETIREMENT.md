# Participant retirement: implementation contract

Status: internal inventory transactions implemented, not end to end. NameGuard.Close exists;
production retirement and boot assembly must not be enabled on that alone.

## Guarantee

A participant may leave the Strong exclusion set only after it cannot introduce
or serve a conflicting weaker binding. Losing gossip or timing out a request is
not evidence of this condition. Name retirement does not stop process execution,
revoke a PID already returned to a caller, or fence access to application storage.

## Voluntary sequence

1. Seal and join direct KV registry mutations, then close the shared
   name-admission guard and join its holders. Cancellation leaves
   admission closed; a retry joins the same shutdown rather than reopening it.
2. Seal locally owned EVENTUAL intents and join reassertion/admitted mutation work.
   Withdraw local bindings and produce EVENTUAL tombstones. Continue protocol
   observation/attestation until retirement commits; stopping transport first
   can strand the participant in outstanding reservations.
3. Commit removal of the exact enrolled incarnation, with an inventory-version
   check. Preserve a durable rejection of delayed enrollment for that incarnation.
   A missing active entry is not enough: otherwise an already queued snapshot
   request can enroll the retired incarnation again.
4. After a definitive committed result, stop endpoint/reconciliation before KV
   and relay. An uncertain transaction result leaves the node sealed until its
   exact retirement record is authoritatively resolved.

The internal transaction now keeps active and retired maps under one atomic
version-checked update. A retired stable node consumes its identity slot and
cannot use ordinary enrollment, even with a different incarnation. An explicit
replacement transition exchanges the exact retired predecessor for a fresh
active incarnation. Only one terminal incarnation per stable node is retained;
active plus retired identities share maxParticipants. There is no timer-based
slot reclamation. This bounds retained identities, including identity churn.

Fresh incarnations must never be reused over the lifetime of a stable identity.
The future lifecycle owner must generate them and authorize replacement; neither
knowledge of an old incarnation nor an ordinary snapshot request grants that
permission. These internal primitives currently have no production caller.
Terminal withdrawal, authority for replacement, persistence across restart, and
remote stale-replica behavior remain required before boot integration.

## Resolution and replication boundary

Admission closure alone does not fence reads: LookupContext, EVENTUAL Lookup,
and KV Lookup currently still serve bindings. Do not globally make NameReady a
lookup gate; temporary synchronization loss and terminal retirement have different
semantics, and raw local presence is required for conflict attestation.

EVENTUAL tombstones are asynchronously replicated. Local withdrawal alone does
not prove other nodes have stopped serving old weaker bindings. Acceptance must
establish either that every remaining participant reconciles those conflicts
before acknowledging a new Strong claim, or add an incarnation-aware resolution
rule. Existing PIDs/weaker records do not carry the participant incarnation.
This is a proof obligation, not permission to assume tombstone delivery.

Existing pending reservations retain their captured participant set. Removing a
participant from the current inventory must not silently count its missing ACK
as success. Either preserve completion/expiry under the captured set or implement
an explicit generation-checked transition and test it separately.

## Required tests before boot activation

- Delayed enrollment, snapshot requests, ACKs and unregister after retirement.
- Restart with a new incarnation; old incarnation cannot replace or resurrect it.
- Retirement concurrent with reservation capture/promotion and client refresh.
- Active registration/reassertion during closure; cancellation and repeated join.
- Lost commit reply leaves admission sealed; authoritative resolution is idempotent.
- Remote stale EVENTUAL replicas cannot shadow a subsequently promoted Strong name.
- No application process-stop guarantee inferred from name withdrawal.
- Independent TLS nodes: graceful leave, partition during leave, restart and heal.

## Confirmed cleanup prerequisite

PIDRegistry.Remove previously deleted each name from its reverse index without
checking the current owner. An interleaved unregister/reassignment let old process
exit cleanup erase a replacement process's binding. pid_cleanup_test.go reproduces
that schedule using a held reverse-index mutex. Cleanup now checks semantic PID
identity and conditionally deletes the observed value; cached PID representation
is explicitly covered. This fixes replacement-owner deletion; it is not the
complete retirement protocol or a same-PID registration-generation fence.

## Mutation seal evidence

sealParticipantMutations is an internal lifecycle prerequisite. It refuses and
joins direct mutations without canceling reconciliation or allocating a waiter
goroutine. NameReady remains false even if a concurrent successful refresh sets
the synchronization flag again. Canceled joins remain sealed and can be retried.
The race regression holds an admitted mutation through cancellation, rejects a
subsequent real Consistent registration, then completes refresh and shutdown.
This does not close LOCAL/EVENTUAL admission or withdraw their bindings.

## Weaker-scope withdrawal primitives

PIDRegistry.WithdrawLocal and EVENTUAL Service.WithdrawLocal now require and
close the shared NameGuard. LOCAL clears only its own two indexes; parent and
other scope registries remain untouched. EVENTUAL clears local owned intents,
tombstones all local-origin dots (including ones hidden behind remote winners),
and queues tombstones through its existing replication path. Both are terminal
for this guard/lifecycle; cancellation can be retried.

During EVENTUAL withdrawal, incoming local-origin echoes serialize with the sweep
and become same-counter tombstones. Remote-origin traffic retains the existing
sharded path. The replica remains available for replication; this is not proof
of remote convergence. A replacement must not coexist with the retired replica:
the old lifecycle/transport must be joined before explicit replacement admission.
The raw State API is an infrastructure surface and is not a retirement admission
API. Ordinary runtime registrations pass through the sealed Service guard.

Tests cover remote winner preservation, delayed higher-counter local echoes,
newly observed old local names, canceled closure/retry, parent preservation, and
real EVENTUAL frame encoding/decoding with delayed live frames after tombstones.
These are in-process replicas, not independent native network acceptance.

## Local retirement composition

Internal retireParticipant(ctx, withdraw) now serializes: mutation seal/join,
shared guard closure/join, weaker-scope callback, exact-incarnation committed
retirement. Callback failure retains inventory and leaves admission closed.
A caller-directed retry resolves the same terminal state. No endpoint or boot
caller is wired; callback coverage and endpoint shutdown remain lifecycle-owner
responsibilities, not properties inferred from a successful arbitrary callback.

Closing the shared guard initially prevented Strong observations as well as new
bindings. lockExclusionContext now allows protocol-only conflict observation after
both the application mutation seal and the shared guard's completed drain. It
does not skip conflict checks or incarnation validation, nor admit registration.
During ordinary operation it uses the original per-name guard. This lets captured
pending reservations still receive ACK/reject while withdrawal is in progress.
Tests cover actual LOCAL/EVENTUAL composition, failed withdrawal retaining
inventory, and both ACK and conflict reject after closure.
