# TCP component fixture

This Rust component opens an IPv4 TCP connection to `127.0.0.1:8099` using
canonical WASI Preview 2 sockets (`wasi:sockets@0.2.8` / `wasi:io@0.2.8`). It
writes `ping`, reads four bytes, and returns that string from `run`. Child
streams are dropped before the socket. The fixture uses the real Canonical ABI and handles `finish-connect`
`would-block` through `tcp-socket.subscribe` / `pollable.block`. A successful
round trip alone does not establish coverage of that branch.

WIT is the upstream 0.2.8 text, not a host-simplified signature. The integration test
intercepts the yielded connect command and supplies a controlled socket. The checked-in binary allows Go
integration tests to run without installing the Rust toolchain.

## Rebuild

```sh
# Run from the runtime repository root.
cargo build --locked --release --target wasm32-unknown-unknown --manifest-path runtime/wasm/engine/testdata/tcp/Cargo.toml --target-dir /tmp/w1-tcp-fixture-target
wasm-tools component new /tmp/w1-tcp-fixture-target/wasm32-unknown-unknown/release/tcp_fixture.wasm -o runtime/wasm/engine/testdata/tcp.wasm
wasm-tools validate runtime/wasm/engine/testdata/tcp.wasm
```

## WIT provenance

Vendored from tagged WASI 0.2.8 proposal repos. `wit/deps/sockets/package.wit`
only supplies `package wasi:sockets@0.2.8;` because the upstream interface files
share that declaration with `world.wit`, which is omitted so UDP and
`ip-name-lookup` stay out of this fixture.

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
55c598b16829f7dfcd3fd373d96dd86ef0373d9354745436a61c2d3832791e11  wit/deps/io/error.wit
9e5e80f09bbea80f8f06948a2cab4d1bd348533f69f3b78d465ef5f64619b10b  wit/deps/io/poll.wit
f0c0932aaf39a7a318b765b985f030380988284e8c9cf592494a08aa899d9bad  wit/deps/io/streams.wit
854db80e062a173aab086569d6ced88b83eecfa6ac2239e8b53197c64eb0062a  wit/deps/clocks/monotonic-clock.wit
```
