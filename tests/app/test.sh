#!/bin/bash
# SPDX-License-Identifier: MPL-2.0

set -euo pipefail

cd "$(dirname "$0")"
mkdir -p /tmp/wippy-gocache /tmp/wippy-gotmp

test_log="$(mktemp /tmp/wippy-app-tests.XXXXXX.log)"

# The application suite owns this port. Refuse to run against an unrelated
# listener: otherwise HTTP/WebSocket tests can return plausible but completely
# wrong responses from another local Wippy instance.
if timeout 1 bash -c 'exec 3<>/dev/tcp/127.0.0.1/18085' 2>/dev/null; then
	echo "test HTTP port 18085 is already in use"
	exit 1
fi

# Network overlay tests require a running docker-compose stack (socks5-proxy).
# Auto-detect the fast SOCKS5 proxy; skip the suite if not listening.
: "${SKIP_NETWORK_TESTS:=}"
if [ -z "$SKIP_NETWORK_TESTS" ]; then
	if ! timeout 1 bash -c 'exec 3<>/dev/tcp/127.0.0.1/1080' 2>/dev/null; then
		SKIP_NETWORK_TESTS=1
		echo "socks5-proxy not reachable on 127.0.0.1:1080, skipping network tests (run docker-compose up to enable)"
	fi
fi
export SKIP_NETWORK_TESTS

# SQS tests require a running ElasticMQ (or LocalStack) container reachable
# at 127.0.0.1:9324. Auto-detect and skip the suite if nothing is listening.
: "${SKIP_SQS_TESTS:=}"
if [ -z "$SKIP_SQS_TESTS" ]; then
	if ! timeout 1 bash -c 'exec 3<>/dev/tcp/127.0.0.1/9324' 2>/dev/null; then
		SKIP_SQS_TESTS=1
		echo "elasticmq not reachable on 127.0.0.1:9324, skipping sqs tests (run docker-compose up elasticmq to enable)"
	fi
fi
export SKIP_SQS_TESTS

# The exec.docker application tests use alpine directly. Make the dependency
# explicit and deterministic instead of letting three tests fail later with a
# generic process-start error.
if ! docker info >/dev/null 2>&1; then
	echo "docker is required by the exec.docker application tests"
	exit 1
fi
if ! docker image inspect alpine:latest >/dev/null 2>&1; then
	echo "alpine:latest is missing; pulling it for exec.docker tests"
	docker pull alpine:latest
fi

GOCACHE=/tmp/wippy-gocache GOTMPDIR=/tmp/wippy-gotmp OTEL_SDK_DISABLED=true SKIP_TEMPORAL_TESTS=1 SKIP_CLOUDSTORAGE_TESTS=1 \
	go run -tags treesitter ../../cmd/wippy test -c -s | tee "$test_log"

# Keep a shell-level guard so structured runner regressions cannot produce a
# false-green application suite.
clean_log="$(mktemp /tmp/wippy-app-tests-clean.XXXXXX.log)"
sed -E 's/\x1B\[[0-9;?]*[ -\/]*[@-~]//g' "$test_log" > "$clean_log"
if grep -qE "(^|[[:space:]])[1-9][0-9]* failed([[:space:]]|$)|(^|[[:space:]])FAILED([[:space:]]|$)" "$clean_log"; then
	echo "wippy app test runner reported failures (see $test_log)"
	exit 1
fi

go test ../../service/temporal/peer
