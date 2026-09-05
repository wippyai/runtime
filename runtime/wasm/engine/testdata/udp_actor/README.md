# UDP actor fixture

UDP echo transport fixture. It binds IPv4 `127.0.0.1:8100` with canonical WASI
Preview 2 sockets (`wasi:sockets@0.2.8` / `wasi:io@0.2.8`), using split-phase
`start-bind` / `finish-bind`. `would-block` on `finish-bind` polls
`udp-socket.subscribe` through `wasi:io/poll.poll`. After bind it opens
unconnected datagram streams with `stream(none)` and caches the incoming and
outgoing subscriptions. Subscriptions are dropped before the streams and the
socket.

`run` returns `datagrams:3` after three echoes are queued and the test client
has observed them. Asyncify is the host runtime's embedded instrumentation;
this binary is not pre-instrumented with `wasm-opt --asyncify`. Guest I/O uses
nonblocking `receive` / `check-send` / `send` plus `poll.poll`. There are no
blocking stream calls.

## Idle receive

WASI `incoming-datagram-stream.receive` never blocks and never returns
`error(would-block)`. An idle socket returns `ok([])` when `max-results > 0`
and no datagram is immediately available.

Immediately after `stream(none)` and subscribe, the fixture calls
`receive(max-results=16)` once and records that result. The idle call must
return an empty list. A non-empty idle receive is an error: the test client
must not send until the guest is waiting in `poll` on the incoming
subscription. `max-results` is a datagram count, not a byte limit.

## Handshake

The Go client talks to `127.0.0.1:8100` after the guest has bound and entered
`poll`. Payloads are at most 64 bytes. Datagram boundaries and source
addresses are preserved, including a zero-length datagram.

1. Guest: `create-udp-socket(ipv4)`, `instance-network`, `start-bind` /
   `finish-bind` on `127.0.0.1:8100`.
2. Guest: `stream(none)`, subscribe incoming and outgoing, idle
   `receive(16)` (empty), then `poll` the incoming subscription.
3. Client: send three datagrams, one of them zero-length, each `<= 64` bytes.
4. Guest: `receive` until those three arrive. Echo each payload to its
   `remote-address` with `check-send` then a permitted `send` of one
   datagram. `check-send == 0` or `send == 0` polls the outgoing
   subscription and retries, including a fresh `check-send` before every
   `send`. Zero-length payloads are echoed as zero-length datagrams.
5. Client: read three echoes, matching payload and datagram boundaries,
   sourced from `127.0.0.1:8100`.
6. Client: send a fourth ACK datagram. The guest does not echo it.
7. Guest: after the ACK arrives, drop subscriptions, streams, socket, and
   network, then return `datagrams:3`.

The ACK is required because `send` may only queue. Returning before the
client has received the echoes would drop the socket and lose in-flight
datagrams. The client sends the ACK only after it has observed all three
echoes, so a received ACK means the queued sends were delivered.

## Rebuild

```sh
# Run from the runtime repository root.
CARGO_TARGET_DIR=/tmp/w1-udp-actor-target cargo build --locked --release --target wasm32-wasip2 --manifest-path runtime/wasm/engine/testdata/udp_actor/Cargo.toml
cp /tmp/w1-udp-actor-target/wasm32-wasip2/release/udp_actor_fixture.wasm runtime/wasm/engine/testdata/udp_actor.wasm
wasm-tools validate runtime/wasm/engine/testdata/udp_actor.wasm
```

The checked-in binary allows the Go integration harness to run without
installing the Rust toolchain.

## WIT provenance

Vendored WASI 0.2.8 text. `udp.wit` matches the authoritative WASI sockets
v0.2.8 source linked below. `udp-create-socket.wit` is
the same tag. `network.wit`, `instance-network.wit`, `io/poll.wit`, and
`io/error.wit` are exact copies of `../tcp/wit/deps`.
`wit/deps/sockets/package.wit` only supplies `package wasi:sockets@0.2.8;`.

| Package | Tag | Commit | Source files |
| --- | --- | --- | --- |
| `wasi:sockets@0.2.8` | [v0.2.8](https://github.com/WebAssembly/wasi-sockets/tree/v0.2.8) | `85f0c064f5b9ea2faa3c65b1a80b870119c0fc7f` | `wit/udp.wit`, `wit/udp-create-socket.wit`, `wit/network.wit`, `wit/instance-network.wit` |
| `wasi:io@0.2.8` | [v0.2.8](https://github.com/WebAssembly/wasi-io/tree/v0.2.8) | `0faf002dfc5ffdc5eeae72f427f0857e61d24cc2` | `wit/error.wit`, `wit/poll.wit` |

SHA-256 of the vendored copies:

```
af04c9286b1c5168da7d8fc4772526cb2b372f15d4c395b007c98e13cc25de5e  wit/deps/sockets/udp.wit
c52b9bf91ef4e30e7ec08ad1fc9e77ef005a0a87648c5d2e416dd6e35369f08c  wit/deps/sockets/udp-create-socket.wit
09afdea373c696f25b4478f5806768cb5702433c75eae865f4dde71f1c82274f  wit/deps/sockets/network.wit
1e4a5d97df44421503a169e1d514eca2cf160f545c44d320ce8a82f726a40cf8  wit/deps/sockets/instance-network.wit
55c598b16829f7dfcd3fd373d96dd86ef0373d9354745436a61c2d3832791e11  wit/deps/io/error.wit
9e5e80f09bbea80f8f06948a2cab4d1bd348533f69f3b78d465ef5f64619b10b  wit/deps/io/poll.wit
```
