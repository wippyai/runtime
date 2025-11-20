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
build-runner-compressed: build-runner-optimized
	@echo "Compressing binary with UPX..."
	@if [ "$(shell go env GOOS)" = "darwin" ]; then \
		upx --best --lzma --force-macos ./dist/runner-$(shell go env GOOS)-$(shell go env GOARCH); \
	else \
		upx --best --lzma ./dist/runner-$(shell go env GOOS)-$(shell go env GOARCH); \
	fi
	@echo "Final compressed binary size:"
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

# Windows ARM64 build - requires special toolchain, not supported on standard Windows runners
build-runner-windows-arm64:
	@echo "⚠️  Windows ARM64 build is not supported on standard Windows runners"
	@echo "This target is kept for local development with proper ARM64 toolchain"
	mkdir -p ./dist
	CGO_ENABLED=1 GOOS=windows GOARCH=arm64 go build --tags "fts5 sqlite_vec" -o "./dist/runner-windows-arm64.exe" "./cmd/runner/"

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
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b bin v2.6.2
	bin/golangci-lint --version

lint:
	bin/golangci-lint run

mock:
	go tool mockgen -destination tests/mock/identityv1connect/identityv1connect.go github.com/wippyai/module-registry-proto-go/registry/identity/v1/identityv1connect OrganizationServiceClient
	go tool mockgen -destination tests/mock/modulev1connect/modulev1connect.go github.com/wippyai/module-registry-proto-go/registry/module/v1/modulev1connect ModuleServiceClient,CommitServiceClient,LabelServiceClient,DownloadServiceClient
	go tool mockgen -destination tests/mock/deps/moduleloader.go github.com/wippyai/runtime/deps ManifestLoader


# OpenTelemetry commands
otel-up:
	cd tests && docker-compose up -d --remove-orphans

otel-down:
	cd tests && docker-compose down

build-runner-linux-amd64-exp:
	mkdir -p ./dist
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
	GOEXPERIMENT=jsonv2,greenteagc \
	go build --tags "fts5 sqlite_vec" \
	   -ldflags="-s -w -X main.version=$(shell git describe --tags --always --dirty)" \
	   -trimpath \
	   -buildmode=pie \
	   -o ./dist/runner-linux-amd64-exp ./cmd/runner/

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
	CGO_ENABLED=0 go build \
		-ldflags="$(WIPPY_LDFLAGS)" \
		-trimpath \
		-o ./dist/wippy-$(shell go env GOOS)-$(shell go env GOARCH) \
		./cmd/wippy/

.PHONY: build-wippy-all
build-wippy-all: build-wippy-linux-amd64 build-wippy-darwin-amd64 build-wippy-darwin-arm64 build-wippy-windows-amd64

.PHONY: build-wippy-linux-amd64
build-wippy-linux-amd64:
	mkdir -p ./dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
		-ldflags="$(WIPPY_LDFLAGS)" \
		-trimpath \
		-o ./dist/wippy-linux-amd64 \
		./cmd/wippy/

.PHONY: build-wippy-linux-amd64-exp
build-wippy-linux-amd64-exp:
	mkdir -p ./dist
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
	GOEXPERIMENT=jsonv2,greenteagc \
	go build --tags "fts5 sqlite_vec" \
		-ldflags="$(WIPPY_LDFLAGS)" \
		-trimpath \
		-buildmode=pie \
		-o ./dist/wippy-linux-amd64-exp \
		./cmd/wippy/

.PHONY: build-wippy-darwin-amd64
build-wippy-darwin-amd64:
	mkdir -p ./dist
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build \
		-ldflags="$(WIPPY_LDFLAGS)" \
		-trimpath \
		-o ./dist/wippy-darwin-amd64 \
		./cmd/wippy/

.PHONY: build-wippy-darwin-arm64
build-wippy-darwin-arm64:
	mkdir -p ./dist
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build \
		-ldflags="$(WIPPY_LDFLAGS)" \
		-trimpath \
		-o ./dist/wippy-darwin-arm64 \
		./cmd/wippy/

.PHONY: build-wippy-windows-amd64
build-wippy-windows-amd64:
	mkdir -p ./dist
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build \
		-ldflags="$(WIPPY_LDFLAGS)" \
		-trimpath \
		-o ./dist/wippy-windows-amd64.exe \
		./cmd/wippy/

.PHONY: run-wippy
run-wippy:
	go run -ldflags="$(WIPPY_LDFLAGS)" ./cmd/wippy/ $(ARGS)

# Build standalone application from pack file
# Usage: make build-app PACK=myapp.wapp OUTPUT=myapp
.PHONY: build-app
build-app:
	@test -n "$(PACK)" || (echo "Error: PACK parameter required. Usage: make build-app PACK=myapp.wapp OUTPUT=myapp"; exit 1)
	@test -n "$(OUTPUT)" || (echo "Error: OUTPUT parameter required. Usage: make build-app PACK=myapp.wapp OUTPUT=myapp"; exit 1)
	@test -f "$(PACK)" || (echo "Error: Pack file $(PACK) not found"; exit 1)
	@echo "Building application from pack: $(PACK) -> $(OUTPUT)"
	@mkdir -p cmd/app
	@cp $(PACK) cmd/app/app.wapp
	CGO_ENABLED=1 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build --tags "fts5 sqlite_vec" \
		-ldflags="-s -w" \
		-trimpath \
		-o ./dist/$(OUTPUT) \
		./cmd/app/
	@rm cmd/app/app.wapp
	@echo "✓ Built: ./dist/$(OUTPUT)"

# Build app from current lock file
# Usage: make build-app-from-lock OUTPUT=myapp META_FLAGS="--meta app.name=MyApp"
.PHONY: build-app-from-lock
build-app-from-lock:
	@test -n "$(OUTPUT)" || (echo "Error: OUTPUT parameter required. Usage: make build-app-from-lock OUTPUT=myapp"; exit 1)
	@echo "Creating pack from lock file..."
	./wippy pack /tmp/$(OUTPUT).wapp $(META_FLAGS)
	@echo "Building application..."
	$(MAKE) build-app PACK=/tmp/$(OUTPUT).wapp OUTPUT=$(OUTPUT) GOOS=$(GOOS) GOARCH=$(GOARCH)
	@rm /tmp/$(OUTPUT).wapp
	@echo "✓ Complete: ./dist/$(OUTPUT)"

# Platform-specific app builds
.PHONY: build-app-linux-amd64
build-app-linux-amd64:
	$(MAKE) build-app GOOS=linux GOARCH=amd64 PACK=$(PACK) OUTPUT=$(OUTPUT)

.PHONY: build-app-darwin-amd64
build-app-darwin-amd64:
	$(MAKE) build-app GOOS=darwin GOARCH=amd64 PACK=$(PACK) OUTPUT=$(OUTPUT)

.PHONY: build-app-darwin-arm64
build-app-darwin-arm64:
	$(MAKE) build-app GOOS=darwin GOARCH=arm64 PACK=$(PACK) OUTPUT=$(OUTPUT)