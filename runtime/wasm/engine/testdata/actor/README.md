# Actor component fixture

This Rust component retains a counter across mailbox waits. It replies to each
`increment` message and exits on `stop`. `probe` exercises identity and empty
nonblocking receive; `deny` verifies that a denied send can be handled by the guest. It exercises the real Canonical ABI and
Asyncify path; host-only mailbox tests cannot substitute for this fixture.

Rebuild from the repository root:

```sh
cargo build --locked --release --target wasm32-unknown-unknown --manifest-path runtime/wasm/engine/testdata/actor/Cargo.toml --target-dir /tmp/w1-actor-fixture-target
wasm-tools component new /tmp/w1-actor-fixture-target/wasm32-unknown-unknown/release/actor_fixture.wasm -o runtime/wasm/engine/testdata/actor.wasm
```

The source WIT is copied from `runtime/wasm/host/wippy/hosts/actor/actor.wit`.
Keep both copies identical when changing the interface. The checked-in binary
allows Go integration tests to run without installing the Rust toolchain.
