# Review Disposition: e01 Round 3

## Result

Round 3 failed the dual-review gate (91/100 and 91/100). Both remaining must-fix findings were reproduced red, corrected, and revalidated before round 4. Gate 0 is now version 3.

## Applied Findings

1. **Failed/retrying-controller pre-stop ignored the shutdown bound** — fixed in `be17981d`. `stopFailedStartRetries` now receives the shutdown context, invokes `Controller.StopContext`, waits only within that bound, and returns a typed bound cause to `Supervisor.StopContext`. The strengthened existing retry test proves a 50 ms shutdown deadline beats a 500 ms lifecycle timeout, the exact deadline reaches the service, and retry does not resume.
2. **Y02 did not prove retained state preservation** — fixed in `be17981d`. The pre-existing controller is seeded as desired-running/failed with sentinel details; the complete public `State` is snapshotted and required unchanged after explicit-cancel and expired-deadline transactions, in addition to pointer identity and provisional-only removal.
3. **Closed allowlist after the regression strengthening** — reconciled in Gate v3. `system/supervisor/supervisor_test.go` is an explicit WS06 review-fix path. The fingerprint inventory remains exactly 165.

## Verification

- Failed-retry shutdown regression: 50 race repetitions passed.
- Y02 ownership/state regression: 100 race repetitions passed.
- Complete supervisor race suite: PASS.
- Complete canonical race suite and lint: PASS.
- Linux/amd64 CGO tagged inventory: PASS.
- Windows/amd64 36-package compile-only lane: PASS.
- Differential coverage: 82.4%.
- Final security follow-up through `be17981d`: APPROVED.

## Residuals

- `StopContext` remains one-shot through `sync.Once`; process shutdown intentionally does not retry after its deadline.
- Windows evidence remains compile-only without CGO/runtime tags.
- Live provider integrations remain outside the local short profile.
