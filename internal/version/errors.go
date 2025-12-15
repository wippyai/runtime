package version

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrInvalidVersionFormat = apierror.New(apierror.Invalid, "invalid version format").WithRetryable(apierror.False)
)

func NewParseVersionError(version string, cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to parse version: "+version).WithCause(cause).WithRetryable(apierror.False)
}

func NewVersionNotFoundError(version any) apierror.Error {
	return apierror.New(apierror.NotFound, fmt.Sprintf("version not found: %v", version)).WithRetryable(apierror.False)
}

func NewVersionAlreadyExistsError(version any) apierror.Error {
	return apierror.New(apierror.AlreadyExists, fmt.Sprintf("version already exists: %v", version)).WithRetryable(apierror.False)
}
