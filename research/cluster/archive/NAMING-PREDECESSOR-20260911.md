# Cluster hardening recovery — 2026-09-11

Persistent checkout: /home/wolfy-j/wippy/runtime-naming-hardening-20260911
Base: 1c071948fb (surviving main workspace HEAD, not the former integration).

The previously reported /tmp/wippy-cluster-integration-20260910 and
/tmp/wippy-eventual-reassert-20260911 are absent. Their uncommitted changes,
including Stream setup, negotiated payload limits, boot assembly, composed
lookup and broader lifecycle changes, are not verified present here. Do not
claim the prior integration recovered or release-ready. Preserve dirty work
in the main workspace. No commits, pushes or merges made in this recovery.

## Fresh evidence

TestRevokeForStrongUsesLocalOrigin failed on this base in all three cases:
local keep incorrectly deleted, remote keep incorrectly suppressing local
revocation, and unrelated keep notifying the remote winner instead of local
owner. Fix checks local-origin PID and tombstones under the same State shard
lock. Same-owner intent survives; conflicting intent is disarmed even if its
local dot has already been tombstoned. Register/reassert/unregister serialize
local state changes with owned-intent changes. Notifications remain outside
ownedMu. Ordinary read paths and remote merge rules are unchanged.

Tests additionally ensure withdrawal cannot reassert already tombstoned intent.
Validation log: ../runtime-naming-hardening-20260911-validation.log (absolute
/home/wolfy-j/wippy/runtime-naming-hardening-20260911-validation.log).
Command: GOWORK=off go test -race ./system/topology/namereg/eventual
./system/topology/namereg/global ./system/topology -count=1

## Still outstanding

This does not implement a whole-domain admission barrier or lifecycle seal.
Cross-scope reservation admission/reassertion, Strong normal CLI boot,
shutdown completion proofs, snapshot consistency, restart identity fencing,
shared transport fairness, native cross-node Stream integration and real
multinode/toxic/load/recovery acceptance remain open. Recover prior work from
persistent artifacts/branches if possible before recreating it. Old performance
figures are historical evidence, not validation of this checkout. Rodrigo
review and coherent PR integration remain outstanding.
