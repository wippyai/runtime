// SPDX-License-Identifier: MPL-2.0

package process

import (
	"time"

	apierror "github.com/wippyai/runtime/api/error"
)

// ErrInvalidExecutionTimeout indicates an invalid (negative) execution timeout.
var ErrInvalidExecutionTimeout = apierror.New(InvalidState, "invalid execution timeout: must be non-negative").WithRetryable(apierror.False)

// ExecutionTimeoutProvider is an optional interface that a Process can implement
// to expose its total execution lifetime to schedulers.
// A positive duration specifies a maximum execution lifetime.
// A duration of 0 indicates indefinite execution.
// A negative duration is invalid and fails closed.
type ExecutionTimeoutProvider interface {
	ExecutionTimeout() time.Duration
}
