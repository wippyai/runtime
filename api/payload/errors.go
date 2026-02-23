// SPDX-License-Identifier: MPL-2.0

package payload

import (
	apierror "github.com/wippyai/runtime/api/error"
)

// ErrEmptyFormat is a sentinel error for payload operations.
var (
	ErrEmptyFormat = apierror.New(apierror.Invalid, "payload format is empty").WithRetryable(apierror.False)
)
