# Test Design: e01 Runtime Behavioral Expansion

## Risk and Level Strategy

| Workstream | Risk | Primary level | Planned leaves |
| --- | --- | --- | ---: |
| WS01 WASI HTTP | P0 | package/unit | 14 |
| WS02 WASI sockets | P0 | package/loopback | 16 |
| WS03 WASI IO/filesystem | P1 | unit/package | 14 |
| WS04 API boundaries | P1 | unit/package | 27 |
| WS05 boot/CLI artifacts | P0 | package/filesystem | 14 |
| WS06 system lifecycle | P0 | package/concurrency | 16 |
| WS07 data services | P0 | unit/local component | 16 |
| WS08 network ownership | P0 | package/loopback | 15 |
| WS09 Temporal router | P1 | unit/package | 10 |
| WS10 health registry | P1 | unit/concurrency | 7 |
| WS11 Lua engine | P1 | unit/composition | 11 |
| WS12 WebSocket relay | P1 | package/loopback | 5 |

The exact 165 scenario fingerprints, test names, package identities, file allowlists, and validation commands are defined in [`../test-expansion/gate0.json`](../test-expansion/gate0.json).

## Fixture Architecture

- Recording interfaces with atomic counters for ownership/release assertions.
- `preview2.ResourceTable` and direct WASI host method calls.
- `fstest.MapFS`, temporary files, and platform-scoped symlink fixtures.
- `net.Pipe`, `httptest`, loopback UDP/TCP, explicit socket deadlines, and scripted SAM servers.
- File-backed SQLite in WAL mode for cross-connection transaction isolation.
- Channel barriers and context cancellation for forced lifecycle interleavings.
- Package-local Temporal environment/process/timer fakes.
- Global health state snapshot/restore only in `_test.go` under the package mutex.

No selected scenario uses arbitrary sleeps, ambient services, live infrastructure, public DNS/internet, or a new dependency.

## Semantic Fingerprint

Each executable leaf has:

1. Stable scenario ID embedded in its top-level Go test name.
2. SuiteID equal to Go import path plus execution profile.
3. One production symbol or state-machine branch.
4. Explicit precondition and action.
5. Externally observable state, error, collaboration, or cleanup oracle.
6. A killed mutation or pre-fix failure.

Selected tests do not create `t.Run` descendants. A loop inside a selected leaf is permitted only when all iterations form one boundary invariant, such as operational error-category mapping.

## Validation

Per workstream:

1. Exact focused selector with `-count=1` and a five-minute timeout.
2. Exact focused selector under race/repetition with a five-minute timeout.
3. Full affected package set under race once.
4. JSON parser proves every expected test executed exactly once as a terminal pass.
5. Diff path allowlist and `git diff --check` pass.

Portfolio:

1. `make test` with the CI skip environment and `GORACE=halt_on_error=1`.
2. Inclusive tagged short lane across `./...`.
3. Sanitized supplemental `test.sh` with Docker disabled and CDC DSNs unset.
4. Tagged binary build and `--help` end-user smoke.
5. Exact 165-test fingerprint audit, zero duplicate identities, and no new Linux-profile skips.
6. Same-profile coverage must remain at least 61.4%.
7. Fresh-context correctness, test-quality, security, and simplicity reviews.
8. Required Ubuntu and Windows CI after integration.

## Rejection Criteria

Reject tests distinguished only by names, seeds, durations, counts, payload spelling, platform labels, or equivalent input permutations. Reject parent nodes, skips, compile-only checks, synthetic reports, local copies of production logic, constant/interface assertions, broad end-to-end duplication, and any test that does not invoke the named production branch.
