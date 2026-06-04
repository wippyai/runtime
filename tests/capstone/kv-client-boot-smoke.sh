#!/bin/bash
# SPDX-License-Identifier: MPL-2.0
#
# Capstone smoke: boot the real wippy binary as a registry NON-MEMBER
# (cluster.raft.role=client) with the kv backend and prove the new client boot
# path runs in the real binary without panic.
#
# What this proves: boot/components/system/raft.go::loadClientRegistry executes
# in a real process — it builds the forward-only kv engine (systemkv.ClientSubmitter),
# the non-member kvbacked.Service, the dissem delegate, and registers the registry
# facades — emitting the wiring log line, with no panic. A client alone has no
# forward target (PickForwardTarget returns false until a member is gossiped), so
# this is a BOOT smoke; cross-node resolve/register over the relay is proven
# deterministically in-process by cluster/clustertest (TestE2E_KVRegistry_Client*,
# real raft + real relay), and the selector/submitter/guards by unit tests.

set -uo pipefail
cd "$(dirname "$0")/../app" || exit 1

DATA=/tmp/wippy-capstone-kvclient
rm -rf "$DATA"; mkdir -p "$DATA" /tmp/wippy-gocache /tmp/wippy-gotmp
LOG="$(mktemp /tmp/wippy-capstone-kvclient.XXXXXX)"

GOCACHE=/tmp/wippy-gocache GOTMPDIR=/tmp/wippy-gotmp OTEL_SDK_DISABLED=true \
  SKIP_TEMPORAL_TESTS=1 SKIP_CLOUDSTORAGE_TESTS=1 SKIP_NETWORK_TESTS=1 SKIP_SQS_TESTS=1 GOEXPERIMENT=jsonv2 \
  go run -tags treesitter ../../cmd/wippy run -c \
    --set cluster.enabled=true \
    --set cluster.name=capstone-kvclient \
    --set cluster.raft.role=client \
    --set cluster.raft.registry_backend=kv \
    --set cluster.raft.data_dir="$DATA" \
    test > "$LOG" 2>&1 &
RUN_PID=$!

cleanup() { kill "$RUN_PID" 2>/dev/null; pkill -P "$RUN_PID" 2>/dev/null; wait "$RUN_PID" 2>/dev/null; }
trap cleanup EXIT

ok=0
for _ in $(seq 1 120); do
	if ! kill -0 "$RUN_PID" 2>/dev/null; then break; fi
	if grep -qiE 'panic:' "$LOG"; then break; fi
	if grep -qE 'raft\(client\): kv name registry wired' "$LOG"; then ok=1; break; fi
	sleep 1
done

clean="$(mktemp)"; sed -E 's/\x1B\[[0-9;?]*[ -\/]*[@-~]//g' "$LOG" > "$clean"
if grep -qiE 'panic:' "$clean"; then
	echo "FAIL: client boot path panicked (see $LOG)"
	grep -iE 'panic:' "$clean" | head -3
	exit 1
fi
if [ "$ok" != 1 ]; then
	echo "FAIL: client registry wiring line not observed (see $LOG)"
	tail -20 "$clean"
	exit 1
fi
echo "OK: role=client kv name registry wired in the real binary; no panic (full log: $LOG)"
grep -E 'raft\(client\): kv name registry wired' "$clean" | head -1
