// SPDX-License-Identifier: MPL-2.0

package relay

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrHostNotFound      = apierror.New(apierror.NotFound, "host not found").WithRetryable(apierror.False)
	ErrHostAlreadyExists = apierror.New(apierror.AlreadyExists, "host already exists").WithRetryable(apierror.False)
	ErrEmptyNodeID       = apierror.New(apierror.Invalid, "nodeID cannot be empty").WithRetryable(apierror.False)
)
