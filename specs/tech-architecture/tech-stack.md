# Runtime Project Context

## Stack

- Go module `github.com/wippyai/runtime`, tested with Go 1.26.x.
- Runtime and CLI builds use `fts5`, `sqlite_vec`, and `treesitter` tags.
- Core boundaries include Lua and WebAssembly execution, dispatcher/process APIs, lifecycle supervision, registries, cluster/Raft/memberlist networking, HTTP, Temporal, SQL/KV/queue, and AWS adapters.
- Mandatory CI runs `make test` with the race detector on Ubuntu and Windows.

## Architecture

- `api/`: public contracts, value types, adapters, contexts, queues, stores, and resource leases.
- `boot/`: composition root, dependency loading, runtime/service/system component assembly.
- `runtime/`: Lua and WASM engines, modules, host adapters, process/function managers.
- `system/`: process and supervisor lifecycle, registries, topology, resources, logs, health, filesystem, and networking.
- `service/`: concrete HTTP, network, Temporal, persistence, queue, storage, security, and integration adapters.
- `cluster/`: membership, internode transport, Raft, distributed stack assembly, and deterministic test fabrics.
- `cmd/`: end-user CLI, pack/install/run workflows, configuration, and shutdown.

## Observed Conventions

- Tests live beside production packages and use Go's standard test runner.
- Package-local fakes and channel/barrier synchronization are preferred over exported test hooks.
- External systems are gated by build tags or environment variables; ordinary short-lane tests must remain self-contained.
- Errors are generally wrapped while preserving `errors.Is`/`errors.As` contracts.
- Lifecycle and registry operations are expected to fail closed and compensate partial acquisition.
- Resource ownership is explicit through acquire/release, handles, context cancellation, and `Drop` semantics.

## Test Architecture Baseline

- 354 listed packages, 1,277 production Go files, and 912 Go test files.
- Same-profile JSON baseline: 13,181 executable leaves, 12,929 passing and 252 skipped.
- 1,211 aggregate parent nodes are emitted by Go but are not executable leaves.
- Statement coverage is 61.4%; strongest gaps are WASI HTTP/IO/sockets/filesystem, boot Lua composition, lifecycle failure boundaries, and production routers hidden behind synthetic tests.
- `make test` runs package groups with `-short -race`; some integration and `tests/`/`tools/` paths require separate activation and are not counted as new confidence unless they execute.

## Active Considerations

- New test identities use `(Go import path + execution profile, full terminal test name)`.
- Every new test must invoke production and own a distinct setup, stimulus, oracle, and killed failure/mutation.
- Skips, copied implementations, generated-code getters, constant assertions, seeds, durations, names, parent suites, and synthetic reports do not increase the accepted count.
- Linux/amd64 with CGO and runtime tags is the authoritative inventory profile; Windows remains a mandatory build/full-regression profile.
- Live AWS, Kubernetes, Temporal, brokers, databases, public DNS, and internet are excluded from the first wave.
