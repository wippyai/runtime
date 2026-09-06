# Bidirectional Lua / PTY mesh proof

This harness runs two independent Wippy actor schedulers and the real internode
connection manager. Each node starts a Bash PTY behind the existing Lua `exec`
and terminal proxy APIs, then mounts the other node's viewport from a Lua agent.
Each agent has a separate observation reference and input/resize reference.

It verifies 20 commands in each direction, screen contents after ANSI clear and
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
ports (19470/19471 by default; `--port` changes the pair). SSH bootstraps the
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
network round trips. These measurements are a smoke benchmark, not a capacity
claim for a larger mesh. Deterministic permission, replay, cancellation, queue,
and lifecycle tests live in `system/tty` and `cluster/internode`.
