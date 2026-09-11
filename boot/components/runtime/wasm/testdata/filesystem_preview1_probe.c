/*
 * Preview1 reactor adapted with Wasmtime v36.0.2 (WASI 0.2.6).
 * Rebuild using wasi-sdk 34.0 and wasm-tools:
 * curl -fL https://github.com/bytecodealliance/wasmtime/releases/download/v36.0.2/wasi_snapshot_preview1.reactor.wasm -o /tmp/wasi-reactor.wasm
 * "$WASI_SDK_PATH/bin/clang" -O1 -mexec-model=reactor -Wl,--export=check filesystem_preview1_probe.c -o /tmp/fs-core.wasm
 * wasm-tools component embed filesystem_preview1_probe.wit /tmp/fs-core.wasm -o /tmp/fs-embedded.wasm
 * wasm-tools component new /tmp/fs-embedded.wasm --adapt wasi_snapshot_preview1=/tmp/wasi-reactor.wasm -o filesystem_preview1_probe.wasm
 */
#include <stdio.h>
#include <errno.h>
int check(void) { FILE *f=fopen("/data/input.txt", "r"); if (!f) return errno; int c=fgetc(f); fclose(f); return c==109 ? 0 : 99; }
