#!/bin/bash
cd "$(dirname "$0")"
OTEL_SDK_DISABLED=true GOEXPERIMENT=jsonv2 go run -race ../cmd/wippy run -p -s --exec 'app:terminal/app:test_runner'
