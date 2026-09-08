# Terminal broker and mesh mounts

The existing broker remains local: it owns the producer, immutable screen
snapshots, and bounded update watermarks. Mesh mounts add consumers to that
broker without changing the producer's terminal API or transmitting file
handles, Go pointers, or process context.

A mount record binds an opaque random reference to its issuing viewport, exact
recipient PID, independent rights, and lease. Requests arrive through a
dedicated internode receiver whose peer identity comes from the authenticated
connection. The wire has no selectable target surface/process: the owner-side
record chooses both. Generic `process.send` cannot reach this protocol.

Observation uses a notification / snapshot / acknowledgement cycle with at
most one outstanding notification per mount. Intermediate frames coalesce in
the local broker. Each attachment serializes its RPCs and numbers them; the
owner caches its latest response so transport batch replay cannot execute input
twice. A timeout fails the mount rather than retrying an uncertain input action.
Snapshots are whole current frames, not a history of ANSI byte writes. This
keeps initial attachment and skipped revisions independent of a delta baseline.

Limits are explicit: 128 issued mounts per broker, 128 remote views and pending
RPCs, 128 queued incoming requests, four request workers, and a 512 KiB wire
frame limit. The mesh surface queue admits 32 frames per peer, with space reserved
for another 32-frame in-flight batch to survive failed writes without dropping
accepted input. New admission rejects overflow; repeated reconnects cannot grow
the combined queue and in-flight batch beyond 64 frames.
Surface and ordinary application frames take alternating turns after existing
control traffic. Snapshots that exceed the protocol limit close their mount;
there is no unbounded backlog or implicit truncation of terminal content.

Unredeemed grants expire after 30 seconds. Remote requests renew that lease;
clients send an idle renewal every 10 seconds. Closing the issuer, process exit,
explicit revoke, or lease expiry releases the underlying observer and closes its
updates. On disconnect, a mount fails on its request timeout / renewal failure;
a new attachment requires a fresh mount. Already delivered screen contents
remain known to their recipient, as with any read capability.

The security boundary trusts each participating runtime to identify its own
processes. An unrelated peer cannot use a claimed PID to redeem a mount for
another node. A runtime compromised on the authorized recipient's own node is
inside that runtime trust boundary.

Closing a remote view wakes both its in-flight RPC and operations queued behind
it immediately; caller cancellation also interrupts queue admission. Neither
path waits for the peer's five-second response timeout. A close that races with
attachment cannot repopulate the closed view's cached screen. Owner exit and
revocation notify input-only attachments too; they do not depend on an
observation pump or the next heartbeat. Supervised restarts require new mounts
for the new process PID; old references never regain authority.

Local and remote views expose the same cached snapshot and update interface.
A shell can clip their rows into canvas regions and present the composed frame
through one physical Surface, with local controls painted outside those regions.
Translate input coordinates into the focused region. Forward remote input from
a separate Lua coroutine so network waits cannot block local exit controls;
use a bounded input queue and report overflow instead of discarding clicks.
