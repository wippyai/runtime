#!/usr/bin/env bash
# SPDX-License-Identifier: MPL-2.0
#
# Decide whether a nightly release is warranted.
# Inputs (env):
#   CURRENT_SHA - commit being considered for release (required)
#   PREV_SHA    - commit of the most recent nightly, empty if none
#   FORCE       - "true" forces a release regardless of comparison
# Output:
#   prints "should_release=true|false"; also appends to $GITHUB_OUTPUT when set.
set -euo pipefail

current="${CURRENT_SHA:-}"
prev="${PREV_SHA:-}"
force="${FORCE:-false}"

if [[ -z "$current" ]]; then
  echo "CURRENT_SHA is required" >&2
  exit 1
fi

if [[ "$force" == "true" ]]; then
  result=true
elif [[ -z "$prev" ]]; then
  result=true
elif [[ "$current" != "$prev" ]]; then
  result=true
else
  result=false
fi

echo "should_release=$result"

if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
  echo "should_release=$result" >>"$GITHUB_OUTPUT"
fi
