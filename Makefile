run:
	go run --tags "fts5 sqlite_vec" -race ./cmd/runner/main.go run -c config.json

debug:
	dlv debug --build-flags "--tags=fts5,sqlite_vec -race" ./cmd/runner/main.go -- run -c config.json

test-clean:
	go clean -testcache

test:
	go test ./internal/... -v -race
	go test ./api/... -v -race
	go test ./system/... -v -race
	go test ./service/... -v -race
	go test --tags "fts5 sqlite_vec" ./runtime/... -v -race

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

debug_vm:
	dlv test --build-flags "--tags=fts5,sqlite_vec" -- test.v -test.run="^TestVM\$"

# Build runner for all platforms where cross-compilation is possible
build-runner-all: build-runner-local build-runner-cross

# Build for the local platform (always works)
build-runner-local:
	mkdir -p ./dist
	CGO_ENABLED=1 go build --tags "fts5 sqlite_vec" -o ./dist/runner-$(shell go env GOOS)-$(shell go env GOARCH) ./cmd/runner/main.go

# Cross-compilation targets (require appropriate toolchains)
build-runner-cross: build-runner-check
	@echo "Building cross-platform binaries where possible"
	$(MAKE) build-runner-linux-amd64
	-$(MAKE) build-runner-linux-arm64
	-$(MAKE) build-runner-darwin-amd64
	-$(MAKE) build-runner-darwin-arm64
	-$(MAKE) build-runner-windows-amd64

# Check if cross-compilation is happening with CGO
build-runner-check:
	@echo "Note: Cross-compilation with CGO_ENABLED=1 requires appropriate toolchains"
	@echo "For linux/arm64: apt-get install gcc-aarch64-linux-gnu libc6-dev-arm64-cross libsqlite3-dev"
	@echo "For darwin: osxcross toolchain"
	@echo "For windows: mingw-w64"

# Individual platform targets
build-runner-linux-amd64:
	mkdir -p ./dist
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build --tags "fts5 sqlite_vec" -o ./dist/runner-linux-amd64 ./cmd/runner/main.go

build-runner-linux-arm64:
	mkdir -p ./dist
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 CC=aarch64-linux-gnu-gcc go build --tags "fts5 sqlite_vec" -o ./dist/runner-linux-arm64 ./cmd/runner/main.go

build-runner-darwin-amd64:
	mkdir -p ./dist
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 CC=o64-clang go build --tags "fts5 sqlite_vec" -o ./dist/runner-darwin-amd64 ./cmd/runner/main.go

build-runner-darwin-arm64:
	mkdir -p ./dist
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 CC=oa64-clang go build --tags "fts5 sqlite_vec" -o ./dist/runner-darwin-arm64 ./cmd/runner/main.go

build-runner-windows-amd64:
	mkdir -p ./dist
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc go build --tags "fts5 sqlite_vec" -o ./dist/runner-windows-amd64.exe ./cmd/runner/main.go

# Build runner with embedded data (example: make build-runner-embed EMBED_DIR=/path/to/your/data)
build-runner-embed:
	@echo "Building runner with embedded data"
	mkdir -p ./embed/data
	rm -rf ./embed/data/*
	cp -r $(EMBED_DIR)/* ./embed/data/ 2>/dev/null || :
	mkdir -p ./dist
	CGO_ENABLED=1 go build --tags "fts5 sqlite_vec" -o ./dist/runner-embed-$(shell go env GOOS)-$(shell go env GOARCH) ./cmd/runner/main.go
