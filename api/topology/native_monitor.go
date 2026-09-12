// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"context"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/runtime"
)

// NativeMonitor describes an exact relationship at a locally registered native
// provider. Reference/Previous have the same retry and compare/replace semantics
// as mesh monitors. These values do not grant authority outside that registration.
type NativeMonitor struct {
	Caller, Target      pid.PID
	Reference, Previous string
}

// MonitorCompletion reports a confirmed target result. Failure retains the
// provider's obligation to retry; cancellation/disconnection are not results.
type MonitorCompletion func(context.Context, *runtime.Result) error

// NativeMonitorProvider is an optional local virtual-peer capability. Runtime
// resolves it through the exact owned relay registration, never network payload
// type assertions. Methods must honor cancellation and bound retained work.
// Admission success confirms installation. Retries of an active reference are
// idempotent; release must match exactly and prevent stale reinstallation.
// After completion, a fresh reference may request another observation of a
// logical provider target. The provider decides whether that target has a new
// execution; the runtime never reuses a completed callback reference.
type NativeMonitorProvider interface {
	AdmitMonitor(context.Context, NativeMonitor, MonitorCompletion) error
	ReleaseMonitor(context.Context, NativeMonitor) error
}
