# BUG: Core shutdown context test races service startup

## Reproduce

Post-merge Ubuntu CI failed `TestCorePlugins` at `boot/components/core/core_test.go:133`:

```text
service test:shutdown is starting
stopping supervisor
service test:shutdown is failed: context canceled
Shutdown() error = <nil>, want deadline exceeded
```

Evidence: <https://github.com/wippyai/runtime/actions/runs/30410867871/job/90446397975>

Environment: Ubuntu runner, Go 1.26.4, `go test ./boot/... -v -race -short`. The same PR-head CI had passed, confirming scheduling sensitivity. A local 500-run race repetition passed, so the CI log is the authoritative reproduction and the defect is classified as a synchronization flake rather than a deterministic platform failure.

## Isolate

The failure is confined to `TestCorePlugins`. The test waited for `coreManagedService.Start` to be entered and then used an unrelated event-bus barrier. Entering `Start` does not prove that `Controller.tryStart` has consumed its return and committed `StatusRunning`. The failed log contains the starting transition but no running transition before shutdown.

Production `Supervisor.StopContext` correctly cancels an in-flight start. In that branch the controller reports `context.Canceled` before the shutdown context expires, so `StopContext` can return nil. The test's deadline oracle is valid only after the service reaches running state and shutdown invokes the blocking `Stop` method.

## Hypotheses

1. **Confirmed candidate:** the test starts shutdown before the controller commits running state. Falsification: wait for the lifecycle supervisor's `ServiceUpdate` payload to report running before shutdown; the deadline must then be observed consistently.
2. **Rejected:** production loses the shutdown deadline after a running service is stopped. Existing focused supervisor tests and the successful PR CI prove propagation; the failed log never reached running.
3. **Rejected:** the event bus drops registration or commit events. The failed log proves registration and start dispatch occurred.

## Verify

Root cause: the fixture's `started` channel signaled method entry, while the unrelated barrier could overtake the asynchronous controller transition. Neither synchronized on `StatusRunning`.

A falsification attempt using `ServiceInfo.GetState` failed deterministically because this test creates one supervisor through `All()` and a second through the focused lifecycle loader; the application context intentionally retains the first registered service-info adapter. The lifecycle supervisor's own `ServiceUpdate` event payload is therefore the authoritative synchronization point for this focused loader.

Fix plan: subscribe to supervisor `ServiceUpdate` before registration, wait until the `test:shutdown` event payload reports `StatusRunning`, unsubscribe, then execute the existing 50 ms shutdown deadline oracle. Remove the misleading method-entry signal and unrelated barrier.

Verification completed locally:

- focused test: 500 race-detector repetitions passed;
- complete `boot/components/core` race suite passed;
- 100 complete package repetitions passed;
- canonical `make test`, `go test ./boot/... -race -short`, vet, and lint passed;
- independent review approved with zero must-fix findings.

Release gates still required:

- Ubuntu and Windows CI on the fix PR;
- post-merge CI green.
