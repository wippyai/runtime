package memory

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Sentinel errors
var (
	ErrNoHeadVersion = apierror.New(apierror.NotFound, "no head version set")
)

// NewVersionNotFoundError creates an error when a version is not found
func NewVersionNotFoundError(version string) apierror.Error {
	return apierror.E(
		apierror.NotFound,
		"version not found: "+version,
		apierror.False,
		attrs.NewBagFrom(map[string]any{"version": version}),
		nil,
	)
}
