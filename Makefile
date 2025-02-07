run:
	go run -race ./cmd/main.go run -c config.json

debug:
	dlv debug --build-flags -race ./cmd/main.go -- run -c config.json

test-clean:
	go clean -testcache

test:
	go test ./internal/... -v -race
	go test ./api/... -v -race
	go test ./system/... -v -race
	go test ./service/... -v -race
	go test ./runtime/... -v -race

test-system:
	go test ./internal/... -v -race
	go test ./api/... -v -race
	go test ./system/... -v -race
	
test-runtime:
	go test ./internal/... -v -race
	go test ./api/... -v -race
	go test ./runtime/... -v -race

test-service:
	go test ./internal/... -v -race
	go test ./api/... -v -race
	go test ./service/... -v -race

debug_vm:
	dlv test -- test.v -test.run="^TestVM\$"

