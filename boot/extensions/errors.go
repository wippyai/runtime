package extensions

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
)

func newInvalidConfigError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid extension configuration").WithCause(cause).WithRetryable(apierror.False)
}

func newOpenError(path string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to open extension: %s", path)).WithCause(cause).WithRetryable(apierror.False)
}

func newSymbolError(path string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to lookup extension symbol: %s", path)).WithCause(cause).WithRetryable(apierror.False)
}

func newManifestError(path string, reason string) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("invalid extension manifest (%s): %s", path, reason)).WithRetryable(apierror.False)
}

func newInitError(name string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("extension init failed: %s", name)).WithCause(cause).WithRetryable(apierror.False)
}
