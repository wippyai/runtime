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

- Attested source head: `006b3b265a2133eff256cd0030defe42abd9b904`
- Authoritative profile: Linux/amd64, CGO enabled, `-short`, tags `fts5 sqlite_vec treesitter`
- `make test`: PASS
- `make lint`: PASS, 0 issues
- Inclusive tagged short suite: PASS across 355 packages
- Planned fingerprints: 165 terminal passes, zero skips, zero descendants
- Leaf identities: 13,382 (`+200` from Linux baseline 13,182)
- Passing leaves: 13,129 (`+199` from Linux baseline 12,930)
- Visible skips: 253 (`+1`)
- Newly visible skip: `service/aws/sqs.TestSQSDriver_Conformance`; the pre-existing Docker integration test was previously hidden because short-mode `TestMain` exited before unit tests ran
- False-confidence leaves removed: 15
- Duplicate terminal identities: 0
- Statement coverage: 62.9% (Linux baseline 61.4%)
- Differential changed-statement coverage: 81.5%
- Sanitized `test.sh`: PASS with Docker disabled and CDC DSNs unset
- Tagged binary build, `version --short`, and `--help`: PASS
- Windows/amd64 compatibility: all 36 changed Go packages cross-compiled with `CGO_ENABLED=0`
- Linux final JSON: `/tmp/runtime-test-expansion-linux-amd64/out/test.json`, SHA-256 `c866bc60dc05d28cec0c37ffbcb3f3a326f9e8ebb80612838c873d4b4bfc7749`
- Linux coverage profile: `/tmp/runtime-test-expansion-linux-amd64/out/coverage.out`, SHA-256 `a6e41202c881da13a68c8601c7f8ddd89b82969e96b2b7725acd633181a063ca`
- Linux platform attestation: `/tmp/runtime-test-expansion-linux-amd64/out/platform.txt`, SHA-256 `b12a35e4de9722e4a7d0194a34db49bd72a050f6063fe310132b35cc878f227e`
- Linux summary: `/tmp/runtime-test-expansion-linux-amd64/out/summary.json`, SHA-256 `34484c248f3201e4653c6ef68d5a8879c7046791e5f125236438c8d8f1adbb4d`
- Linux differential coverage: `/tmp/runtime-test-expansion-linux-amd64/out/differential-coverage.json`, SHA-256 `f465e2ecf449635e4ce1d129a1c2e31a0068b3fd16691070a35e1df0e5eee7ed`
- Linux base JSON: `/tmp/runtime-test-expansion-linux-amd64/base/test.json`, SHA-256 `17b55144ca7e8f5053ef76fe1c1d9d6516ca0649beaef71284ac597bc08eb2c4`
- Windows package list: `/tmp/runtime-test-expansion-windows-packages.txt`, SHA-256 `d41dddc1bb0c751e3ed076c2f3859c4807b259d0f322a1dbcb3199d0733291b0`
- Windows compile log: `/tmp/runtime-test-expansion-windows-compile.log`, SHA-256 `cfee181ecdc6868853ce900115ed617af7b799cd08ae811341436d4d4f1fc66a`
