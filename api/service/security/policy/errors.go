// SPDX-License-Identifier: MPL-2.0

package policy

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrActionsStringEmpty   = apierror.New(apierror.Invalid, "actions string cannot be empty").WithRetryable(apierror.False)
	ErrActionsListEmpty     = apierror.New(apierror.Invalid, "actions list cannot be empty").WithRetryable(apierror.False)
	ErrActionsInvalidType   = apierror.New(apierror.Invalid, "actions must be a string or list").WithRetryable(apierror.False)
	ErrResourcesStringEmpty = apierror.New(apierror.Invalid, "resources string cannot be empty").WithRetryable(apierror.False)
	ErrResourcesListEmpty   = apierror.New(apierror.Invalid, "resources list cannot be empty").WithRetryable(apierror.False)
	ErrResourcesInvalidType = apierror.New(apierror.Invalid, "resources must be a string or list").WithRetryable(apierror.False)
	ErrExpressionEmpty      = apierror.New(apierror.Invalid, "expression cannot be empty").WithRetryable(apierror.False)
)

// NewInvalidPolicyEffectError reports an invalid policy effect value.
func NewInvalidPolicyEffectError(effect Effect) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("invalid policy effect: %s", effect)).WithRetryable(apierror.False)
}

// NewConditionFieldEmptyError reports a missing condition field.
func NewConditionFieldEmptyError(index int) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("condition %d: field is required", index)).WithRetryable(apierror.False)
}

// NewConditionOperatorEmptyError reports a missing condition operator.
func NewConditionOperatorEmptyError(index int) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("condition %d: operator is required", index)).WithRetryable(apierror.False)
}

// NewConditionValueRequiredError reports a missing condition value.
func NewConditionValueRequiredError(index int) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("condition %d: value or value_from is required", index)).WithRetryable(apierror.False)
}

// NewConditionInvalidOperatorError reports an invalid condition operator.
func NewConditionInvalidOperatorError(index int, operator string) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("condition %d: invalid operator: %s", index, operator)).WithRetryable(apierror.False)
}
