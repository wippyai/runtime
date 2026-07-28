# Runtime Test Expansion Progress

## Gate 0

- Integration branch: `test/runtime-behavior-expansion`
- Common implementation base: `55bc0336f205b3158d62d32ef434149a5f14275b`
- Baseline `make test`: PASS
- Baseline leaf audit: PASS
- Gate-0 manifest validation: 12 workstreams, 165 unique IDs, 165 unique `(SuiteID, test)` identities, zero exact path overlaps
- Planning review: initial plan rejected; unsafe allocation, ownership, duplicate, platform, selector, and live-environment issues corrected in `specs/test-expansion/gate0.json`

## Worktrees

| ID | Branch | Worktree | State |
| --- | --- | --- | --- |
| WS01 | `test-expansion/ws01-wasi-http` | `/opt/workspace/wippy/worktrees/runtime-test-expansion-ws01` | ready |
| WS02 | `test-expansion/ws02-wasi-sockets` | `/opt/workspace/wippy/worktrees/runtime-test-expansion-ws02` | ready |
| WS03 | `test-expansion/ws03-wasi-io` | `/opt/workspace/wippy/worktrees/runtime-test-expansion-ws03` | ready |
| WS04 | `test-expansion/ws04-api-boundaries` | `/opt/workspace/wippy/worktrees/runtime-test-expansion-ws04` | ready |
| WS05 | `test-expansion/ws05-boot-cli` | `/opt/workspace/wippy/worktrees/runtime-test-expansion-ws05` | ready |
| WS06 | `test-expansion/ws06-system-lifecycle` | `/opt/workspace/wippy/worktrees/runtime-test-expansion-ws06` | ready |
| WS07 | `test-expansion/ws07-data-services` | `/opt/workspace/wippy/worktrees/runtime-test-expansion-ws07` | ready |
| WS08 | `test-expansion/ws08-network-ownership` | `/opt/workspace/wippy/worktrees/runtime-test-expansion-ws08` | ready |
| WS09 | `test-expansion/ws09-temporal-workflow` | `/opt/workspace/wippy/worktrees/runtime-test-expansion-ws09` | ready |
| WS10 | `test-expansion/ws10-health` | `/opt/workspace/wippy/worktrees/runtime-test-expansion-ws10` | ready |
| WS11 | `test-expansion/ws11-lua-engine` | `/opt/workspace/wippy/worktrees/runtime-test-expansion-ws11` | ready |
| WS12 | `test-expansion/ws12-wsrelay` | `/opt/workspace/wippy/worktrees/runtime-test-expansion-ws12` | ready |

## Integration Ledger

| ID | Commit | Focused | Repeat/race | Review | Integrated |
| --- | --- | --- | --- | --- | --- |
| WS01 | pending | pending | pending | pending | no |
| WS02 | pending | pending | pending | pending | no |
| WS03 | pending | pending | pending | pending | no |
| WS04 | pending | pending | pending | pending | no |
| WS05 | pending | pending | pending | pending | no |
| WS06 | pending | pending | pending | pending | no |
| WS07 | pending | pending | pending | pending | no |
| WS08 | pending | pending | pending | pending | no |
| WS09 | pending | pending | pending | pending | no |
| WS10 | pending | pending | pending | pending | no |
| WS11 | pending | pending | pending | pending | no |
| WS12 | pending | pending | pending | pending | no |
