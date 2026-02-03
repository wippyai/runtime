package version

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

func NewVersionNotFoundError(version any) apierror.Error {
	return apierror.New(apierror.NotFound, fmt.Sprintf("version not found: %v", version)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"version": version}))
}

func NewVersionAlreadyExistsError(version any) apierror.Error {
	return apierror.New(apierror.AlreadyExists, fmt.Sprintf("version already exists: %v", version)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"version": version}))
}

func NewNoPathError(from, to any) apierror.Error {
	return apierror.New(apierror.NotFound, fmt.Sprintf("no path exists from %v to %v", from, to)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"from": from, "to": to}))
}
