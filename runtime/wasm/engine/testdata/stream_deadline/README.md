# WASI TCP stream deadline fixture

This Rust component uses standard WASI 0.2.8 socket, stream, and poll imports.
It reuses the vendored WIT definitions from `../tcp/wit`; there is no build-time
Asyncify step. The runtime applies its embedded transformer.

After connecting to `127.0.0.1:8099`, the guest reads one mode byte:

| Mode | Operation |
| --- | --- |
| R | Stalled blocking read |
| K | Stalled blocking skip |
| W | Partial blocking write and flush |
| Z | Partial blocking write-zeroes and flush |
| F | Nonblocking write followed by stalled blocking flush |
| S | Blocking splice with a full output ring |
| I | Generic input subscription waits beyond the socket timeout |
| D | Successful blocking write whose completion reaches the guest after its deadline |

Timeout cases require `last-operation-failed` with an owned error whose debug
string explains the timeout, followed by closed input and output streams. Each
case drops streams, socket, and network before returning. The Go harness uses
controlled `net.Pipe` connections that reject read/write deadline setters,
exercises the real dispatcher and embedded Asyncify, checks partial payloads,
and verifies physical close and socket quota cleanup. This is a semantic fixture,
not a network throughput benchmark.

From the W1 worktree root:

```sh
CARGO_TARGET_DIR=/tmp/w1-stream-deadline-target cargo build --locked --release \
  --target wasm32-wasip2 \
  --manifest-path runtime/wasm/engine/testdata/stream_deadline/Cargo.toml
cp /tmp/w1-stream-deadline-target/wasm32-wasip2/release/stream_deadline_fixture.wasm \
  runtime/wasm/engine/testdata/stream_deadline.wasm
wasm-tools validate runtime/wasm/engine/testdata/stream_deadline.wasm
```
