// SPDX-License-Identifier: MPL-2.0

package build

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
)

func NewStageError(stageName string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("stage '%s' failed", stageName)).WithCause(cause)
}
