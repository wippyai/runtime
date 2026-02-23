#!/bin/bash
# SPDX-License-Identifier: MPL-2.0

set -euo pipefail

cd "$(dirname "$0")"
mkdir -p /tmp/wippy-gocache /tmp/wippy-gotmp

test_log="$(mktemp /tmp/wippy-app-tests.XXXXXX.log)"
GOCACHE=/tmp/wippy-gocache GOTMPDIR=/tmp/wippy-gotmp OTEL_SDK_DISABLED=true SKIP_TEMPORAL_TESTS=1 SKIP_CLOUDSTORAGE_TESTS=1 GOEXPERIMENT=jsonv2 \
	go run ../../cmd/wippy run -c -s test | tee "$test_log"

# The test runner currently prints failures but exits 0; enforce failure here.
clean_log="$(mktemp /tmp/wippy-app-tests-clean.XXXXXX.log)"
sed -E 's/\x1B\[[0-9;?]*[ -\/]*[@-~]//g' "$test_log" > "$clean_log"
if rg -q "(^|[[:space:]])[1-9][0-9]* failed([[:space:]]|$)|(^|[[:space:]])FAILED([[:space:]]|$)" "$clean_log"; then
	echo "wippy app test runner reported failures (see $test_log)"
	exit 1
fi

go test ../../service/temporal/peer
