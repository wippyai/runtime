# MQTT component fixture

Limited MQTT 3.1.1 server fixture. Not a production MQTT broker. It binds IPv4
`127.0.0.1:1883` with canonical WASI Preview 2 sockets (`wasi:sockets@0.2.8` /
`wasi:io@0.2.8`), then `start-listen` / `finish-listen` with `would-block`
polling and `accept` with `would-block` polling. Child streams are dropped
before the accepted socket. Reads assemble frames in a bounded guest buffer
from `input-stream.blocking-read`; writes use `output-stream.blocking-write-and-flush`.

The Go harness redirects the yielded listen command to an ephemeral listener.
A successful `run` result of `served:2` is not a native-broker performance claim.

## Supported subset

Exactly two sequential clients. Each client, in order:

1. CONNECT (MQTT 3.1.1 only)
2. fixture replies CONNACK success
3. PINGREQ
4. fixture replies PINGRESP
5. DISCONNECT, then the fixture drops streams and the accepted socket

CONNECT must use protocol name `MQTT`, protocol level `4`, clean session, no
will, no username, no password, remaining length at most 4096. Malformed
frames, oversize remaining length, and packets outside this subset return
`result` errors and drop resources.

Not supported: PUBLISH, SUBSCRIBE, UNSUBSCRIBE, QoS > 0, retained messages,
sessions, topics, wildcards, MQTT 5, MQTT 3.1 (`MQIsdp`), TLS, WebSocket,
authentication, concurrent clients, keep-alive timeouts.

## Wire

CONNECT (client id `w1x`, keep-alive 60):

```
10 0f 00 04 4d 51 54 54 04 02 00 3c 00 03 77 31 78
```

Empty client id is also accepted:

```
10 0c 00 04 4d 51 54 54 04 02 00 3c 00 00
```

CONNACK success (session present 0, return code 0):

```
20 02 00 00
```

PINGREQ / PINGRESP / DISCONNECT:

```
c0 00
d0 00
e0 00
```

`run` returns `served:2` after both clients complete.

## Rebuild

```sh
# Run from the runtime repository root.
cargo build --locked --release --target wasm32-unknown-unknown --manifest-path runtime/wasm/engine/testdata/mqtt/Cargo.toml --target-dir /tmp/w1-mqtt-fixture-target
wasm-tools component new /tmp/w1-mqtt-fixture-target/wasm32-unknown-unknown/release/mqtt_fixture.wasm -o runtime/wasm/engine/testdata/mqtt.wasm
wasm-tools validate runtime/wasm/engine/testdata/mqtt.wasm
```

WIT is the upstream 0.2.8 text, not a host-simplified signature. Asyncify is
the host runtime's embedded instrumentation; this binary is not pre-instrumented
with `wasm-opt --asyncify`. The checked-in binary allows the Go integration
harness to run without installing the Rust toolchain.

## WIT provenance

Copied from `../tcp/wit/deps`. That tree is vendored from tagged WASI 0.2.8
proposal repos. `wit/deps/sockets/package.wit` only supplies
`package wasi:sockets@0.2.8;` because the upstream interface files share that
declaration with `world.wit`, which is omitted so UDP and `ip-name-lookup` stay
out of this fixture.

| Package | Tag | Commit | Source files |
| --- | --- | --- | --- |
| `wasi:sockets@0.2.8` | [v0.2.8](https://github.com/WebAssembly/wasi-sockets/tree/v0.2.8) | `85f0c064f5b9ea2faa3c65b1a80b870119c0fc7f` | `wit/network.wit`, `wit/instance-network.wit`, `wit/tcp.wit`, `wit/tcp-create-socket.wit` |
| `wasi:io@0.2.8` | [v0.2.8](https://github.com/WebAssembly/wasi-io/tree/v0.2.8) | `0faf002dfc5ffdc5eeae72f427f0857e61d24cc2` | `wit/error.wit`, `wit/poll.wit`, `wit/streams.wit` |
| `wasi:clocks@0.2.8` | [v0.2.8](https://github.com/WebAssembly/wasi-clocks/tree/v0.2.8) | `be769fb9ea5e4ed48f2da5ab795e41021f425f6e` | `wit/monotonic-clock.wit` (`duration` used by `wasi:sockets/tcp`) |

SHA-256 of the vendored copies:

```
09afdea373c696f25b4478f5806768cb5702433c75eae865f4dde71f1c82274f  wit/deps/sockets/network.wit
1e4a5d97df44421503a169e1d514eca2cf160f545c44d320ce8a82f726a40cf8  wit/deps/sockets/instance-network.wit
c449a90523196b1796090b820b0e2402c5fff7a60d4ed667b6755d5a241d5c02  wit/deps/sockets/tcp.wit
96a6a1a93b859127ee60273f0a77cda8d60930f7952174fb22469293b5da6a3a  wit/deps/sockets/tcp-create-socket.wit
9e555408ab4c8430716caf961fa8be8c1cf836fd4d75d68e38839cdeb6e74459  wit/deps/sockets/package.wit
55c598b16829f7dfcd3fd373d96dd86ef0373d9354745436a61c2d3832791e11  wit/deps/io/error.wit
9e5e80f09bbea80f8f06948a2cab4d1bd348533f69f3b78d465ef5f64619b10b  wit/deps/io/poll.wit
f0c0932aaf39a7a318b765b985f030380988284e8c9cf592494a08aa899d9bad  wit/deps/io/streams.wit
854db80e062a173aab086569d6ced88b83eecfa6ac2239e8b53197c64eb0062a  wit/deps/clocks/monotonic-clock.wit
```
