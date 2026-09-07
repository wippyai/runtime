This reactor reads `/data/input.txt` via WASI preview1 libc and checks that its first byte is `m`. It is adapted to a component using the official Wasmtime v36.0.2 reactor adapter (WASI 0.2.6). Its descriptor calls need no allocator. The regression fails before the optional canonical allocator fix.

Rebuild with wasi-sdk 34.0 and wasm-tools:

```sh
curl -fL https://github.com/bytecodealliance/wasmtime/releases/download/v36.0.2/wasi_snapshot_preview1.reactor.wasm -o /tmp/wasi-reactor.wasm
"$WASI_SDK_PATH/bin/clang" -O1 -mexec-model=reactor -Wl,--export=check filesystem_preview1_probe.c -o /tmp/fs-core.wasm
wasm-tools component embed filesystem_preview1_probe.wit /tmp/fs-core.wasm -o /tmp/fs-embedded.wasm
wasm-tools component new /tmp/fs-embedded.wasm --adapt wasi_snapshot_preview1=/tmp/wasi-reactor.wasm -o filesystem_preview1_probe.wasm
```
