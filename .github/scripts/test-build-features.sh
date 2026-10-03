#!/usr/bin/env bash
# SPDX-License-Identifier: MPL-2.0

set -euo pipefail

features="${1:-}"
tags="fts5 sqlite_vec sqlite_preupdate_hook $features"
packages="$(go list -deps -tags "$tags" ./cmd/wippy)"
features=" ${features//,/ } "

has_package() {
  [[ $'\n'"$packages"$'\n' == *$'\n'"$1"$'\n'* ]]
}

if has_package plugin; then
  printf 'Native Go plugin support must not be linked\n' >&2
  exit 1
fi

for feature in tailscale treesitter; do
  if [[ "$feature" == tailscale ]]; then
    package=tailscale.com/tsnet
  else
    package=github.com/tree-sitter/go-tree-sitter
  fi
  if [[ "$features" == *" $feature "* ]]; then
    if ! has_package "$package"; then
      printf 'Enabled feature %s is missing from the build\n' "$feature" >&2
      exit 1
    fi
  elif has_package "$package"; then
    printf 'Disabled feature %s is still linked\n' "$feature" >&2
    exit 1
  fi
done

printf 'Build feature contract passed: %s\n' "${1:-default}"
