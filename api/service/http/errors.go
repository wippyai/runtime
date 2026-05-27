// SPDX-License-Identifier: MPL-2.0

package http

import apierror "github.com/wippyai/runtime/api/error"

var (
	ErrEmptyAddr     = apierror.New(apierror.Invalid, "address is required").WithRetryable(apierror.False)
	ErrNilMetadata   = apierror.New(apierror.Invalid, "metadata is required").WithRetryable(apierror.False)
	ErrEmptyFuncName = apierror.New(apierror.Invalid, "function name is required").WithRetryable(apierror.False)
	ErrEmptyPath     = apierror.New(apierror.Invalid, "path is required").WithRetryable(apierror.False)
	ErrEmptyMethod   = apierror.New(apierror.Invalid, "method is required").WithRetryable(apierror.False)

	ErrTLSOffHasInputs         = apierror.New(apierror.Invalid, "tls.mode=off must not set cert/key or mTLS fields").WithRetryable(apierror.False)
	ErrTLSAutoHasCertInputs    = apierror.New(apierror.Invalid, "tls.mode=auto must not set cert/key (driver-managed)").WithRetryable(apierror.False)
	ErrTLSManualMissingCert    = apierror.New(apierror.Invalid, "tls.mode=manual requires cert+key or cert_env+key_env").WithRetryable(apierror.False)
	ErrTLSManualAmbiguousCert  = apierror.New(apierror.Invalid, "tls.mode=manual: set either cert+key or cert_env+key_env, not both").WithRetryable(apierror.False)
	ErrTLSManualPartialCert    = apierror.New(apierror.Invalid, "tls.mode=manual: cert and key must both be set").WithRetryable(apierror.False)
	ErrTLSManualPartialCertEnv = apierror.New(apierror.Invalid, "tls.mode=manual: cert_env and key_env must both be set").WithRetryable(apierror.False)
	ErrTLSMTLSRequiresManual   = apierror.New(apierror.Invalid, "mTLS (client_auth) requires tls.mode=manual").WithRetryable(apierror.False)
	ErrTLSMTLSAmbiguousCA      = apierror.New(apierror.Invalid, "set either client_ca or client_ca_env, not both").WithRetryable(apierror.False)
	ErrTLSMTLSCAWithoutAuth    = apierror.New(apierror.Invalid, "client_ca set but client_auth is empty").WithRetryable(apierror.False)
	ErrTLSMTLSMissingCA        = apierror.New(apierror.Invalid, "client_auth verification modes require client_ca or client_ca_env").WithRetryable(apierror.False)
)

// NewMissingMetadataError reports missing metadata for a specific field.
func NewMissingMetadataError(field string) apierror.Error {
	return apierror.New(apierror.Invalid, field+" metadata is required").WithRetryable(apierror.False)
}

// NewPathMustStartWithSlashError reports a path validation failure.
func NewPathMustStartWithSlashError() apierror.Error {
	return apierror.New(apierror.Invalid, "path must start with /").WithRetryable(apierror.False)
}

// NewInvalidHTTPMethodError reports an invalid HTTP method.
func NewInvalidHTTPMethodError(method string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid HTTP method: "+method).WithRetryable(apierror.False)
}

// NewInvalidTimeoutConfigError wraps invalid timeout configuration errors.
func NewInvalidTimeoutConfigError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid timeout configuration").WithCause(cause).WithRetryable(apierror.False)
}

// NewInvalidTimeoutError reports a negative timeout value.
func NewInvalidTimeoutError(name string) apierror.Error {
	return apierror.New(apierror.Invalid, name+" must be non-negative").WithRetryable(apierror.False)
}

// NewNegativeConfigError reports a negative configuration value.
func NewNegativeConfigError(name string) apierror.Error {
	return apierror.New(apierror.Invalid, name+" must be non-negative").WithRetryable(apierror.False)
}

// NewInvalidTLSModeError reports an unknown tls.mode value.
func NewInvalidTLSModeError(mode string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid tls.mode: "+mode).WithRetryable(apierror.False)
}

// NewInvalidClientAuthError reports an unknown tls.client_auth value.
func NewInvalidClientAuthError(auth string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid tls.client_auth: "+auth).WithRetryable(apierror.False)
}
