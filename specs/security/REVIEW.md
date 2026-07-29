# Release Security Review

**Verdict: APPROVED**

- Exact reviewed range: `55bc0336f205b3158d62d32ef434149a5f14275b..72ba2d307e44cb2c536b21c07c7c1afd7c04bb0e`
- Reviewed head: `72ba2d307e44cb2c536b21c07c7c1afd7c04bb0e`
- Release code head after CI portability correction: `b8be4156b77377cf6d2e8e02066dc7b7645549cd`
- Security-attested production head: `5bd49e4dfd3e22efa9c3812caac85ce93790b87e`
- Scope: 29 commits, 106 changed files, including all 35 changed non-test Go files.
- Gate: zero unresolved HIGH findings at confidence 8/10 or higher.

## Review

- **Correct — SSRF:** Guest-controlled WASI request authority and path/query are stored separately at `runtime/wasm/host/wippy/hosts/http/outgoing.go:153-203`. The changed query split only assigns `URL.Path` and `URL.RawQuery` (`:163-165`) and cannot overwrite authority. Before the request reaches the dispatcher, the complete URL is checked by `security.IsAllowed` and the private-address policy (`:316-334`); literal and resolved private addresses require a separate permission (`:29-62`). No SSRF bypass was introduced.
- **Correct — SQL injection:** The only changed production SQL execution path is `service/store/sql/sqlstore.go:173-272`. Attacker-derived entry keys and payloads remain Squirrel values and are passed separately through `existsArgs`/`args` at `:190-221,235-265`; the change makes a failed existence probe return rather than continuing. No attacker-controlled string is concatenated into a changed SQL sink.
- **Correct — XSS and command injection:** The complete added-line sink scan found no HTML/template trust primitive, browser DOM sink, `os/exec`, `exec.Command`, shell, or process-launch sink in changed production code. No concrete attacker-controlled input-to-sink path exists in these categories.
- **Correct — unsafe deserialization and auth/IDOR:** Relay JSON is decoded into the fixed `RelayCommand` struct, malformed data is removed and rejected with HTTP 400 before response commitment (`service/http/middleware/wsrelay/manager.go:64-79,155-185`), and the target PID is syntactically validated (`:187-192`). The WebSocket upgrade still applies configured origin patterns (`:240-242`) and defaults to same-origin when no patterns are configured (`:129-148`). The relay header is produced by the wrapped application handler rather than copied from the request. No changed generic/polymorphic deserializer, authorization bypass, or cross-object access path was found.
- **Correct — lifecycle authorization isolation:** Shutdown callers supply only an additional deadline/cancellation bound. Managed services remain rooted in an isolated, sealed frame with their configured security context (`system/supervisor/controller.go:65-85`). The bound propagation reads only `Deadline`, `Done`, and canonical `Err`, not caller values or custom causes (`:387-413`). Queued stop calls now also return on their own bound (`:146-169`). This does not allow caller context data to replace service authorization state.
- **Correct — path traversal/file publication:** Changed filesystem paths are local CLI or administrator configuration inputs, not remotely attacker-controlled request data. Pack output rejects input/output aliases, creates a same-directory temporary file, verifies it, and only then atomically renames it (`cmd/wippy/cmd/pack.go:663-732`). Extension component collisions are rejected before extension initialization (`boot/extensions/loader_unix.go:163-205`). No remotely attacker-controlled path reaches a newly introduced unrestricted file sink.
- **Correct — resource-bound guest inputs:** Guest-controlled WASI read/skip lengths fail closed above `MaxAllocationSize` before allocation/read (`runtime/wasm/host/wippy/hosts/filesystem/types.go:235-274`; `runtime/wasm/host/wippy/hosts/io/streams.go:35-50,68-83`). One-shot HTTP resource ownership also prevents repeated adoption/consumption (`runtime/wasm/host/wippy/hosts/http/outgoing.go:225-239,425-505`; `runtime/wasm/host/wippy/hosts/http/types.go:245-300`).
- **Correct — crypto and secrets:** No dependency or lockfile changed, no changed production crypto primitive was introduced, and the diff credential/private-key signature scan found no concrete secret. Test-only placeholder credentials were suppressed as required.
- **Correct — post-attestation release delta:** `5bd49e4dfd3e22efa9c3812caac85ce93790b87e` is an ancestor of the release head. The three later commits are `4c78ea95a18979a91962acfd68a762ac72353295` (spec/review documents and Gate JSON), `0fa36ce9d5fe59625072788e69fa422934fb83ca` (only `system/supervisor/supervisor_test.go`), and `72ba2d307e44cb2c536b21c07c7c1afd7c04bb0e` (spec/review documents). The sole Go delta is deterministic retry-test configuration, `Jitter: -1`, at `system/supervisor/supervisor_test.go:542-547`; no production source changed after attestation.
- **Correct — Gate v4:** `specs/test-expansion/gate0.json` parses with base `55bc0336f205b3158d62d32ef434149a5f14275b`, version 4, 12 workstreams, 165 unique fingerprint IDs and suite/test identities, net arithmetic `165 + 50 - 15 = 200`, 112 unique allow paths, and coverage of all 92 changed Go paths.
- **Correct — post-review CI delta:** `72ba2d30..b8be4156` changes specifications plus two unit-test-only files. `b8be4156` replaces L08's Unix path literals with `t.TempDir()`-derived platform-native absolute paths. Unit-test-only changes are a hard security-review exclusion, and no production file changed.
- **Blocker:** None. No HIGH-severity finding at confidence 8/10 or higher remains unresolved.

## Category Disposition

| Category | Disposition |
| --- | --- |
| SQL injection | No finding |
| XSS | No finding |
| SSRF | No finding; policy/private-IP checks remain before dispatch |
| Command injection | No finding |
| Authentication/authorization bypass | No finding |
| Unsafe deserialization | No finding |
| Path traversal | No finding |
| IDOR/cross-object access | No finding |
| Cryptography | No changed vulnerable primitive |
| Secrets | No concrete secret in diff |

Test-only and documentation observations, and all observations below confidence 8/10, were suppressed.

## Residual Risks

- No residual security finding at confidence 8/10 or higher.
- Live AWS, Temporal, I2P, and other external-provider integrations were not exercised in this local release review; focused local tests, race tests, vet, static data-flow review, and the prior production-head attestation provide the release evidence.
- Existing one-shot shutdown behavior is an availability characteristic, not a demonstrated security boundary bypass.

## Commands and Evidence

- `git diff --stat/--name-status 55bc0336f205b3158d62d32ef434149a5f14275b..72ba2d307e44cb2c536b21c07c7c1afd7c04bb0e` — 106 files, 8,146 insertions, 531 deletions; all production diffs inspected.
- `git diff --name-status 5bd49e4dfd3e22efa9c3812caac85ce93790b87e..72ba2d307e44cb2c536b21c07c7c1afd7c04bb0e` plus per-commit `git diff-tree` — only specs/docs and `system/supervisor/supervisor_test.go` changed.
- `git merge-base --is-ancestor 5bd49e4dfd3e22efa9c3812caac85ce93790b87e 72ba2d307e44cb2c536b21c07c7c1afd7c04bb0e` — passed.
- Added-line scans for SQL, shell/process execution, HTML/template, network/URL, deserialization, filesystem, crypto, credential, and private-key patterns — no reportable sink path or concrete secret; no dependency-file changes.
- `go test ./runtime/wasm/host/wippy/hosts/http ./runtime/wasm/host/wippy/hosts/filesystem ./runtime/wasm/host/wippy/hosts/io ./runtime/wasm/host/wippy/hosts/sockets ./service/http/client ./service/http/middleware/wsrelay ./cmd/wippy/cmd ./cmd/internal/entries ./service/aws/s3 ./service/aws/sqs ./service/store/sql ./service/temporal/client ./service/net/i2p ./system/supervisor ./system/registry/runner` — passed all 16 focused packages.
- `go vet` over those same 16 packages — passed with no diagnostics.
- `go test -race ./system/supervisor -run '^(TestController_GracefulShutdown|TestSupervisor_StopCancelsFailedAutoStartRetryTransition|TestY02FailedSupervisorCommitRemovesController|TestY03SupervisorStopIdempotent|TestY04LateControlAfterStopIsSafe|TestY05LateRegistryCallbackAfterStopIsSafe)$' -count=10 -timeout=3m` — passed in 15.377s.
- Gate-v4 JSON invariant/allowlist script — passed: version 4, 12 workstreams, 165 unique fingerprints, net 200, 112 unique allow paths, all 92 changed Go paths covered; post-attestation production non-test paths = 0.
- `git diff --check 55bc0336f205b3158d62d32ef434149a5f14275b..72ba2d307e44cb2c536b21c07c7c1afd7c04bb0e` and `git diff --check` — passed.
- `git status --porcelain=v1` and `git diff --cached --name-only` — clean; no staged or unstaged repository files.

```acceptance-report
{
  "criteriaSatisfied": [
    {
      "id": "criterion-1",
      "status": "satisfied",
      "evidence": "review-findings provide concrete file:line evidence for SSRF, SQLi, relay deserialization/auth, lifecycle authorization isolation, path handling, guest allocation bounds, crypto, and secrets; no HIGH finding at confidence >=8 remains. residual-risks records validation limits."
    }
  ],
  "changedFiles": [
    "runtime/wasm/host/wippy/hosts/http/outgoing.go",
    "service/http/middleware/wsrelay/manager.go",
    "service/store/sql/sqlstore.go",
    "cmd/wippy/cmd/pack.go",
    "boot/extensions/loader_unix.go",
    "system/supervisor/controller.go",
    "system/supervisor/supervisor.go"
  ],
  "testsAddedOrUpdated": [
    "runtime/wasm/host/wippy/hosts/http/outgoing_test.go",
    "service/http/middleware/wsrelay/manager_integration_test.go",
    "service/store/sql/sqlstore_boundary_test.go",
    "cmd/wippy/cmd/pack_atomic_test.go",
    "system/supervisor/controller_test.go",
    "system/supervisor/supervisor_test.go"
  ],
  "commandsRun": [
    {
      "command": "go test [16 focused security-relevant packages]",
      "result": "passed",
      "summary": "All focused packages passed, including HTTP/WASI, relay, CLI pack, AWS, SQL, Temporal, I2P, supervisor, and registry runner."
    },
    {
      "command": "go vet [same 16 focused packages]",
      "result": "passed",
      "summary": "No vet diagnostics."
    },
    {
      "command": "go test -race ./system/supervisor -run focused-release-security-regressions -count=10 -timeout=3m",
      "result": "passed",
      "summary": "Queued shutdown bound, retry suppression, lifecycle, and transaction regressions passed in 15.377s."
    },
    {
      "command": "python3 Gate-v4 invariant, allowlist, and post-attestation classification script",
      "result": "passed",
      "summary": "165 unique fingerprints, net 200, all 92 changed Go paths covered, and zero post-attestation production non-test files."
    },
    {
      "command": "git diff --check 55bc0336f205b3158d62d32ef434149a5f14275b..72ba2d307e44cb2c536b21c07c7c1afd7c04bb0e && git diff --check",
      "result": "passed",
      "summary": "No whitespace errors."
    },
    {
      "command": "security sink, dependency, credential, and private-key diff scans",
      "result": "passed",
      "summary": "No reportable attacker-input sink, dependency delta, or concrete secret found."
    }
  ],
  "validationOutput": [
    "Exact release range: 55bc0336f205b3158d62d32ef434149a5f14275b..72ba2d307e44cb2c536b21c07c7c1afd7c04bb0e",
    "Attested production head 5bd49e4dfd3e22efa9c3812caac85ce93790b87e is an ancestor; subsequent production non-test changes: 0",
    "Focused tests: 16 packages passed",
    "Focused supervisor race repetitions: passed",
    "Security threshold >=8/10: zero unresolved HIGH findings"
  ],
  "residualRisks": [
    "No residual security finding at confidence 8/10 or higher.",
    "Live external-provider integrations were not exercised locally; focused tests, race, vet, static tracing, and production-head attestation were used."
  ],
  "noStagedFiles": true,
  "diffSummary": "Reviewed all 106 files and all 35 changed production Go files in the exact 29-commit range; post-attestation changes are specs/docs plus deterministic test-only retry configuration.",
  "reviewFindings": [
    "no blocker: no unresolved HIGH finding at confidence >=8/10",
    "no SQLi: service/store/sql/sqlstore.go:190-265 keeps attacker-derived values parameterized",
    "no SSRF regression: runtime/wasm/host/wippy/hosts/http/outgoing.go:153-203,316-334 preserves authority separation and pre-dispatch policy checks",
    "no unsafe deserialization/auth bypass: service/http/middleware/wsrelay/manager.go:64-79,155-192,240-242 fails closed and validates origin/target",
    "no caller-context auth bypass: system/supervisor/controller.go:65-85,387-413 preserves sealed service security context"
  ],
  "manualNotes": "APPROVED. No repository files were edited; test-only/docs findings and confidence-below-8 observations were suppressed."
}
```
