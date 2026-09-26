// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/pid"
)

var (
	ErrProcessRequired           = apierror.New(apierror.Invalid, "process is required").WithRetryable(apierror.False)
	ErrHostRequired              = apierror.New(apierror.Invalid, "host is required").WithRetryable(apierror.False)
	ErrBootGateRequiresAutoStart = apierror.New(apierror.Invalid, "boot_gate requires auto_start").WithRetryable(apierror.False)
)

// NewInvalidHostError reports a reserved or invalid host ID.
func NewInvalidHostError(hostID pid.HostID) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid host: "+hostID).WithRetryable(apierror.False)
}
