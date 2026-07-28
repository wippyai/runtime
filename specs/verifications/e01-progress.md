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
| WS01 | `4479e753` | pass | pass | approved after fix | yes |
| WS02 | `bbabac36` | pass | pass | approved | yes |
| WS03 | `3302ad22` | pass | pass | approved | yes |
| WS04 | `2ede5376` | pass | pass | approved | yes |
| WS05 | `a789d123` | pass | pass | approved | yes |
| WS06 | `2a205985` | pass | pass | approved after fix | yes |
| WS07 | `2946623b` | pass | pass | approved | yes |
| WS08 | `1df5a511` | pass | pass | approved | yes |
| WS09 | `2a607746` | pass | pass | approved | yes |
| WS10 | `73e46eae` | pass | pass | approved | yes |
| WS11 | `aa47fa01` | pass | pass | approved | yes |
| WS12 | `77761e0a` | pass | pass | approved | yes |

## Final Verification

- Head before this documentation checkpoint: `4da25af28dc14f4501700b9e2258466d1df0f482`
- `make test`: PASS
- `make lint`: PASS, 0 issues
- Inclusive tagged short suite: PASS across 355 packages
- Planned fingerprints: 165 terminal passes, zero skips, zero descendants
- Leaf identities: 13,381 (`+200` from 13,181)
- Passing leaves: 13,128 (`+199` from 12,929)
- Visible skips: 253 (`+1`)
- Newly visible skip: `service/aws/sqs.TestSQSDriver_Conformance`; the pre-existing Docker integration test was previously hidden because short-mode `TestMain` exited before unit tests ran
- False-confidence leaves removed: 15
- Duplicate terminal identities: 0
- Statement coverage: 62.8% (baseline 61.4%)
- Differential changed-statement coverage: 80.9%
- Sanitized `test.sh`: PASS with Docker disabled and CDC DSNs unset
- Tagged binary build, `version --short`, and `--help`: PASS
- Final JSON: `/tmp/runtime-test-expansion-final.json`, SHA-256 `cef2944ccc36352f74ca0809dba3006a5039315cfbf7df40fddc9eb98a12beab`
- Coverage profile: `/tmp/runtime-test-expansion-final.cover`, SHA-256 `cb0dff8f8075867b0fa84e95c230247409482fdf4d386d62fcf74e668024097b`
- Summary: `/tmp/runtime-test-expansion-final-summary.json`, SHA-256 `29a81029b55a44fda6f3a830263f0cb770d5c58c81206cdcea9c765185f2928f`
- Differential coverage: `/tmp/runtime-test-expansion-diff-coverage.json`, SHA-256 `c8f472b05bdf995651fbac2bf80b846ccde0c433325e1f0586d6cf103f75e7f1`
