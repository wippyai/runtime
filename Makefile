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

# Release packaging targets
VERSION ?= $(shell git describe --tags --always --dirty)

# Create release archives for all platforms
build-release-all: build-release-linux-amd64 build-release-linux-arm64 build-release-windows-amd64 build-release-windows-arm64 build-release-darwin-amd64 build-release-darwin-arm64

# Linux AMD64 release archive
build-release-linux-amd64: build-runner-linux-amd64
	@echo "Creating Linux AMD64 release archive..."
	mkdir -p ./dist/release-temp/wippy-linux-amd64-$(VERSION)
	cp ./dist/runner-linux-amd64 ./dist/release-temp/wippy-linux-amd64-$(VERSION)/wippy
	@if [ -f "./packcli-linux-amd64" ]; then \
		cp ./packcli-linux-amd64 ./dist/release-temp/wippy-linux-amd64-$(VERSION)/packcli; \
	else \
		echo "⚠️  PackCLI binary not found for Linux AMD64"; \
	fi
	# Compress binaries with UPX
	upx --best --lzma ./dist/release-temp/wippy-linux-amd64-$(VERSION)/wippy
	@if [ -f "./dist/release-temp/wippy-linux-amd64-$(VERSION)/packcli" ]; then \
		upx --best --lzma ./dist/release-temp/wippy-linux-amd64-$(VERSION)/packcli; \
	fi
	cd ./dist/release-temp && tar -czf ../wippy-linux-amd64-$(VERSION).tar.gz wippy-linux-amd64-$(VERSION)/
	rm -rf ./dist/release-temp
	@echo "✅ Created wippy-linux-amd64-$(VERSION).tar.gz"

# Linux ARM64 release archive
build-release-linux-arm64: build-runner-linux-arm64
	@echo "Creating Linux ARM64 release archive..."
	mkdir -p ./dist/release-temp/wippy-linux-arm64-$(VERSION)
	cp ./dist/runner-linux-arm64 ./dist/release-temp/wippy-linux-arm64-$(VERSION)/wippy
	@if [ -f "./packcli-linux-arm64" ]; then \
		cp ./packcli-linux-arm64 ./dist/release-temp/wippy-linux-arm64-$(VERSION)/packcli; \
	else \
		echo "⚠️  PackCLI binary not found for Linux ARM64"; \
	fi
	# Compress binaries with UPX
	upx --best --lzma ./dist/release-temp/wippy-linux-arm64-$(VERSION)/wippy
	@if [ -f "./dist/release-temp/wippy-linux-arm64-$(VERSION)/packcli" ]; then \
		upx --best --lzma ./dist/release-temp/wippy-linux-arm64-$(VERSION)/packcli; \
	fi
	cd ./dist/release-temp && tar -czf ../wippy-linux-arm64-$(VERSION).tar.gz wippy-linux-arm64-$(VERSION)/
	rm -rf ./dist/release-temp
	@echo "✅ Created wippy-linux-arm64-$(VERSION).tar.gz"

# Windows AMD64 release archive
build-release-windows-amd64: build-runner-windows-amd64
	@echo "Creating Windows AMD64 release archive..."
	mkdir -p ./dist/release-temp/wippy-windows-amd64-$(VERSION)
	cp ./dist/runner-windows-amd64.exe ./dist/release-temp/wippy-windows-amd64-$(VERSION)/wippy.exe
	@if [ -f "./packcli-windows-amd64.exe" ]; then \
		cp ./packcli-windows-amd64.exe ./dist/release-temp/wippy-windows-amd64-$(VERSION)/packcli.exe; \
	else \
		echo "⚠️  PackCLI binary not found for Windows AMD64"; \
	fi
	# Compress binaries with UPX
	upx --best --lzma ./dist/release-temp/wippy-windows-amd64-$(VERSION)/wippy.exe
	@if [ -f "./dist/release-temp/wippy-windows-amd64-$(VERSION)/packcli.exe" ]; then \
		upx --best --lzma ./dist/release-temp/wippy-windows-amd64-$(VERSION)/packcli.exe; \
	fi
	cd ./dist/release-temp && zip -r ../wippy-windows-amd64-$(VERSION).zip wippy-windows-amd64-$(VERSION)/
	rm -rf ./dist/release-temp
	@echo "✅ Created wippy-windows-amd64-$(VERSION).zip"

# Windows ARM64 release archive
build-release-windows-arm64: build-runner-windows-arm64
	@echo "Creating Windows ARM64 release archive..."
	mkdir -p ./dist/release-temp/wippy-windows-arm64-$(VERSION)
	cp ./dist/runner-windows-arm64.exe ./dist/release-temp/wippy-windows-arm64-$(VERSION)/wippy.exe
	@if [ -f "./packcli-windows-arm64.exe" ]; then \
		cp ./packcli-windows-arm64.exe ./dist/release-temp/wippy-windows-arm64-$(VERSION)/packcli.exe; \
	else \
		echo "⚠️  PackCLI binary not found for Windows ARM64"; \
	fi
	# Compress binaries with UPX
	upx --best --lzma ./dist/release-temp/wippy-windows-arm64-$(VERSION)/wippy.exe
	@if [ -f "./dist/release-temp/wippy-windows-arm64-$(VERSION)/packcli.exe" ]; then \
		upx --best --lzma ./dist/release-temp/wippy-windows-arm64-$(VERSION)/packcli.exe; \
	fi
	cd ./dist/release-temp && zip -r ../wippy-windows-arm64-$(VERSION).zip wippy-windows-arm64-$(VERSION)/
	rm -rf ./dist/release-temp
	@echo "✅ Created wippy-windows-arm64-$(VERSION).zip"

# macOS AMD64 release archive
build-release-darwin-amd64: build-runner-darwin-amd64
	@echo "Creating macOS AMD64 release archive..."
	mkdir -p ./dist/release-temp/wippy-darwin-amd64-$(VERSION)
	cp ./dist/runner-darwin-amd64 ./dist/release-temp/wippy-darwin-amd64-$(VERSION)/wippy
	@if [ -f "./packcli-darwin-amd64" ]; then \
		cp ./packcli-darwin-amd64 ./dist/release-temp/wippy-darwin-amd64-$(VERSION)/packcli; \
	else \
		echo "⚠️  PackCLI binary not found for macOS AMD64"; \
	fi
	# Compress binaries with UPX (force macOS support)
	upx --best --lzma --force-macos ./dist/release-temp/wippy-darwin-amd64-$(VERSION)/wippy
	@if [ -f "./dist/release-temp/wippy-darwin-amd64-$(VERSION)/packcli" ]; then \
		upx --best --lzma --force-macos ./dist/release-temp/wippy-darwin-amd64-$(VERSION)/packcli; \
	fi
	cd ./dist/release-temp && tar -czf ../wippy-darwin-amd64-$(VERSION).tar.gz wippy-darwin-amd64-$(VERSION)/
	rm -rf ./dist/release-temp
	@echo "✅ Created wippy-darwin-amd64-$(VERSION).tar.gz"

# macOS ARM64 release archive
build-release-darwin-arm64: build-runner-darwin-arm64
	@echo "Creating macOS ARM64 release archive..."
	mkdir -p ./dist/release-temp/wippy-darwin-arm64-$(VERSION)
	cp ./dist/runner-darwin-arm64 ./dist/release-temp/wippy-darwin-arm64-$(VERSION)/wippy
	@if [ -f "./packcli-darwin-arm64" ]; then \
		cp ./packcli-darwin-arm64 ./dist/release-temp/wippy-darwin-arm64-$(VERSION)/packcli; \
	else \
		echo "⚠️  PackCLI binary not found for macOS ARM64"; \
	fi
	# Compress binaries with UPX (force macOS support)
	upx --best --lzma --force-macos ./dist/release-temp/wippy-darwin-arm64-$(VERSION)/wippy
	@if [ -f "./dist/release-temp/wippy-darwin-arm64-$(VERSION)/packcli" ]; then \
		upx --best --lzma --force-macos ./dist/release-temp/wippy-darwin-arm64-$(VERSION)/packcli; \
	fi
	cd ./dist/release-temp && tar -czf ../wippy-darwin-arm64-$(VERSION).tar.gz wippy-darwin-arm64-$(VERSION)/
	rm -rf ./dist/release-temp
	@echo "✅ Created wippy-darwin-arm64-$(VERSION).tar.gz"

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

