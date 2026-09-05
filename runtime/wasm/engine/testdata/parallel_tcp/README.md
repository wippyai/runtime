# Parallel TCP connect component fixture

This Rust component opens two concurrent IPv4 TCP connections to `127.0.0.1:8099`
and `127.0.0.1:8100` using canonical WASI Preview 2 sockets (`wasi:sockets@0.2.8` /
`wasi:io@0.2.8`). Both sockets issue `start-connect` before either invokes
`finish-connect`. When `finish-connect` returns `would-block`, the fixture polls
pending sources using `tcp-socket.subscribe` and `wasi:io/poll.poll`. After both
connections complete successfully, child streams (`input-stream`, `output-stream`),
pollables, sockets, and the network instance are dropped in correct order before
returning `connected:2` from `run`. No stream I/O is performed.

The integration test harness withholds the first connection's completion until the
second connection has started to verify true split-phase execution without
deadlock. Errors are bounded with no unbounded guest allocations.

WIT is reused from `../tcp/wit` via `wit_bindgen::generate!({ path: "../tcp/wit", world: "tcp", generate_all })`
so that upstream vendored WIT definitions are not duplicated. Asyncify is the host
runtime's embedded instrumentation; this binary is not pre-instrumented with
external tools such as `wasm-opt --asyncify`.

## Rebuild

```sh
# Run from the runtime repository root.
cargo build --locked --release --target wasm32-unknown-unknown --manifest-path runtime/wasm/engine/testdata/parallel_tcp/Cargo.toml --target-dir /tmp/w1-parallel-tcp-target
wasm-tools component new /tmp/w1-parallel-tcp-target/wasm32-unknown-unknown/release/parallel_tcp_fixture.wasm -o runtime/wasm/engine/testdata/parallel_tcp.wasm
wasm-tools validate runtime/wasm/engine/testdata/parallel_tcp.wasm
```

## WIT provenance

Reuses `../tcp/wit` directly without vendoring a duplicate WIT copy. Upstream
proposals are tagged WASI 0.2.8 (`wasi:sockets@0.2.8`, `wasi:io@0.2.8`,
`wasi:clocks@0.2.8`).
