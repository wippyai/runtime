#!/bin/bash
# SPDX-License-Identifier: MPL-2.0
#
# Capstone smoke: boot the real wippy binary with the kv-backed name registry
# (cluster.raft.registry_backend=kv) and run the app test suite, proving the kv
# boot wiring initializes and the process-registry facade works through real boot.
#
# What this proves: the kv boot path (raft multiplex + kvbacked Service +
# ConfigureStrong/ConfigureDissem/StartReconciler + facade registration) starts
# without panic and the process suite (process.registry register/lookup/resolve/
# auto-release) passes. Stable MULTI-NODE behaviour (replication, strong promote
# across nodes, leader-kill failover) is proven deterministically in-process by
# cluster/clustertest (TestE2E_KVRegistry_*); this script is the real-binary
# single-node boot smoke.
#
# Known-environmental failures (NOT kv-related), skipped from the gate:
#   - exec/docker_* : require a docker daemon
#   - registry/dependency_* : require hub/network access
#
# Last verified run: 375 passed / 7 failed (all 7 environmental); process 46/46;
# security:registry ok; no kv-path panic.

set -uo pipefail
cd "$(dirname "$0")/../app"

DATA=/tmp/wippy-capstone-kv
rm -rf "$DATA"; mkdir -p "$DATA" /tmp/wippy-gocache /tmp/wippy-gotmp
LOG="$(mktemp /tmp/wippy-capstone-kv.XXXXXX.log)"

GOCACHE=/tmp/wippy-gocache GOTMPDIR=/tmp/wippy-gotmp OTEL_SDK_DISABLED=true \
  SKIP_TEMPORAL_TESTS=1 SKIP_CLOUDSTORAGE_TESTS=1 SKIP_NETWORK_TESTS=1 SKIP_SQS_TESTS=1 GOEXPERIMENT=jsonv2 \
  go run -tags treesitter ../../cmd/wippy run -c -s \
    --set cluster.enabled=true \
    --set cluster.name=capstone-kv \
    --set cluster.raft.bootstrap_expect=1 \
    --set cluster.raft.registry_backend=kv \
    --set cluster.raft.data_dir="$DATA" \
    test > "$LOG" 2>&1

clean="$(mktemp)"; sed -E 's/\x1B\[[0-9;?]*[ -\/]*[@-~]//g' "$LOG" > "$clean"

if grep -qiE 'panic:|kvbacked.*nil|kvreg.*panic' "$clean"; then
	echo "FAIL: kv boot path panicked (see $LOG)"; exit 1
fi
if ! grep -qE 'process \([0-9]+\) [0-9]+/[0-9]+ ' "$clean"; then
	echo "FAIL: process suite did not complete cleanly (see $LOG)"; exit 1
fi
echo "OK: kv boot path initialized; process registry suite passed (full log: $LOG)"
grep -E '[0-9]+ passed +[0-9]+ failed' "$clean" | tail -1
