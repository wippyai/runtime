#!/bin/bash
# SPDX-License-Identifier: MPL-2.0

set -euo pipefail

cd "$(dirname "$0")"
mkdir -p /tmp/wippy-gocache /tmp/wippy-gotmp

test_log="$(mktemp /tmp/wippy-app-tests.XXXXXX.log)"

# Network overlay tests require a running docker-compose stack (socks5-proxy).
# Auto-detect the fast SOCKS5 proxy; skip the suite if not listening.
: "${SKIP_NETWORK_TESTS:=}"
if [ -z "$SKIP_NETWORK_TESTS" ]; then
	if ! (exec 3<>/dev/tcp/127.0.0.1/1080) 2>/dev/null; then
		SKIP_NETWORK_TESTS=1
		echo "socks5-proxy not reachable on 127.0.0.1:1080, skipping network tests (run docker-compose up to enable)"
	else
		exec 3<&-
		exec 3>&-
	fi
fi
export SKIP_NETWORK_TESTS

# SQS tests require a running ElasticMQ (or LocalStack) container reachable
# at 127.0.0.1:9324. Auto-detect and skip the suite if nothing is listening.
: "${SKIP_SQS_TESTS:=}"
if [ -z "$SKIP_SQS_TESTS" ]; then
	if ! (exec 3<>/dev/tcp/127.0.0.1/9324) 2>/dev/null; then
		SKIP_SQS_TESTS=1
		echo "elasticmq not reachable on 127.0.0.1:9324, skipping sqs tests (run docker-compose up elasticmq to enable)"
	else
		exec 3<&-
		exec 3>&-
	fi
fi
export SKIP_SQS_TESTS

GOCACHE=/tmp/wippy-gocache GOTMPDIR=/tmp/wippy-gotmp OTEL_SDK_DISABLED=true SKIP_TEMPORAL_TESTS=1 SKIP_CLOUDSTORAGE_TESTS=1 GOEXPERIMENT=jsonv2 \
	go run ../../cmd/wippy run -c -s test | tee "$test_log"

# The test runner currently prints failures but exits 0; enforce failure here.
clean_log="$(mktemp /tmp/wippy-app-tests-clean.XXXXXX.log)"
sed -E 's/\x1B\[[0-9;?]*[ -\/]*[@-~]//g' "$test_log" > "$clean_log"
if grep -qE "(^|[[:space:]])[1-9][0-9]* failed([[:space:]]|$)|(^|[[:space:]])FAILED([[:space:]]|$)" "$clean_log"; then
	echo "wippy app test runner reported failures (see $test_log)"
	exit 1
fi

go test ../../service/temporal/peer
