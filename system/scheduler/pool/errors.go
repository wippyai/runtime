// SPDX-License-Identifier: MPL-2.0

package pool

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var ErrPoolClosed = apierror.New(apierror.Unavailable, "pool is closed").WithRetryable(apierror.False)
