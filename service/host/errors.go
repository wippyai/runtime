// SPDX-License-Identifier: MPL-2.0

package host

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

var (
	ErrHostNotRunning     = apierror.New(apierror.Unavailable, "host is not running").WithRetryable(apierror.False)
	ErrHostShuttingDown   = apierror.New(apierror.Unavailable, "host is shutting down").WithRetryable(apierror.False)
	ErrHostAlreadyRunning = apierror.New(apierror.Conflict, "host already running").WithRetryable(apierror.False)
)

func NewDecodeConfigError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to decode host config").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}

func NewUnsupportedEntryKindError(kind registry.Kind) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported entry kind").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}
