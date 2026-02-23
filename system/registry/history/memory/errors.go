// SPDX-License-Identifier: MPL-2.0

package memory

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// ErrNoHeadVersion is a sentinel error
var (
	ErrNoHeadVersion = apierror.New(apierror.NotFound, "no head version set").WithRetryable(apierror.False)
)

// NewVersionNotFoundError creates an error when a version is not found
func NewVersionNotFoundError(version string) apierror.Error {
	return apierror.New(apierror.NotFound, "version not found: "+version).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"version": version}))
}
