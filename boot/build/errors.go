// SPDX-License-Identifier: MPL-2.0

package build

import (
	"errors"
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
)

func NewStageError(stageName string, cause error) apierror.Error {
	var diagnostic apierror.Error
	if errors.As(cause, &diagnostic) {
		return apierror.E(diagnostic.Kind(), fmt.Sprintf("stage '%s' failed", stageName), diagnostic.Retryable(), diagnostic.Details(), cause)
	}
	return apierror.New(apierror.Internal, fmt.Sprintf("stage '%s' failed", stageName)).WithCause(cause)
}
