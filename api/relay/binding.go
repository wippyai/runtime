// SPDX-License-Identifier: MPL-2.0
package relay

import (
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/pid"
)

// LocalBinder snapshots a local destination's admission lifetime without IO.
// The returned sender accepts only that PID and incarnation; it must never
// resolve the address again or follow a replacement. Binding neither keeps a
// process alive nor proves delivery. SendContext retains the usual package
// ownership, cancellation and bounded-admission contract.
//
// A binding is safe for concurrent sends and has no workers or release obligation.
// It becomes unusable when its destination retires. This is a native composition
// capability; bindings cannot be serialized or used as remote credentials.
type LocalBinder interface {
	BindLocal(pid.PID) (ContextSender, error)
}

var (
	ErrBindingUnsupported = apierror.New(apierror.Unavailable, "receiver does not support local destination binding").WithRetryable(apierror.False)
	ErrBindingTarget      = apierror.New(apierror.Invalid, "package does not match bound destination").WithRetryable(apierror.False)
	ErrBindingRetired     = apierror.New(apierror.Unavailable, "bound destination retired").WithRetryable(apierror.False)
)
