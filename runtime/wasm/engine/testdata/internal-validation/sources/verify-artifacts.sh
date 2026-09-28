#!/usr/bin/env bash
set -euo pipefail
candidate_root=${1:?usage: $0 /path/to/w1}
fixture_dir="$candidate_root/runtime/wasm/engine/testdata/internal-validation"
expected_actor=31a2ef2c8306423068e23b59b1885f3a62e2f090c4382f5790850a3bb26e3f3c
expected_mixed=ff93415d494e7780679ab5ef44b8a6a808e6189b51612722ea6f98836fe67880
printf '%s  %s\n' "$expected_actor" "$fixture_dir/actor_direct_poll.wasm" | sha256sum -c -
printf '%s  %s\n' "$expected_mixed" "$fixture_dir/mixed.wasm" | sha256sum -c -
