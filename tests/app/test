#!/bin/bash
# SPDX-License-Identifier: MPL-2.0

cd "$(dirname "$0")"
mkdir -p /tmp/wippy-gocache /tmp/wippy-gotmp
GOCACHE=/tmp/wippy-gocache GOTMPDIR=/tmp/wippy-gotmp OTEL_SDK_DISABLED=true SKIP_TEMPORAL_TESTS=1 SKIP_CLOUDSTORAGE_TESTS=1 GOEXPERIMENT=jsonv2 \
	go run ../../cmd/wippy run -c -s test

go test ../../service/temporal/peer
