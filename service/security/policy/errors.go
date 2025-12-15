package policy

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
	policyapi "github.com/wippyai/runtime/api/service/security/policy"
)

var (
	ErrActionsStringEmpty = apierror.New(apierror.KindInvalid, "actions string cannot be empty").WithRetryable(apierror.False)

	ErrActionsListEmpty = apierror.New(apierror.KindInvalid, "actions list cannot be empty").WithRetryable(apierror.False)

	ErrActionsInvalidType = apierror.New(apierror.KindInvalid, "actions must be either a string or a list of strings").WithRetryable(apierror.False)

	ErrResourcesStringEmpty = apierror.New(apierror.KindInvalid, "resources string cannot be empty").WithRetryable(apierror.False)

	ErrResourcesListEmpty = apierror.New(apierror.KindInvalid, "resources list cannot be empty").WithRetryable(apierror.False)

	ErrResourcesInvalidType = apierror.New(apierror.KindInvalid, "resources must be either a string or a list of strings").WithRetryable(apierror.False)

	ErrEmptyFieldPath            = apierror.New(apierror.KindInvalid, "empty field path")
	ErrNoActorFieldSpecified     = apierror.New(apierror.KindInvalid, "no actor field specified")
	ErrNoMetadataFieldSpecified  = apierror.New(apierror.KindInvalid, "no metadata field specified")
	ErrNilOrEmptyActor           = apierror.New(apierror.KindInvalid, "nil or empty actor")
	ErrNumericComparisonRequired = apierror.New(apierror.KindInvalid, "numeric comparison requires numeric values")
	ErrInOperatorRequiresSlice   = apierror.New(apierror.KindInvalid, "'in' operator requires slice or array for comparison")
	ErrContainsRequiresString    = apierror.New(apierror.KindInvalid, "'contains' operator requires string or slice field value")
	ErrMatchesRequiresString     = apierror.New(apierror.KindInvalid, "'matches' operator requires string values")
	ErrExpressionEmpty           = apierror.New(apierror.KindInvalid, "expression cannot be empty")
	ErrConfigNil                 = apierror.New(apierror.KindInvalid, "config cannot be nil")
)

func NewInvalidPolicyEffectError(effect policyapi.Effect) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("invalid policy effect: %s", effect)).WithRetryable(apierror.False)
}

func NewConditionFieldEmptyError(index int) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("condition[%d]: field cannot be empty", index)).WithRetryable(apierror.False)
}

func NewConditionOperatorEmptyError(index int) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("condition[%d]: operator cannot be empty", index)).WithRetryable(apierror.False)
}

func NewConditionValueRequiredError(index int) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("condition[%d]: either value or value_from must be provided", index)).WithRetryable(apierror.False)
}

func NewConditionInvalidOperatorError(index int, operator string) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("condition[%d]: invalid operator: %s", index, operator)).WithRetryable(apierror.False)
}

func NewInvalidRegexPatternError(pattern string, cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid regex pattern "+pattern).WithCause(cause)
}

func NewInvalidActorFieldPathError(fieldPath string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid actor field path: "+fieldPath)
}

func NewInvalidMetaFieldPathError(fieldPath string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid meta field path: "+fieldPath)
}

func NewUnknownActorFieldError(field string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "unknown actor field: "+field)
}

func NewUnsupportedOperatorError(operator string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "unsupported operator: "+operator)
}

func NewUnknownNumericOperatorError(operator string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "unknown numeric operator: "+operator)
}

func NewRegexPatternNotCompiledError(pattern string) apierror.Error {
	return apierror.New(apierror.KindInternal, "regex pattern "+pattern+" not pre-compiled")
}

func NewExprCompilationError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "expression compilation failed").WithCause(cause)
}

func NewExprEvaluationError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "expression evaluation failed").WithCause(cause)
}

func NewExprNotBooleanError(actualType string) apierror.Error {
	return apierror.New(apierror.KindInternal, "expression did not return boolean, got "+actualType)
}

func NewCompileExpressionError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "failed to compile expression").WithCause(cause)
}

func NewUnsupportedPolicyKindError(kind registry.Kind) apierror.Error {
	return apierror.New(apierror.KindInvalid, "unsupported policy kind: "+kind)
}

func NewUnsupportedEntryKindError(kind registry.Kind) apierror.Error {
	return apierror.New(apierror.KindInvalid, "unsupported entry kind: "+kind)
}

func NewDecodePolicyConfigError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "failed to decode policy config").WithCause(cause)
}

func NewCreatePolicyError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create policy").WithCause(cause)
}

func NewDecodeExprPolicyConfigError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "failed to decode expr policy config").WithCause(cause)
}

func NewCreateExprPolicyError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create expr policy").WithCause(cause)
}

func NewCreatePolicyEntryError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create policy entry").WithCause(cause)
}
