// SPDX-License-Identifier: MPL-2.0

package bootconfig

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

func NewReadConfigFileError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to read config file").WithCause(cause).WithRetryable(apierror.False)
}

var ErrMissingVersionField = apierror.New(apierror.Invalid, "missing version field").WithRetryable(apierror.False)

func NewParseYAMLError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to parse YAML").WithCause(cause).WithRetryable(apierror.False)
}

func NewUnsupportedVersionError(version string) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported version: "+version).WithRetryable(apierror.False)
}

func NewMissingOSEnvError(name string) apierror.Error {
	return apierror.New(apierror.Invalid, "missing OS environment variable: "+name).
		WithDetails(attrs.NewBagFrom(map[string]any{"name": name})).
		WithRetryable(apierror.False)
}
