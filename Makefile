run:
	go run -race ./cmd/main.go run -c config.json

debug:
	dlv debug --build-flags -race ./cmd/main.go -- run -c config.json
debug_vm:
	dlv test -- test.v -test.run="^TestVM\$"
