// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"errors"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/pid"
)

// DetailExistingPID is the key for the existing PID in error metadata.
const DetailExistingPID = "existing_pid"

// Sentinel errors for topology operations.
var (
	ErrNameAlreadyRegistered = apierror.New(apierror.AlreadyExists, "name already registered").WithRetryable(apierror.False)
	ErrPIDAlreadyRegistered  = apierror.New(apierror.AlreadyExists, "pid already registered").WithRetryable(apierror.False)
	ErrPIDNotFound           = apierror.New(apierror.NotFound, "pid not found").WithRetryable(apierror.False)
	ErrPIDNotRegistered      = apierror.New(apierror.NotFound, "pid not registered").WithRetryable(apierror.False)
	ErrAlreadyMonitoring     = apierror.New(apierror.AlreadyExists, "already monitoring pid").WithRetryable(apierror.False)

	// ErrNameServiceNotReady is returned by a participating LOCAL register while
	// the node's join-epoch barrier is still in progress. Until the barrier
	// installs the leader's Strong snapshot and revokes conflicting local names,
	// a LOCAL bind could shadow a Strong name owned cluster-wide. Retryable: the
	// barrier completes shortly after join/rejoin.
	ErrNameServiceNotReady = apierror.New(apierror.Unavailable, "name service not ready: join-epoch barrier in progress").WithRetryable(apierror.True)
)

// NameAlreadyRegisteredError creates an error with the existing PID in details.
func NameAlreadyRegisteredError(existingPID pid.PID) error {
	details := attrs.NewBag()
	details.Set(DetailExistingPID, existingPID)
	return apierror.SetDetails(ErrNameAlreadyRegistered, details)
}

// GetExistingPID extracts the existing PID from a name registration error.
func GetExistingPID(err error) (pid.PID, bool) {
	var apiErr apierror.Error
	if !asError(err, &apiErr) {
		return pid.PID{}, false
	}
	if apiErr.Details() == nil {
		return pid.PID{}, false
	}
	if v, ok := apiErr.Details().Get(DetailExistingPID); ok {
		if p, ok := v.(pid.PID); ok {
			return p, true
		}
	}
	return pid.PID{}, false
}

// asError extracts an apierror.Error from the error chain.
func asError(err error, target *apierror.Error) bool {
	var e apierror.Error
	if errors.As(err, &e) {
		*target = e
		return true
	}
	return false
}
