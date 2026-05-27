// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// ErrEmptyDirectoryPath indicates a missing directory path.
var ErrEmptyDirectoryPath = apierror.New(apierror.Invalid, "directory path is required").WithRetryable(apierror.False)

// NewInvalidModeFormatError reports invalid file mode formatting.
func NewInvalidModeFormatError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid file mode format").WithCause(cause).WithRetryable(apierror.False)
}

// NewInvalidBaseError reports an unknown relative path base.
func NewInvalidBaseError(base string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid directory base").
		WithDetails(attrs.NewBagFrom(map[string]any{
			"base":    base,
			"allowed": []string{BaseProject, BaseModule},
		})).
		WithRetryable(apierror.False)
}
