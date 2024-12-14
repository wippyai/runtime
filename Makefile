run:
	go run -race ./cmd/main.go run -c config.json

debug:
	dlv debug --build-flags -race ./cmd/main.go -- run -c config.json

test:
	go test ./pkg/... -v -race
	go test ./internal/... -v -race

debug_vm:
	dlv test -- test.v -test.run="^TestVM\$"