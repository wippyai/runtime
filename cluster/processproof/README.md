# Three-process cluster proof

This test is the first real-process cluster milestone. It launches three
separate copies of the Go test binary and boots each through the production
bootstrap context and component loader:

```text
metrics -> cluster (memberlist + authenticated internode TLS) -> topology -> raft
```

The parent generates a per-run memberlist symmetric-encryption secret, three
Ed25519 internode identities with static peer pins, and a short-lived local CA
with one mutual-TLS leaf credential per node. File credentials live in
`t.TempDir()` with mode `0600`; the memberlist secret and internode identity
material are passed to the child test binary in its private environment and
are never logged.
Each child exposes a Unix-domain control socket only for the duration of the
test. It is not a runtime API and does not exist in production builds. The
parent treats a child as ready only after the loader has returned successfully
and both the Raft and global registry services are present; socket creation by
itself is not readiness.

Run it on a host that permits local loopback TCP and UDP sockets plus Unix
domain sockets:

```sh
go test ./cluster/processproof -run TestThreeProcessStrongClaimAndRevoke -count=1 -timeout=70s -v
```

The proof dynamically obtains three loopback TCP ports before child startup.
That is intentionally not a fixed shared-port allocation, but there is an
unavoidable close/rebind race on a busy host; a bind conflict fails the test
instead of selecting an unsafe fallback. It is a bounded local proof, not a
100-node benchmark or a network-fault test.

Success proves that three independent runtime OS processes form a
memberlist-encrypted gossip cluster with TLS-protected internode traffic,
converge on three Raft voters and one leader, then commit and revoke
one `STRONG` name claim. The test reads the owner and the post-revoke absence
from every voter process, then requires every child to acknowledge shutdown and
exit cleanly. A child that exits early, fails to acknowledge shutdown, or needs
forced termination fails the test. Partial boot failures are also cleaned up
inside the helper before the child reports its error.

It skips under `go test -short` and on Windows, so ordinary constrained CI can
run unit tests without requiring local socket admission. It does not replace
separate-process fault injection or capacity testing.
