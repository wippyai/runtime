package security

import apierror "github.com/wippyai/runtime/api/error"

// Error kind constants.
const (
	NotFound apierror.Kind = apierror.NotFound
	Invalid  apierror.Kind = apierror.Invalid
	Expired  apierror.Kind = "Expired"
	Revoked  apierror.Kind = "Revoked"
	Denied   apierror.Kind = apierror.PermissionDenied
)

// Errors returned by security operations.
var (
	ErrNoFrameContext = apierror.New(Invalid, "no frame context available").WithRetryable(apierror.False)

	ErrScopeNotFound = apierror.New(NotFound, "security scope not found in context").WithRetryable(apierror.False)

	ErrRegistryNotFound = apierror.New(NotFound, "security registry not found in context").WithRetryable(apierror.False)

	ErrPolicyNotFound = apierror.New(NotFound, "policy not found").WithRetryable(apierror.False)

	ErrGroupNotFound = apierror.New(NotFound, "policy group not found").WithRetryable(apierror.False)

	ErrTokenInvalid = apierror.New(Invalid, "invalid token format").WithRetryable(apierror.False)

	ErrTokenExpired = apierror.New(Expired, "token expired").WithRetryable(apierror.False)

	ErrTokenRevoked = apierror.New(Revoked, "token revoked").WithRetryable(apierror.False)

	ErrTokenNotFound = apierror.New(NotFound, "token not found").WithRetryable(apierror.False)

	ErrUnsupportedTokenType = apierror.New(Invalid, "unsupported token type").WithRetryable(apierror.False)

	ErrPermissionDenied = apierror.New(Denied, "permission denied").WithRetryable(apierror.False)
)
