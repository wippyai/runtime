# Filesystem stream fixture

A WASI 0.2.8 component using the runtime's embedded Asyncify transform. It opens
an input file, creates a stream at offset 3, drops the descriptor, and computes
the count/sum through blocking reads. It then exclusively creates an output,
drops that descriptor, and writes and flushes the result through its retained stream.

The Go harness blocks the first host read until the guest yields a stream-wait
command. It checks 4 MiB of input with at most two 64 KiB resident stream buffers,
the output content, descriptor lifetime, and released budget charges. No network
permission or service is required; the generic stream wait uses the existing I/O
dispatch protocol.

Rebuild from the runtime root:

```sh
CARGO_TARGET_DIR=/tmp/w1-filesystem-stream-target cargo build --offline --locked --release --target wasm32-wasip2 --manifest-path runtime/wasm/engine/testdata/filesystem_stream/Cargo.toml
cp /tmp/w1-filesystem-stream-target/wasm32-wasip2/release/filesystem_stream_fixture.wasm runtime/wasm/engine/testdata/filesystem_stream.wasm
wasm-tools validate runtime/wasm/engine/testdata/filesystem_stream.wasm
```

Dependencies under `wit/deps` are the official WebAssembly/wasi-filesystem,
WebAssembly/wasi-io and WebAssembly/wasi-clocks WIT files at tag v0.2.8.
