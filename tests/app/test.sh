#!/bin/bash
cd "$(dirname "$0")"
mkdir -p /tmp/wippy-gocache /tmp/wippy-gotmp
GOCACHE=/tmp/wippy-gocache GOTMPDIR=/tmp/wippy-gotmp OTEL_SDK_DISABLED=true GOEXPERIMENT=jsonv2 \
	go run -race ../../cmd/wippy run -v test
