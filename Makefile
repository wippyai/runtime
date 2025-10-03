run:
	go run --tags "fts5 sqlite_vec" -race ./cmd/runner/ run -c config.json

debug:
	dlv debug --build-flags "--tags=fts5,sqlite_vec -race" ./cmd/runner/ -- run -c config.json

test-clean:
	go clean -testcache

test:
	go test ./internal/... -v -race
	go test ./api/... -v -race
	go test ./system/... -v -race
	go test ./service/... -v -race
	go test ./cluster/... -v -race
	go test --tags "fts5 sqlite_vec" ./runtime/... -v -race
	go test ./deps/... -v -race
	go test ./deps/requirementresolver/... -v -race

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

debug_vm:
	dlv test --build-flags "--tags=fts5,sqlite_vec" -- test.v -test.run="^TestVM\$"

# Build runner for all platforms where cross-compilation is possible
build-runner-all: build-runner-local build-runner-cross

# Build for the local platform (always works)
build-runner-local:
	mkdir -p ./dist
	CGO_ENABLED=1 go build --tags "fts5 sqlite_vec" -ldflags="-s -w -X main.version=$(shell git describe --tags --always --dirty)" -trimpath -o ./dist/runner-$(shell go env GOOS)-$(shell go env GOARCH) ./cmd/runner/

# Build optimized for production (maximum size reduction)
build-runner-optimized:
	mkdir -p ./dist
	CGO_ENABLED=1 go build --tags "fts5 sqlite_vec" \
		-ldflags="-s -w -X main.version=$(shell git describe --tags --always --dirty)" \
		-trimpath \
		-buildmode=pie \
		-o ./dist/runner-$(shell go env GOOS)-$(shell go env GOARCH) ./cmd/runner/
	@echo "Binary size after optimization:"
	@ls -lh ./dist/runner-$(shell go env GOOS)-$(shell go env GOARCH)

# Build with UPX compression (maximum size reduction)
# Note: UPX compression is disabled on macOS due to crash issues on macOS Ventura 13.0+
# See: https://github.com/upx/upx/issues/612
build-runner-compressed: build-runner-optimized
	@echo "Compressing binary with UPX..."
	@if [ "$(shell go env GOOS)" = "darwin" ]; then \
		echo "⚠️  UPX compression disabled on macOS due to crash issues (see https://github.com/upx/upx/issues/612)"; \
		echo "Using optimized binary without compression..."; \
	else \
		upx --best --lzma ./dist/runner-$(shell go env GOOS)-$(shell go env GOARCH); \
	fi
	@echo "Final binary size:"
	@ls -lh ./dist/runner-$(shell go env GOOS)-$(shell go env GOARCH)

# Cross-compilation targets (require appropriate toolchains)
build-runner-cross: build-runner-check
	@echo "Building cross-platform binaries where possible"
	$(MAKE) build-runner-linux-amd64
	-$(MAKE) build-runner-linux-arm64
	-$(MAKE) build-runner-darwin-amd64
	-$(MAKE) build-runner-darwin-arm64
	-$(MAKE) build-runner-windows-amd64
	@echo "⚠️  Skipping Windows ARM64 (not supported on standard runners)"
	@echo "⚠️  macOS builds work best on macOS runners (no cross-compilation needed)"

# Check if cross-compilation is happening with CGO
build-runner-check:
	@echo "Note: Cross-compilation with CGO_ENABLED=1 requires appropriate toolchains"
	@echo "For linux/arm64: apt-get install gcc-aarch64-linux-gnu libc6-dev-arm64-cross libsqlite3-dev"
	@echo "For darwin: Only works on macOS (use build-runner-local on macOS or GitHub Actions macos-latest)"
	@echo "For windows: Use native Windows builds (no cross-compilation needed)"

# Individual platform targets
build-runner-linux-amd64:
	mkdir -p ./dist
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build --tags "fts5 sqlite_vec" \
		-ldflags="-s -w -X main.version=$(shell git describe --tags --always --dirty)" \
		-trimpath \
		-buildmode=pie \
		-o ./dist/runner-linux-amd64 ./cmd/runner/

build-runner-linux-arm64:
	mkdir -p ./dist
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 CC=aarch64-linux-gnu-gcc go build --tags "fts5 sqlite_vec" \
		-ldflags="-s -w -X main.version=$(shell git describe --tags --always --dirty)" \
		-trimpath \
		-buildmode=pie \
		-o ./dist/runner-linux-arm64 ./cmd/runner/

build-runner-windows-amd64:
	mkdir -p ./dist
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 go build --tags "fts5 sqlite_vec" \
		-ldflags="-s -w -X main.version=$(shell git describe --tags --always --dirty)" \
		-trimpath \
		-buildmode=pie \
		-o "./dist/runner-windows-amd64.exe" "./cmd/runner/"

# Standard Darwin builds (works on any macOS)
build-runner-darwin-amd64:
	mkdir -p ./dist
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build --tags "fts5 sqlite_vec" \
		-ldflags="-s -w -X main.version=$(shell git describe --tags --always --dirty)" \
		-trimpath \
		-o ./dist/runner-darwin-amd64 ./cmd/runner/

build-runner-darwin-arm64:
	mkdir -p ./dist
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build --tags "fts5 sqlite_vec" \
		-ldflags="-s -w -X main.version=$(shell git describe --tags --always --dirty)" \
		-trimpath \
		-o ./dist/runner-darwin-arm64 ./cmd/runner/

# Build for M1 architecture on M1 Mac (specialized versions)
build-runner-darwin-arm64--on-M1:
	mkdir -p ./dist
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 \
	CC=clang \
	go build --tags "fts5 sqlite_vec" -o ./dist/runner-darwin-arm64 ./cmd/runner/

build-runner-darwin-amd64--on-M1:
	mkdir -p ./dist
	arch -x86_64 env \
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 \
	CC=clang \
	CGO_CFLAGS="-I/usr/local/include" \
	CGO_LDFLAGS="-L/usr/local/lib" \
	go build --tags "fts5 sqlite_vec" -o ./dist/runner-darwin-amd64 ./cmd/runner/

# Build universal binary (both architectures)
build-runner-darwin-universal--on-M1: build-runner-darwin-arm64--on-M1 build-runner-darwin-amd64--on-M1
	mkdir -p ./dist
	lipo -create -output ./dist/runner-darwin-universal ./dist/runner-darwin-arm64 ./dist/runner-darwin-amd64

# Release packaging targets
VERSION ?= $(shell git describe --tags --always --dirty)

# Create release archives for all platforms
build-release-all: build-release-linux-amd64 build-release-linux-arm64 build-release-windows-amd64 build-release-windows-arm64 build-release-darwin-amd64 build-release-darwin-arm64

# Universal archive builder function
# Usage: $(call build-archive,platform,arch,format)
define build-archive
	@if [ "$(1)" = "windows" ]; then \
		bash ./scripts/build-release-archive.sh $(1) $(2) $(3) $(VERSION); \
	else \
		./scripts/build-release-archive.sh $(1) $(2) $(3) $(VERSION); \
	fi
endef

# Linux AMD64 release archive
build-release-linux-amd64: build-runner-linux-amd64
	$(call build-archive,linux,amd64,tar.gz)

# Linux ARM64 release archive
build-release-linux-arm64: build-runner-linux-arm64
	$(call build-archive,linux,arm64,tar.gz)

# Windows AMD64 release archive
build-release-windows-amd64: build-runner-windows-amd64
	$(call build-archive,windows,amd64,zip)

# Windows ARM64 release archive (optional - may not have runner binary)
build-release-windows-arm64:
	$(call build-archive,windows,arm64,zip)

# macOS AMD64 release archive
build-release-darwin-amd64: build-runner-darwin-amd64
	$(call build-archive,darwin,amd64,tar.gz)

# macOS ARM64 release archive
build-release-darwin-arm64: build-runner-darwin-arm64
	$(call build-archive,darwin,arm64,tar.gz)

# Build runner with embedded data (example: make build-runner-embed EMBED_DIR=/path/to/your/data)
build-runner-embed:
	@echo "Building runner with embedded data"
	mkdir -p ./embed/data
	rm -rf ./embed/data/*
	cp -r $(EMBED_DIR)/* ./embed/data/ 2>/dev/null || :
	mkdir -p ./dist
	CGO_ENABLED=1 go build --tags "fts5 sqlite_vec" -o ./dist/runner-embed-$(shell go env GOOS)-$(shell go env GOARCH) ./cmd/runner/

lint-init:
	# binary will be bin/golangci-lint
	mkdir -p bin
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b bin v2.2.1
	bin/golangci-lint --version

lint:
	bin/golangci-lint run

mock:
	go tool mockgen -destination tests/mock/identityv1connect/identityv1connect.go github.com/wippyai/module-registry-proto-go/registry/identity/v1/identityv1connect OrganizationServiceClient
	go tool mockgen -destination tests/mock/modulev1connect/modulev1connect.go github.com/wippyai/module-registry-proto-go/registry/module/v1/modulev1connect ModuleServiceClient,CommitServiceClient,LabelServiceClient,DownloadServiceClient
	go tool mockgen -destination tests/mock/deps/moduleloader.go github.com/ponyruntime/pony/deps ManifestLoader


# OpenTelemetry commands
otel-up:
	cd tests && docker-compose up -d --remove-orphans

otel-down:
	cd tests && docker-compose down

