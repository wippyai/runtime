#!/usr/bin/env bash
# SPDX-License-Identifier: MPL-2.0

set -euo pipefail

binary="$(realpath "${1:?binary path required}")"
expected_version="${2:?expected version required}"

# Execute on the supported baseline even if the build runner changes later.
actual_version="$(docker run --rm --network none \
  --volume "$binary:/wippy:ro" ubuntu:22.04 /wippy version --short)"

if [ "$actual_version" != "$expected_version" ]; then
  printf 'version mismatch: expected %s, got %s\n' "$expected_version" "$actual_version" >&2
  exit 1
fi

printf 'Ubuntu 22.04 compatibility verified: %s\n' "$actual_version"
