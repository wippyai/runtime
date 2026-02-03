#!/bin/bash
cd "$(dirname "$0")"
OTEL_SDK_DISABLED=true GOEXPERIMENT=jsonv2 go run -race ../../cmd/wippy run -s -c --exec 'app:test_runner'
