// SPDX-License-Identifier: MPL-2.0

package hash

import apierror "github.com/wippyai/runtime/api/error"

func NewEntryHashError(entryID string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to hash entry "+entryID).WithCause(cause)
}

func NewMarshalError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to marshal entries").WithCause(cause)
}
