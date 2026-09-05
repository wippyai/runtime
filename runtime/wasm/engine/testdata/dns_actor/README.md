# DNS actor fixture

DNS resolve-stream fixture. It uses canonical WASI Preview 2 sockets
(`wasi:sockets@0.2.8` / `wasi:io@0.2.8`) `ip-name-lookup` plus
`instance-network`. `resolve-addresses` never blocks in the WIT sense: a
literal or validation failure returns immediately, and a hostname returns a
`resolve-address-stream` after the host start acknowledgement. Results are
read with nonblocking `resolve-next-address`. `would-block` polls
`resolve-address-stream.subscribe` through `wasi:io/poll.poll`. Subscriptions
are dropped before the streams and the network.

`run` returns `dns:4` after four addresses are collected. `wait` starts
`cancel.example` and loops `resolve-next-address` / `poll` until the host
cancels the call. `timeout` starts `timeout.example` and loops until
`error-code::timeout`, then returns `timeout`. No clocks are imported.
Asyncify is the host runtime's embedded instrumentation; this binary is not
pre-instrumented with `wasm-opt --asyncify`. Guest I/O uses nonblocking
`resolve-next-address` plus `poll.poll`. There are no blocking stream calls.

## Handshake

The Go harness supplies a fake resolver through the socket dispatcher. Lookup
results are withheld until the guest's first `poll` on a resolve-stream
subscription. Provider names are the host-normalized IDNA / ASCII names, not
the raw guest strings. `bücher.example` is looked up as `xn--bcher-kva.example`.

1. Guest: `instance-network`.
2. Guest: `resolve-addresses(network, "::ffff:192.0.2.9")`. This is a literal;
   no provider lookup. The stream yields plain `ipv4((192, 0, 2, 9))` then
   `none`. IPv4-mapped IPv6 is not returned as IPv6.
3. Guest: `resolve-addresses(network, "a..b")`. Immediate
   `error-code::invalid-argument`. No provider lookup.
4. Guest: `resolve-addresses(network, "bücher.example")` then
   `resolve-addresses(network, "second.example")` before either result stream
   completes. The provider sees `xn--bcher-kva.example` and `second.example`.
5. Guest: `resolve-next-address` on each stream returns `would-block`. The
   harness delays results until the first poll.
6. Guest: subscribe both streams, then `poll` both subscriptions.
7. Provider, after that poll: `xn--bcher-kva.example` yields three addresses
   that the guest observes in order as `ipv4((192, 0, 2, 1))`,
   `ipv6((0x2001, 0xdb8, 0, 0, 0, 0, 0, 1))`, `ipv4((198, 51, 100, 7))`. Mapped
   IPv6 provider input is unmapped by the host. `second.example` yields
   `ipv4((203, 0, 113, 8))`.
8. Guest: collect those four addresses. A further `resolve-next-address` that
   returns `would-block` is polled again. End of stream is `none`.
9. Guest: drop subscriptions, then streams, then network, and return `dns:4`.

`wait` starts `cancel.example`, then loops `resolve-next-address` / `poll`. The
host cancels the call once poll is entered. Unexpected completion (`some` /
`none`) or any error is returned as a diagnostic string.

`timeout` starts `timeout.example`, then loops `resolve-next-address` / `poll`
until `error-code::timeout` and returns `timeout`. Any other result is an error.

## Rebuild

```sh
# Run from the runtime repository root.
CARGO_TARGET_DIR=/tmp/w1-dns-actor-target cargo build --locked --release --target wasm32-wasip2 --manifest-path runtime/wasm/engine/testdata/dns_actor/Cargo.toml
cp /tmp/w1-dns-actor-target/wasm32-wasip2/release/dns_actor_fixture.wasm runtime/wasm/engine/testdata/dns_actor.wasm
wasm-tools validate runtime/wasm/engine/testdata/dns_actor.wasm
```

The checked-in binary allows the Go integration harness to run without
installing the Rust toolchain.

## WIT provenance

Vendored WASI 0.2.8 text. `ip-name-lookup.wit` is the official
`wasi-sockets` v0.2.8 interface. `network.wit`, `instance-network.wit`,
`io/poll.wit`, and `io/error.wit` are exact copies of `../udp_actor/wit/deps`.
`wit/deps/sockets/package.wit` only supplies `package wasi:sockets@0.2.8;`.

| Package | Tag | Commit | Source files |
| --- | --- | --- | --- |
| `wasi:sockets@0.2.8` | [v0.2.8](https://github.com/WebAssembly/wasi-sockets/tree/v0.2.8) | `85f0c064f5b9ea2faa3c65b1a80b870119c0fc7f` | `wit/ip-name-lookup.wit`, `wit/network.wit`, `wit/instance-network.wit` |
| `wasi:io@0.2.8` | [v0.2.8](https://github.com/WebAssembly/wasi-io/tree/v0.2.8) | `0faf002dfc5ffdc5eeae72f427f0857e61d24cc2` | `wit/error.wit`, `wit/poll.wit` |

SHA-256 of the vendored copies:

```
a92d6a7e956904ff8212002d57aa9c1b604b96ad73daf6949c2043e2402bd053  wit/deps/sockets/ip-name-lookup.wit
09afdea373c696f25b4478f5806768cb5702433c75eae865f4dde71f1c82274f  wit/deps/sockets/network.wit
1e4a5d97df44421503a169e1d514eca2cf160f545c44d320ce8a82f726a40cf8  wit/deps/sockets/instance-network.wit
55c598b16829f7dfcd3fd373d96dd86ef0373d9354745436a61c2d3832791e11  wit/deps/io/error.wit
9e5e80f09bbea80f8f06948a2cab4d1bd348533f69f3b78d465ef5f64619b10b  wit/deps/io/poll.wit
```
