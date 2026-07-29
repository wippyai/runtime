# Review Disposition: e01 Round 1

## Result

Round 1 failed the dual-review gate (85/100 and 86/100). Every finding was addressed before round 2.

## Applied Must-Fix Findings

1. **Malformed RelayHeader after downstream commit** — fixed in `e20a7ab2`. The response wrapper validates malformed relay JSON before forwarding `Write`, `WriteHeader`, or `Flush`; W04 now attempts to commit status/body and proves the observable response remains 400 without downstream content or relay side effects.
2. **Deadline-canceled commits leaked provisional controllers** — fixed in `e20a7ab2`. Cleanup now keys off `ctx.Err()` and the returned error chain, covering explicit cancellation and deadline expiry without deleting controllers retained for logical lifecycle observability. Y02 covers both cancellation causes.
3. **Canceled runtime context prevented managed shutdown** — fixed in `e20a7ab2` and `006b3b26`. Controller lifetime is detached from run-loop cancellation, `StopContext` accepts the bounded shutdown context, the boot adapter forwards it, Y03 proves canceled-Start shutdown and idempotence, and `TestCorePlugins` composes loader, supervisor, managed service, cancellation, and shutdown.
4. **WASI input skip forwarded unbounded lengths** — fixed in `e20a7ab2`. Skip and blocking skip reject values above `preview2.MaxAllocationSize` before reading; R05 kills the missing-bound mutation.
5. **Linux/amd64 authority and Windows compatibility lacked evidence** — resolved with a native Linux/amd64 CGO container run of both base and final revisions plus cross-compilation of all 36 changed packages for Windows/amd64. Evidence and hashes are recorded in `e01-progress.md`.

## Applied Should-Fix Finding

1. **Atomic publication changed pack permissions to 0600** — fixed in `e20a7ab2`. Existing output permissions are preserved; new output defaults to 0644. B06 proves failure preservation and successful publication mode.

## Deliberate Disagreements and Deferred Considerations

1. **Remove provisional controllers on every logical commit failure** — not applied. Runtime intentionally keeps controllers created by logical start/dependency failures supervised and observable for state inspection and retry. Cleanup is limited to transaction-context cancellation/deadline, where the transaction itself lost ownership. Existing supervisor failure/retry tests enforce this contract.
2. **Extension manifest double validation** — retained. Pre-init validation prevents side effects for invalid input; post-init validation rejects invalid mutations made by extension initialization. The two checks guard different trust boundaries.
3. **Direct oracle for the registration-phase wapp rollback loop** — deferred to a later fingerprint. B10/B11 intentionally cover staging atomicity for corrupt and duplicate late packs. No reviewer demonstrated a production failure in the existing unregister loop, and adding an unplanned leaf would weaken the closed Gate-0 inventory contract.
4. **Root `plan.md` and `progress.md` absent** — intentional. Audit artifacts were relocated under `specs/epics/` and `specs/verifications/` to keep project-root output clean.

## Verification After Fixes

- Canonical race suite: PASS.
- Lint: PASS, zero issues.
- Sanitized supplemental harness and binary smoke: PASS.
- Linux/amd64 CGO tagged inventory: PASS.
- Windows/amd64 changed-package compile: PASS.
- Follow-up security review: APPROVED, no finding at confidence 8/10 or higher.
