package policy

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrActionsStringEmpty   = apierror.New(apierror.KindInvalid, "actions string cannot be empty").WithRetryable(apierror.False)
	ErrActionsListEmpty     = apierror.New(apierror.KindInvalid, "actions list cannot be empty").WithRetryable(apierror.False)
	ErrActionsInvalidType   = apierror.New(apierror.KindInvalid, "actions must be a string or list").WithRetryable(apierror.False)
	ErrResourcesStringEmpty = apierror.New(apierror.KindInvalid, "resources string cannot be empty").WithRetryable(apierror.False)
	ErrResourcesListEmpty   = apierror.New(apierror.KindInvalid, "resources list cannot be empty").WithRetryable(apierror.False)
	ErrResourcesInvalidType = apierror.New(apierror.KindInvalid, "resources must be a string or list").WithRetryable(apierror.False)
	ErrExpressionEmpty      = apierror.New(apierror.KindInvalid, "expression cannot be empty").WithRetryable(apierror.False)
)

func NewInvalidPolicyEffectError(effect Effect) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("invalid policy effect: %s", effect)).WithRetryable(apierror.False)
}

func NewConditionFieldEmptyError(index int) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("condition %d: field is required", index)).WithRetryable(apierror.False)
}

func NewConditionOperatorEmptyError(index int) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("condition %d: operator is required", index)).WithRetryable(apierror.False)
}

func NewConditionValueRequiredError(index int) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("condition %d: value or value_from is required", index)).WithRetryable(apierror.False)
}

func NewConditionInvalidOperatorError(index int, operator string) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("condition %d: invalid operator: %s", index, operator)).WithRetryable(apierror.False)
}
