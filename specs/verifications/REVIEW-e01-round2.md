# Review Disposition: e01 Round 2

## Result

Round 2 failed the dual-review gate (86/100 and 90/100). Gate 0 was formally revised to version 2 and every must-fix finding was addressed before round 3.

## Applied Findings

1. **Shutdown context did not bound an in-flight service stop** — fixed by `9894015c` and `a2db6312`. Controllers now accept an additional stop context, preserve lifecycle/frame values, expose the earlier deadline to the service, propagate a typed bound cause, and let the sequencer stop waiting at the shutdown boundary. The composed core test proves a 50 ms shutdown context beats a 500 ms service timeout and is stable under race and Linux repetition.
2. **Y02 did not prove ownership-limited cleanup** — fixed in `9894015c`. Each cancellation variant now seeds a pre-existing controller and proves pointer identity/state survive while only the transaction-created controller disappears.
3. **Pack mode oracle used the 0644 fallback** — fixed in `9894015c`. B06 now publishes over a chmod-confirmed 0640 destination and requires 0640 afterward, killing removal of the existing-mode branch.
4. **Boot supervisor files were outside the closed allowlist** — reconciled in Gate v2. `boot/components/core/supervisor.go` and `boot/components/core/core_test.go` are explicit WS06 review-fix paths.
5. **Inventory arithmetic and skip assertion contradicted evidence** — reconciled in Gate v2. The executable delta is 165 fingerprinted leaves + 50 pre-existing SQS leaves newly exposed by `m.Run` - 15 removals = +200. The only accepted new visible skip is the pre-existing Docker `TestSQSDriver_Conformance`; no fingerprint skips.
6. **Windows evidence did not match a valid profile** — reconciled in Gate v2. Windows/amd64 is compile-only for all 36 changed packages with `CGO_ENABLED=0`, `-exec=true`, and runtime tags omitted because those tags require native CGO tree-sitter/sqlite bindings. The attestation records source head, host, target, command, target Go environment, package results, and exit status.

## Deliberate Residuals

- Supervisor replacement semantics remain deferred. Keeping logical failures supervised is required for existing retry and state-observability behavior; same-ID replacement is a separate contract.
- `StopContext` remains one-shot through `sync.Once`. Process shutdown does not retry after its bound expires; this is documented as an availability residual, not a security or correctness bypass for this gate.
- Live provider integrations remain outside the short, local Gate-0 profile.

## Verification

- Canonical race suite and lint: PASS.
- Composed shutdown bound: 100 local race repetitions and 100 Linux repetitions passed.
- Full Linux/amd64 CGO tagged inventory: PASS.
- Gate-v2 fingerprint, delta, accepted-skip, and allowlist assertions: PASS.
- Windows/amd64 changed-package compile-only lane: PASS.
- Follow-up security review through `a2db6312`: APPROVED.
