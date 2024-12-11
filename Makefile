run:
	go run -race ./cmd/main.go run -c config.json

debug:
	dlv debug --build-flags -race ./cmd/main.go -- run -c config.json

test:
	go test ./core/events/ -v -race
	go test ./core/payload/ -v -race
	go test ./internal/graph/ -v -race

	#go test ./core/ -v -race
	#go test ./components/ -v -race

debug_vm:
	dlv test -- test.v -test.run="^TestVM\$"