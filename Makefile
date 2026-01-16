# Enable JSON v2 for all Go commands
export GOEXPERIMENT := jsonv2

test-clean:
	go clean -testcache

test:
	go test ./internal/... -v -race
	go test ./api/... -v -race
	go test ./system/... -v -race
	go test ./service/... -v -race
	go test ./cluster/... -v -race
	go test --tags "fts5 sqlite_vec" ./runtime/... -v -race
	go test ./boot/... -v -race

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

lint-init:
	mkdir -p bin
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b bin v2.6.2
	bin/golangci-lint --version

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
	-X github.com/wippyai/runtime/cmd/wippy/version.Version=$(WIPPY_VERSION) \
	-X github.com/wippyai/runtime/cmd/wippy/version.Commit=$(WIPPY_COMMIT) \
	-X github.com/wippyai/runtime/cmd/wippy/version.Date=$(WIPPY_DATE) \
	-X github.com/wippyai/runtime/cmd/wippy/version.BuiltBy=$(WIPPY_BUILDER)

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
build-wippy-all: build-wippy-linux-amd64 build-wippy-darwin-amd64 build-wippy-darwin-arm64 build-wippy-windows-amd64

.PHONY: build-wippy-linux-amd64
build-wippy-linux-amd64:
	mkdir -p ./dist
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build --tags "fts5 sqlite_vec" \
		-ldflags="$(WIPPY_LDFLAGS)" \
		-trimpath \
		-o ./dist/wippy-linux-amd64 \
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
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 go build --tags "fts5 sqlite_vec" \
		-ldflags="$(WIPPY_LDFLAGS)" \
		-trimpath \
		-o ./dist/wippy-windows-amd64.exe \
		./cmd/wippy/

.PHONY: run-wippy
run-wippy:
	go run --tags "fts5 sqlite_vec" -ldflags="$(WIPPY_LDFLAGS)" ./cmd/wippy/ $(ARGS)
