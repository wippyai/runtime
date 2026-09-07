# QuickJS stateful actor

This fixture runs JavaScript in QuickJS-ng 0.15.1 through rquickjs 0.12.2 inside a WASM
component. `src/actor.js` owns a counter for the actor's lifetime. Its native
`actor.receive()` bridge calls Wippy's `wippy:actor/process@0.1.0` interface from
inside the interpreter; embedded Asyncify suspends and restores that stack.
The fixture is not pre-instrumented with Binaryen.

Send topic `increment` to receive a `quickjs` reply with one text payload
`count:1`, then `count:2`, and so on. An unknown topic replies
`unknown:<topic>`. Topic `stop` exits normally. Each PID has independent state.

The small bridge exposes `actor.receive()`, `actor.sendText(pid, topic, text)`
and `actor.self()`. It is an example bridge, not a general JavaScript SDK.
Pure JavaScript libraries can be bundled into `actor.js`; TypeScript must first
be compiled to JavaScript. Node.js modules, browser APIs, dynamic module loading,
and a Promise event loop are not provided by this example.

The test configures 64 MiB maximum WASM linear memory, an 8 MiB QuickJS heap,
and uses the default 64 KiB owned Asyncify stack per core. The latter lives within
the linear-memory ceiling; it is separate from the interpreter's own stack.
Randomness and clocks use the production WASI hosts. No filesystem preopens
are granted. There is no preemptive scheduling in this demo.

## Run

From the runtime repository root:

```sh
go test -race ./runtime/wasm/engine -run '^TestQuickJSActor_StateAndIsolation$' -count=1 -timeout=5m
go test ./runtime/wasm/engine -run '^$' -bench '^BenchmarkQuickJSActorRoundTrip$' -benchtime=10000x -count=3 -timeout=5m
```

The test checks repeated replies, state isolation, normal exit and termination
while receive is parked. The benchmark includes JavaScript, Asyncify, scheduler
and relay routing, plus reply validation. Compilation and the first request are
excluded from `ns/op`; `cold-start-ms` reports harness creation through the first
reply separately. It is not directly comparable with `BenchmarkActorGuestRoundTrip`,
which excludes scheduler routing.

## Rebuild

Pinned build inputs: Rust 1.96.0, WASI SDK 34.0, rquickjs 0.12.2,
wit-bindgen 0.58.0 (transitive versions in Cargo.lock), wasm-tools 1.241.2,
and Wasmtime 36.0.2's `wasi_snapshot_preview1.reactor.wasm` adapter.
Adapter SHA-256: `9201d4d6b6e09ee48abad4880eadff8e607958a76fceb2923982a14633bc9d1f`.
The `wasi:io/poll@0.2.8` dependency is included for the actor WIT definition;
unused imports are removed from the resulting component.

From this directory, with `WASI_SDK` set to your WASI SDK directory and
`WASI_PREVIEW1_ADAPTER` set to the pinned adapter file:

```sh
rustup target add wasm32-wasip1
cargo build --locked --release --target wasm32-wasip1
wasm-tools component new target/wasm32-wasip1/release/quickjs_actor_guest.wasm \
  --adapt "wasi_snapshot_preview1=$WASI_PREVIEW1_ADAPTER" \
  -o ../quickjs_actor.wasm
wasm-tools validate ../quickjs_actor.wasm
```
