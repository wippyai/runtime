// SPDX-License-Identifier: MPL-2.0

package sql

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrInvalidResourceType = apierror.New(apierror.Invalid, "acquired resource is not a valid database connection").WithRetryable(apierror.False)
)
