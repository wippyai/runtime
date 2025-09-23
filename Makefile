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
	CGO_ENABLED=1 go build --tags "fts5 sqlite_vec" -o ./dist/runner-$(shell go env GOOS)-$(shell go env GOARCH) ./cmd/runner/

# Cross-compilation targets (require appropriate toolchains)
build-runner-cross: build-runner-check
	@echo "Building cross-platform binaries where possible"
	$(MAKE) build-runner-linux-amd64
	-$(MAKE) build-runner-linux-arm64
	-$(MAKE) build-runner-darwin-amd64
	-$(MAKE) build-runner-darwin-arm64
	-$(MAKE) build-runner-windows-amd64
	-$(MAKE) build-runner-windows-arm64

# Check if cross-compilation is happening with CGO
build-runner-check:
	@echo "Note: Cross-compilation with CGO_ENABLED=1 requires appropriate toolchains"
	@echo "For linux/arm64: apt-get install gcc-aarch64-linux-gnu libc6-dev-arm64-cross libsqlite3-dev"
	@echo "For darwin: Only works on macOS (use build-runner-local on macOS)"
	@echo "For windows: apt-get install gcc-mingw-w64-x86-64 gcc-mingw-w64-i686"

# Individual platform targets
build-runner-linux-amd64:
	mkdir -p ./dist
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build --tags "fts5 sqlite_vec" -o ./dist/runner-linux-amd64 ./cmd/runner/

build-runner-linux-arm64:
	mkdir -p ./dist
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 CC=aarch64-linux-gnu-gcc go build --tags "fts5 sqlite_vec" -o ./dist/runner-linux-arm64 ./cmd/runner/

build-runner-windows-amd64:
	mkdir -p ./dist
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc go build --tags "fts5 sqlite_vec" -o "./dist/runner-windows-amd64.exe" "./cmd/runner/"

build-runner-windows-arm64:
	mkdir -p ./dist
	CGO_ENABLED=1 GOOS=windows GOARCH=arm64 CC=aarch64-w64-mingw32-gcc go build --tags "fts5 sqlite_vec" -o "./dist/runner-windows-arm64.exe" "./cmd/runner/"

# Standard Darwin builds (works on any macOS)
build-runner-darwin-amd64:
	mkdir -p ./dist
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 CGO_CFLAGS="-O2 -g" go build --tags "fts5 sqlite_vec" -o ./dist/runner-darwin-amd64 ./cmd/runner/

build-runner-darwin-arm64:
	mkdir -p ./dist
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 CGO_CFLAGS="-O2 -g" go build --tags "fts5 sqlite_vec" -o ./dist/runner-darwin-arm64 ./cmd/runner/

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

