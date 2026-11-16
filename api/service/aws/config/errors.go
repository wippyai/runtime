// SPDX-License-Identifier: MPL-2.0

package config

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// NewUnresolvedCredentialError reports a static credential field that holds no
// value after decode. variable is the environment variable the field declares
// through its _env directive, or empty when the field has no directive.
func NewUnresolvedCredentialError(field, variable string) error {
	details := attrs.Bag{"field": field}
	message := field + " is required: static credentials need both access_key_id and secret_access_key"
	if variable != "" {
		details["variable"] = variable
		message = field + "_env names environment variable " + variable + ", which is not set"
	}
	return apierror.New(apierror.Invalid, message).
		WithRetryable(apierror.False).
		WithDetails(details)
}
