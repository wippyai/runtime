# Runtime Behavioral Test Expansion

## Objective

Add a first wave of 165 deterministic, semantically distinct executable Go test leaves across 12 isolated workstreams, while removing 15 tests that assert copied or synthetic behavior.

The authoritative machine-readable plan is [`specs/test-expansion/gate0.json`](specs/test-expansion/gate0.json).

## Baseline

- Base commit: `55bc0336f205b3158d62d32ef434149a5f14275b`
- Baseline command: `go test -short -tags 'fts5 sqlite_vec treesitter' ./... -count=1 -json`
- Executable leaf identities: 13,181
- Passed: 12,929
- Skipped: 252
- Aggregate parent nodes excluded: 1,211
- Statement coverage: 61.4%
- Canonical `make test`: passed with race detection

## Scope

| Workstream | Area | Planned leaves |
| --- | --- | ---: |
| WS01 | WASI HTTP protocol and ownership | 14 |
| WS02 | WASI socket async state and adoption | 16 |
| WS03 | WASI IO/filesystem/async/random boundaries | 14 |
| WS04 | API adapters, queue generations, leases, stores, payloads | 27 |
| WS05 | Boot/CLI fail-closed loading and atomic artifacts | 14 |
| WS06 | System transaction and lifecycle integrity | 16 |
| WS07 | Local data-service failure boundaries | 16 |
| WS08 | HTTP, Temporal client, and I2P ownership | 15 |
| WS09 | Temporal workflow production router | 10 |
| WS10 | Process-wide health registry | 7 |
| WS11 | Lua boot engine configuration and composition | 11 |
| WS12 | WebSocket relay production middleware | 5 |
| **Total** | | **165** |

Planned removals: 10 Temporal struct/slice/constant tests and five WebSocket tests that call a copied parser instead of production. Expected net leaf increase: approximately 150; final `go test -json` output is authoritative.

## Semantic Contract

Every planned leaf has an exact ID, SuiteID, top-level Go test name, execution profile, production symbol, precondition, action, observable oracle, and killed mutation/pre-fix failure in `gate0.json`.

A result counts only when it is a terminal executable Go test node. Parents, package events, skips, benchmarks, fuzz seeds, compile-only activity, synthetic reports, and table rows that differ only by input spelling do not count.

## Execution

- One worker and one worktree per workstream.
- Workers may change only their exact `allowPaths` entries.
- No dependency, generated-code, workflow, Makefile, or shared-test-framework changes.
- Each worker creates one coherent Conventional Commit from the common base.
- A red regression and its minimal production correction land together.
- Ambiguous product contracts and live-infrastructure scenarios are deferred rather than weakened.

## Integration Order

1. WS10, WS09, WS12
2. WS03, WS11, WS04
3. WS01, WS02
4. WS07, WS08
5. WS05
6. WS06

After each cherry-pick, run the workstream's exact focused command. After each wave, run affected packages once under race. Run the full repository gates after all waves.

## Final Gates

1. Exact fingerprint execution audit from JSON: every planned test passes exactly once and has no children.
2. Zero duplicate `(Package, Test)` terminal identities.
3. No new skips relative to the authoritative Linux profile.
4. `make test` with race detection.
5. Inclusive tagged short lane across `./...`.
6. Focused new-test race repetition, then all affected packages under race once.
7. Sanitized `test.sh` with Docker disabled and CDC DSNs unset.
8. Tagged binary build and end-user `--help` smoke.
9. Coverage must not fall below the 61.4% same-profile baseline; coverage growth is not a substitute for behavior.
10. Fresh-context correctness, test-quality, security, and simplicity review before integration.
