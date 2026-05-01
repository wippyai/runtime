# SPDX-License-Identifier: MPL-2.0

# Enable JSON v2 for all Go commands
export GOEXPERIMENT := jsonv2
export GOFLAGS := -buildvcs=false

test-clean:
	go clean -testcache

test:
	go test ./internal/... -v -race -short
	go test ./api/... -v -race -short
	go test ./system/... -v -race -short
	go test ./service/... -v -race -short
	go test ./cluster/... -v -race -short
	go test --tags "fts5 sqlite_vec" ./runtime/... -v -race -short
	go test ./boot/... -v -race -short

test-system:
	go test ./internal/... -v -race
	go test ./api/... -v -race
	go test ./system/... -v -race

test-runtime:
	go test ./internal/... -v -race
	go test ./api/... -v -race
	go test --tags "fts5 sqlite_vec" ./runtime/... -v -race

test-service:
	go test ./internal/... -v -race
	go test ./api/... -v -race
	go test ./service/... -v -race

test-cluster:
	go test ./internal/... -v -race
	go test ./api/... -v -race
	go test ./cluster/... -v -race

test-integration:
	CGO_ENABLED=1 go test ./tests/... -v -race -timeout 120s

test-network:
	go test -v -race -timeout 300s ./service/net/...

.PHONY: lint
lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.8.0 run --timeout=10m --build-tags=race ./...

mock:
	go tool mockgen -destination tests/mock/identityv1connect/identityv1connect.go github.com/wippyai/module-registry-proto-go/registry/identity/v1/identityv1connect OrganizationServiceClient
	go tool mockgen -destination tests/mock/modulev1connect/modulev1connect.go github.com/wippyai/module-registry-proto-go/registry/module/v1/modulev1connect ModuleServiceClient,CommitServiceClient,LabelServiceClient,DownloadServiceClient
	go tool mockgen -destination tests/mock/deps/moduleloader.go github.com/wippyai/runtime/deps ManifestLoader

otel-up:
	cd tests && docker-compose up -d --remove-orphans

otel-down:
	cd tests && docker-compose down

# Wippy CLI build targets
WIPPY_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
WIPPY_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
WIPPY_DATE ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
WIPPY_BUILDER ?= $(shell whoami)@$(shell hostname)

WIPPY_LDFLAGS := -s -w \
	-X github.com/wippyai/runtime/api/version.Version=$(WIPPY_VERSION) \
	-X github.com/wippyai/runtime/api/version.Commit=$(WIPPY_COMMIT) \
	-X github.com/wippyai/runtime/api/version.Date=$(WIPPY_DATE) \
	-X github.com/wippyai/runtime/api/version.BuiltBy=$(WIPPY_BUILDER)

.PHONY: build-wippy
build-wippy: build-wippy-local

.PHONY: build-wippy-local
build-wippy-local:
	mkdir -p ./dist
	CGO_ENABLED=1 go build --tags "fts5 sqlite_vec" \
		-ldflags="$(WIPPY_LDFLAGS)" \
		-trimpath \
		-o ./dist/wippy-$(shell go env GOOS)-$(shell go env GOARCH) \
		./cmd/wippy/

.PHONY: build-wippy-all
build-wippy-all: build-wippy-linux-amd64 build-wippy-linux-arm64 build-wippy-darwin-amd64 build-wippy-darwin-arm64 build-wippy-windows-amd64

.PHONY: build-wippy-linux-amd64
build-wippy-linux-amd64:
	mkdir -p ./dist
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build --tags "fts5 sqlite_vec" \
		-ldflags="$(WIPPY_LDFLAGS)" \
		-trimpath \
		-o ./dist/wippy-linux-amd64 \
		./cmd/wippy/

.PHONY: build-wippy-linux-arm64
build-wippy-linux-arm64:
	mkdir -p ./dist
	CGO_LDFLAGS="" CGO_CFLAGS="" CC=aarch64-linux-gnu-gcc \
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build --tags "fts5 sqlite_vec" \
		-ldflags="$(WIPPY_LDFLAGS)" \
		-trimpath \
		-o ./dist/wippy-linux-arm64 \
		./cmd/wippy/

.PHONY: build-wippy-darwin-amd64
build-wippy-darwin-amd64:
	mkdir -p ./dist
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build --tags "fts5 sqlite_vec" \
		-ldflags="$(WIPPY_LDFLAGS)" \
		-trimpath \
		-o ./dist/wippy-darwin-amd64 \
		./cmd/wippy/

.PHONY: build-wippy-darwin-arm64
build-wippy-darwin-arm64:
	mkdir -p ./dist
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build --tags "fts5 sqlite_vec" \
		-ldflags="$(WIPPY_LDFLAGS)" \
		-trimpath \
		-o ./dist/wippy-darwin-arm64 \
		./cmd/wippy/

.PHONY: build-wippy-windows-amd64
build-wippy-windows-amd64:
	mkdir -p ./dist
	CGO_LDFLAGS="" CGO_CFLAGS="" CC=x86_64-w64-mingw32-gcc \
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 go build --tags "fts5 sqlite_vec" \
		-ldflags="$(WIPPY_LDFLAGS)" \
		-trimpath \
		-o ./dist/wippy-windows-amd64.exe \
		./cmd/wippy/

# Azure Trusted Signing configuration
AZURE_CLIENT_ID ?=
AZURE_CLIENT_SECRET ?=
AZURE_TENANT_ID ?=
AZURE_ENDPOINT ?= https://eus.codesigning.azure.net/
AZURE_ACCOUNT_NAME ?= SpiralScout
AZURE_CERT_PROFILE ?= SpiralScout
AZURE_METADATA_JSON ?= C:\Projects\gen2-electron\metadata.json
AZURE_CODE_SIGNING_DLIB ?= C:\Users\ryots\AppData\Local\Microsoft\MicrosoftTrustedSigningClientTools\Azure.CodeSigning.Dlib.dll
SIGNTOOL_PATH ?= c:\Program Files (x86)\Windows Kits\10\bin\10.0.26100.0\x64\signtool.exe

.PHONY: sign-wippy-windows
sign-wippy-windows:
	AZURE_CLIENT_ID=$(AZURE_CLIENT_ID) \
	AZURE_CLIENT_SECRET=$(AZURE_CLIENT_SECRET) \
	AZURE_TENANT_ID=$(AZURE_TENANT_ID) \
	"$(SIGNTOOL_PATH)" sign /v /fd SHA256 \
		/tr "http://timestamp.acs.microsoft.com" /td SHA256 \
		/dlib "$(AZURE_CODE_SIGNING_DLIB)" \
		/dmdf "$(AZURE_METADATA_JSON)" \
		./dist/wippy-windows-amd64.exe

.PHONY: build-sign-wippy-windows
build-sign-wippy-windows: build-wippy-windows-amd64 sign-wippy-windows

.PHONY: run-wippy
run-wippy:
	go run --tags "fts5 sqlite_vec" -ldflags="$(WIPPY_LDFLAGS)" ./cmd/wippy/ $(ARGS)
