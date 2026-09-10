# Concurrent Lua / PTY mesh proof

See the [Lua agent guide](LUA_GUIDE.md) for the cross-node API workflow and
the [V1 assessment](V1_REVIEW.md) for evidence and remaining shared-mesh limits.

This harness runs 2–8 independent Wippy actor schedulers and the real internode
connection manager in a fully connected mesh. Each node starts a Bash PTY behind
the existing Lua `exec` and terminal proxy APIs. Agents drive the next node in
a ring concurrently, so every runtime both produces and consumes a surface.
Each agent has a separate observation reference and input/resize reference.

By default it verifies 20 commands per node, screen contents after ANSI clear and
cursor commands, resize propagation, observe-only input rejection, and
input-only snapshot/update rejection. The marker is split in the echoed shell
command, so a matching screen proves command execution rather than input echo.
One scheduler worker per node verifies that remote waits yield.

Build and run locally:

```bash
GOWORK=off go build -o /tmp/wippy-tty-mesh-proof ./tests/tty-mesh
python3 tests/tty-mesh/run.py
```

Run across an authorized SSH host (Linux, x86-64 when using the same binary):

```bash
python3 tests/tty-mesh/run.py \
  --ssh user@REMOTE_IP \
  --peer-address REMOTE_IP \
  --local-address LOCAL_REACHABLE_IP
```

The hosts need Bash, SSH/SCP connectivity, and reachability on the selected mesh
ports (`--port` through `--port + --nodes - 1`, starting at 19470). SSH bootstraps the
second test process and exchanges its process-bound references. All terminal
input, snapshots, updates, and operation responses travel over the real mesh.
The proof generates temporary Ed25519 identities and mutual TLS credentials;
existing cluster credentials and running Wippy instances are not used or changed.
Temporary binaries, keys, and references are removed afterward.

The executable accepts a single real native executor through a small fixture
registry; Lua runs its normal `exec.get` / `exec.run` permission checks. This
keeps the proof independent of registry databases and application boot YAML.
It does not add an unrestricted remote execution endpoint to the runtime.

The reported p50/p95 is elapsed time from issuing a shell command in Lua until
that agent sees the command's resulting screen, including PTY/VT batching and
network round trips. All peer connections must be established before agents run;
reports include peer counts. Producers remain alive until every agent reports
success, then the orchestrator releases all nodes together. Shutdown cancels
runtime work before waiting for the scheduler. SSH never reads terminal stdin,
and bootstrap commands have bounded timeouts.

Exercise eight runtimes with 200 commands each by adding
`--nodes 8 --commands 200` to either command above. In SSH mode node b runs remotely and the other
runtimes run locally. This is an eight-runtime test on two physical hosts,
not a capacity claim for eight hosts or a larger production mesh.

Deterministic permission, replay, cancellation, queue, stalled-peer isolation,
and supervisor-controller restart tests live in `system/tty` and
`cluster/internode`. The restart tests use the process OnComplete barrier and
verify that owner/consumer crashes invalidate old mounts and that restarted
processes need fresh references. They do not boot registry-driven application
supervision.

Measure cached observation and coalescing with 120×40 frames and 1/8/32 observers:

```bash
GOWORK=off go test ./system/tty -run '^$' \
  -bench 'BenchmarkMeshSnapshotFanout|BenchmarkRemoteCachedSnapshot' -benchmem
```

These in-memory transport benchmarks include snapshot encoding/decoding and
report coalesced wire frames per presentation; they do not measure network RTT.
