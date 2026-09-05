# Concurrent TCP echo fixture

Concurrent transport fixture, not an MQTT broker. It binds IPv4 `127.0.0.1:8099`
with canonical WASI Preview 2 sockets (`wasi:sockets@0.2.8` / `wasi:io@0.2.8`),
then `start-listen` / `finish-listen` with `would-block` polling. It serves
exactly eight lifetime connections with up to eight active at once through nonblocking `accept`,
`input-stream.read`, `output-stream.check-write` / `write` / `flush`, and
`wasi:io/poll.poll` multiplexing. Cached input and output subscriptions are
dropped before streams and the accepted socket.

Each connection holds one 64-byte pending frame with read/write offsets and
flush state. Request frames are echoed unchanged. Multiple frames per client
are allowed. EOF at a frame boundary completes that client; a partial frame
EOF is an error. One bounded progress step runs per connection per sweep so a
slow client cannot block the others. Active slots are capped at 8 and the poll
list at 9, using a stack array of borrowed handles. When a sweep makes no
progress the guest polls the relevant accept/input/output pollables instead
of spinning.

`run` returns `frames:<TOTAL>` after eight clients EOF, then drops remaining
slots, the listener subscription, the listener, and the network instance.

WIT is reused from `../mqtt/wit` via
`wit_bindgen::generate!({ path: "../mqtt/wit", world: "mqtt", generate_all })`.
Asyncify is the host runtime's embedded instrumentation; this binary is not
pre-instrumented with `wasm-opt --asyncify`.

## Rebuild

```sh
# Run from the runtime repository root.
CARGO_TARGET_DIR=/tmp/w1-concurrent-tcp-target cargo build --locked --release --target wasm32-wasip2 --manifest-path runtime/wasm/engine/testdata/concurrent_tcp/Cargo.toml
cp /tmp/w1-concurrent-tcp-target/wasm32-wasip2/release/concurrent_tcp_fixture.wasm runtime/wasm/engine/testdata/concurrent_tcp.wasm
wasm-tools validate runtime/wasm/engine/testdata/concurrent_tcp.wasm
```

The checked-in binary allows the Go integration harness to run without
installing the Rust toolchain.

## WIT provenance

Reuses `../mqtt/wit` directly without vendoring a duplicate WIT copy. Upstream
proposals are tagged WASI 0.2.8 (`wasi:sockets@0.2.8`, `wasi:io@0.2.8`,
`wasi:clocks@0.2.8`).

## Validation and benchmark

`TestConcurrentTCPGuestSlowClientDoesNotBlockPeers` stalls one client halfway
through a frame while seven peers finish, then verifies all 29 echoed frames
and zero retained socket reservations. `TestConcurrentTCPGuestPartialEOFReleasesSockets`
checks the error path releases sockets too.

`BenchmarkConcurrentTCPEcho` compares the guest with a Go echo server using the
same eight real loopback clients. Compilation and setup are excluded; guest
execution, socket dispatch, and client I/O are included. Outer process-scheduler
routing is excluded. `ns/op` measures amortized throughput cost per 64-byte frame;
`mean-rtt-ns` measures client round-trip latency. Allocations include the entire
harness and clients. Eight connection setups/teardowns are amortized per batch.
This is a transport benchmark, not an indexer or MQTT broker comparison.
