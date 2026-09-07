#!/usr/bin/env bash
# Rebuilds fixtures into a caller-selected directory. It does not assert byte
# identity: Cargo/Rust/WIT toolchains can legitimately change Wasm bytes.
set -euo pipefail
out=${1:?usage: $0 /output/directory}
root=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$out"
for fixture in actor_direct_poll mixed; do
  (
    cd "$root/$fixture"
    cargo build --locked --release --target wasm32-wasip2
    cp "target/wasm32-wasip2/release/$fixture.wasm" "$out/$fixture.wasm"
  )
done
sha256sum "$out/actor_direct_poll.wasm" "$out/mixed.wasm"
