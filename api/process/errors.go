// SPDX-License-Identifier: MPL-2.0

package process

import (
	"errors"

	apierror "github.com/wippyai/runtime/api/error"
)

// Error kind constants.
const (
	LimitExceeded apierror.Kind = "LimitExceeded"
	NotFound      apierror.Kind = apierror.NotFound
	InvalidState  apierror.Kind = "InvalidState"
	Internal      apierror.Kind = apierror.Internal
)

// Errors returned by process operations.
var (
	ErrMaxProcessesExceeded = apierror.New(LimitExceeded, "max processes limit exceeded").WithRetryable(apierror.False)

	ErrProcessClosed = apierror.New(InvalidState, "process closed").WithRetryable(apierror.False)

	ErrProcessNotFound = apierror.New(NotFound, "process not found").WithRetryable(apierror.False)

	ErrProcessNotIdle = apierror.New(InvalidState, "process is not idle").WithRetryable(apierror.False)

	ErrSchedulerStopping = apierror.New(InvalidState, "scheduler is stopping").WithRetryable(apierror.False)
)

// ErrProcessReplacementRequested is an internal scheduler sentinel. A process
// may return it after writing StepDone when the current result is valid but the
// owning pool should retire this process before the next call.
var ErrProcessReplacementRequested = errors.New("process replacement requested")
