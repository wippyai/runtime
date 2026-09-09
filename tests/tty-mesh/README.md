# Concurrent Lua / PTY mesh proof

See the [Lua agent guide](LUA_GUIDE.md) for the cross-node API workflow and
the [V1 assessment](V1_REVIEW.md) for evidence and remaining shared-mesh limits.

This harness runs 2–16 independent Wippy actor schedulers and the real internode
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

## Remote image proof

The same harness can run native Lua PNG producers and image observers instead
of Bash PTYs. This is the actual typed `tty.image` → virtual surface → native
TLS mesh → `view:capture` → `capture:image` path, with one Lua worker per node:

```sh
GOWORK=off go build -o /tmp/wippy-tty-images-mesh-proof ./tests/tty-mesh
python3 tests/tty-mesh/run.py --binary /tmp/wippy-tty-images-mesh-proof \
  --images --nodes 2 --commands 20
```

The generated PNG is 262,586 bytes and requires 17 resource chunks. The final
report verifies exactly that many image bytes sent by each producer even after
repeated movement and captures. Lua compares the full received PNG with the
producer fixture, rejects image access through an input-only mount, and proves
an explicitly retained image survives observer detach. The harness also accepts
`--nodes 8` and the existing authorized SSH options.

Image mode's latency measures input → snapshot → retained capture/export;
text mode measures Bash command completion. Their p95 values describe different
workloads and must not be compared as a before/after performance claim. The
separate deterministic stalled-chunk test asserts that input makes progress
while a blob reply is blocked.

This fixture explicitly advertises image support because both binaries are built
from the same source. Normal boot uses `tty_surface_graphics=1` metadata plus an
attach acknowledgment. The embedded cluster stack does not install a TTY broker
by itself and does not advertise graphics automatically; an embedding must wire
the service/transport and advertise only capabilities it actually implements.

Measure repeated captures after warming the attachment cache:

```sh
GOWORK=off go test ./system/tty -run '^$' \
  -bench BenchmarkMeshImageCaptureWarm -benchmem
```

The benchmark asserts no further image chunks and reports metadata wire bytes
per capture. It uses an in-memory transport, not a network latency simulation.

For sustained resource traffic, add `--image-churn`: producers alternate two
different PNGs on every command, forcing the attachment cache to fetch again.
The harness checks the exact total transferred bytes. For example:

```sh
python3 tests/tty-mesh/run.py --binary /tmp/wippy-tty-images-mesh-proof \
  --images --image-churn --nodes 16 --commands 200
```

Local validation with 16 processes and 15 connected peers each:
- 1,000 cached commands per node: p95 3.66–4.71 ms; one PNG transfer per node.
- 200 changing-image commands per node: 844,476,576 total image bytes,
  p95 26.37–31.34 ms; 1,573,748 charged image-store bytes per node at completion.
- Repeated replacement/deletion test checks cache accounting returns to zero
  after each of 100 cycles, under the race detector.

These are ring workloads on loopback: every node is connected to every other
node but drives one neighbor. They do not establish all-to-all observation
capacity, WAN latency, peak RSS, fairness under arbitrary application traffic,
or performance at saturation. The changing-image workload measures input through
completed capture; it is not an independent keystroke latency measurement.
