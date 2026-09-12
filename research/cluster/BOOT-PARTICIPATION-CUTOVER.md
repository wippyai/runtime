# Remaining boot participation cutover

Observed in the integration checkout on 2026-09-11. This is an implementation
map, not a claim that participation is boot-enabled.

- boot/components/system/raft.go: loadClientRegistry configures a forwarding
  registry without ConfigureStrong/ConfigureParticipation. Member Load supplies
  discovery Membership, not Incarnation or committed participation. Both paths
  register the raw KV registry host and call StartReconciler/StopReconciler.
- NewParticipantEndpoint already composes bounded native snapshot authority and
  forwarding client on the existing sysreg host. It needs to replace that raw
  host consistently on all naming participants; no mixed-mode fallback.
- topology.go creates the shared NameGuard and PIDRegistry. eventualreg.go uses
  that guard; both concrete services now support WithdrawLocal. Boot must supply
  all actual weaker registries to retirement, not a success-only callback.
- cluster.go's node name defaults to hostname. An authenticated stable node ID
  and an individual naming lifetime are different identities. A new random token
  alone cannot authorize replacing a committed old incarnation.
- cmd/app/deployment.go holds an application deployment lock. That is not an
  established cluster-wide node-identity ownership guarantee. Do not infer that
  all runtimes carrying the same signing key/node ID share that lock or disk.
- nodeDataDir defaults to ~/.wippy/store, or configured cluster.raft.data_dir.
  Diskless operation currently exists. Any persisted local incarnation policy
  must define its behavior for clients and diskless nodes explicitly.

## Required sequencing

Construction must configure Strong/incarnation/inventory, install the participant
endpoint as sysreg host, start underlying KV/transport, then bootstrap the endpoint
before application admission. Shutdown seals mutations and the shared guard,
withdraws weaker bindings, commits exact retirement, joins endpoint before
KV/transport. An uncertain retirement result stays sealed and requires resolution.

Crash replacement still needs an enforced ownership/admission rule. Neither
missing gossip nor elapsed time proves the old process cannot act. A local file
lock could prove one local state-directory owner, but does not fence duplicate
identity on a different host or copied state. The authority accepting replacement
must know what ownership evidence is sufficient for the supported deployment
model. The internal replace transition is not itself that evidence.

## Verified persistence boundary

TestParticipantRetirementSurvivesFullRaftRestart uses three real Raft members
with durable directories, commits enroll/retire/replace/retire, stops every member,
rebuilds all from disk, and checks retired enrollment refusal, wrong-predecessor
refusal, stale reservation-CAS refusal and explicit new replacement admission.
Reads use RaftEngine.GetLinearizable; merely observing IsLeader is insufficient
because the new leader may still be applying its log. The test explicitly waits
for local replica application before shutdown, not forwarded reads.

This is real disk recovery with in-process transport. Independent-process native
TLS recovery is a separate test; the two are not yet a native Raft/boot proof.

## Native lifecycle retirement seam implemented

ParticipantEndpoint.Retire(ctx, withdraw) now exposes the existing seal/join,
shared-guard closure, weaker withdrawal and exact retirement sequence to the
trusted runtime lifecycle owner. Existing authenticated KV transaction forwarding
carries the mutation for clients; no new retirement wire operation is necessary.
Stop cancels and joins accepted Retire calls as well as Start calls. A timed-out
Stop can be retried; it does not silently abandon a blocked withdrawal callback.
Retire is called before Stop while KV and transport remain available. Failure
retains any completed seals and does not authorize replacement.

Focused endpoint/withdrawal race tests pass1.059s. New real3-member Raft forwarding
client proof TestE2E_ForwardingParticipantRetiresThroughExistingKV passes1.83s
(package2.853s): actual LOCAL/EVENTUAL withdrawal, permanent admission closure,
then stopped/partitioned client no longer blocks a fresh Strong reservation.
This uses in-process relay; it is not boot/native TLS retirement acceptance.

Remaining boot prerequisites verified during read-only Luna audit and root source
review:

- boot/infrastructure.go derives relay identity from relay.node_name or stable
  host+working-directory UUID. boot/components/system/cluster.go derives native
  authenticated identity from cluster.name or hostname. No unification is present
  at those construction sites. Resolve this before provenance-bound enrollment;
  do not weaken ReceivedFrom checks to accommodate different identities.
- Raft and EventualReg both depend on Cluster+Topology, with no ordering edge
  between them. Explicitly guarantee Eventual availability through retirement;
  inspect full boot load/start/stop context behavior before changing dependencies.
- First enrollment and voluntary retirement are available; exact replacement
  remains internal and needs a trusted ownership/recovery contract. A fresh token,
  copied state, or network timeout is not proof that the previous runtime stopped.
- The legacy FSM registry path has no participation endpoint integration. This
  candidate cutover must not accidentally treat it as enrolled KV participation.

No commits/pushes/merges. Boot remains incomplete.

## Unified bootstrap routing and authenticated node identity

Previous turn implemented/verified endpoint retirement. User called out3days elapsed;
full goal is still not close to acceptance. Prioritize integrated boot/native
harness over additional isolated component polishing. No numerical ETA claimed.

Reproduced bootstrap cluster-only name ignored by relay and conflicting explicit
relay/cluster names accepted (TestBootstrapClusterAndRelayIdentity failed before
fix after correcting the test's event.Bus cleanup interface).
Bootstrap now chooses one name before allocating infrastructure: enabled explicit
cluster.name and relay.node_name must agree; either explicit value supplies relay
identity; neither retains stable local default/environment behavior. Disabled
cluster config does not alter local naming. Cluster.Load derives its default
from the actual relay identity and rejects mismatch for native embedders too.
No weakening of connection-derived provenance checks. Removed independent hostname
fallback from cluster.Load.

GOWORK=off go test -race ./boot ./boot/components/system passed1.087s/6.265s.
New native-listener tests for omitted cluster.name and mismatched name passed1.134s.
Matching membership/relay identity asserted. Log:
/home/wolfy-j/wippy/cluster-identity-check-20260911.log.

Boot participant integration and restart/replacement ownership, native100node
mixed/toxic load acceptance, and cross-node Streams remain incomplete. No
commits/pushes/merges. Goal remains active.

## KV boot participation cutover implemented; first real boot proof passes

Previous turn reproduced/fixed unified node identity. This continuation replaces
raw sysreg host/reconciler boot wiring with ParticipantEndpoint on BOTH Raft members
and forwarding clients. Shared configureNamingParticipant supplies fresh crypto
incarnation, NameGuard, actual LOCAL/EVENTUAL conflict/revocation hooks, bounded
inventory and configurable snapshot/refresh limits. Strong reservation inventory
has no discovery fallback in the KV boot path. FSM backend remains separate.

Raft now depends on EventualReg as well as Cluster/Topology, so Eventual is loaded
and started first and remains available until retirement. Successful Start records
an owned participant lifetime; Stop calls Retire with actual LOCAL+EVENTUAL
WithdrawLocal, then endpoint Stop, then KV/Raft shutdown. Failed/uncertain retirement
returns error without claiming cleanup/replacement. Failed endpoint startup remains
owned and can be stopped without inventing retirement success. Limits documented
under raft.naming in cluster.example.yaml.

Integration uncovered a real gap: failed bootstrap clears reconciler owner, and
admitMutation formerly treated nil owner as standalone and forwarded writes.
Configured participants now return globalapi.ErrNotReady with no owner. The
client boot test reproduced the forwarded write after failed startup before fix;
it now checks authority-required admission and failed-start cleanup. Existing
Strong registration test now starts its real participant lifecycle before writing.

Actual Topology/Cluster/EventualReg/Raft components in the native mutual-TLS boot
fixture now start a single durable Raft node, enroll, successfully register Strong,
and retire in reverse dependency order before transport cancellation. The first
run reached shutdown but test cleanup called non-idempotent Topology.Stop twice;
test now removes already-stopped components from its cleanup list. No unrelated
Topology lifecycle change was made.

GOWORK=off go test -race ./boot/components/system ./system/topology/namereg/kvbacked
passed12.129s/29.780s. Log:
/home/wolfy-j/wippy/boot-participation-cutover-20260911.log.
Diff check passed. Native independent-TLS-process snapshot test also passed13.5s
in Luna harness audit, but is not full boot/Raft evidence.

Remaining RELEASE BLOCKERS, not solved by this cutover:
- Fresh incarnation enrollment still refuses an already active/retired stable
  identity. Explicit restart/replacement ownership and durable recovery remain
  unfinished; ordinary repeated boot of existing cluster state is not yet ready.
- Initial client startup currently requires visible authority (fails fast).
- Whole-cluster shutdown loses quorum before every member can commit retirement;
  cancellation can also close transport before retirement. Never claim every
  shutdown is graceful or that missing retirement proves processes stopped.
- Native3server+client subprocess proof is delegated to Luna in NEW
  cluster_participant_process_test.go; keep runtime edits root-owned. Then native
  100node/mixed/toxic acceptance and Streams remain.

No commits/pushes/merges. Goal active and incomplete.

## Shutdown preserves uncertainty without leaking local Raft

Previous continuation implemented KV boot participation and first real boot proof.
A new native TLS boot cancellation test reproduced Raft remaining Leader after
Stop returned retirement context.Canceled: early return skipped all local cleanup.
Before-fix log: /home/wolfy-j/wippy/boot-canceled-retirement-before-20260911.log.

Boot Stop now retains the retirement error, marks the attempt terminal (no implicit
mutation retry), joins endpoint work, completes actual LOCAL/EVENTUAL withdrawal
if cancellation prevented retirement entry, and then stops KV/Raft. A failed join
or withdrawal still reports failure rather than pretending cleanup completed.
An unresolved committed participant remains retained; local shutdown completion
is not replacement authorization. The same retirement error is returned after
successful cleanup, including subsequent Stop calls.

Native TLS boot regression passed6.078s initially. Expanded it to assert retained
inventory bytes, closed shared admission guard, and Raft Shutdown after canceled
retirement. Full TestClusterBootTLSUsesNativeManager -race passed10.634s, including
normal TLS, invalid TLS, successful naming retirement, and canceled naming cleanup.
Log /home/wolfy-j/wippy/boot-retirement-shutdown-20260911.log.

Independent3server+client boot proof remains active in Luna harness agent.
Restart/replacement ownership, full100node/mixed/toxic acceptance and Streams still
unfinished. No commits/pushes/merges; goal remains active.
