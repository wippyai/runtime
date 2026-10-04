// SPDX-License-Identifier: MPL-2.0

package host

import apierror "github.com/wippyai/runtime/api/error"

// Eval admission errors.
var (
	ErrEvalSourceRequired    = apierror.New(apierror.Invalid, "eval source required").WithRetryable(apierror.False)
	ErrEvalSourceTooLarge    = apierror.New(apierror.Invalid, "eval source too large").WithRetryable(apierror.False)
	ErrEvalProgramNotFound   = apierror.New(apierror.NotFound, "eval program not found").WithRetryable(apierror.False)
	ErrEvalParentRequired    = apierror.New(apierror.Invalid, "eval parent pid required").WithRetryable(apierror.False)
	ErrEvalDetachedDenied    = apierror.New(apierror.PermissionDenied, "eval detached spawn denied").WithRetryable(apierror.False)
	ErrEvalPolicyUnsupported = apierror.New(apierror.Invalid, "eval policy unsupported").WithRetryable(apierror.False)
	ErrEvalPolicyMismatch    = apierror.New(apierror.Invalid, "eval policy mismatch").WithRetryable(apierror.False)
	ErrEvalBindingInvalid    = apierror.New(apierror.Invalid, "eval binding invalid").WithRetryable(apierror.False)
)
