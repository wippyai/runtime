package policy

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
	policyapi "github.com/wippyai/runtime/api/service/security/policy"
)

var (
	ErrActionsStringEmpty = apierror.New(apierror.Invalid, "actions string cannot be empty").WithRetryable(apierror.False)

	ErrActionsListEmpty = apierror.New(apierror.Invalid, "actions list cannot be empty").WithRetryable(apierror.False)

	ErrActionsInvalidType = apierror.New(apierror.Invalid, "actions must be either a string or a list of strings").WithRetryable(apierror.False)

	ErrResourcesStringEmpty = apierror.New(apierror.Invalid, "resources string cannot be empty").WithRetryable(apierror.False)

	ErrResourcesListEmpty = apierror.New(apierror.Invalid, "resources list cannot be empty").WithRetryable(apierror.False)

	ErrResourcesInvalidType = apierror.New(apierror.Invalid, "resources must be either a string or a list of strings").WithRetryable(apierror.False)

	ErrEmptyFieldPath            = apierror.New(apierror.Invalid, "empty field path")
	ErrNoActorFieldSpecified     = apierror.New(apierror.Invalid, "no actor field specified")
	ErrNoMetadataFieldSpecified  = apierror.New(apierror.Invalid, "no metadata field specified")
	ErrNilOrEmptyActor           = apierror.New(apierror.Invalid, "nil or empty actor")
	ErrNumericComparisonRequired = apierror.New(apierror.Invalid, "numeric comparison requires numeric values")
	ErrInOperatorRequiresSlice   = apierror.New(apierror.Invalid, "'in' operator requires slice or array for comparison")
	ErrContainsRequiresString    = apierror.New(apierror.Invalid, "'contains' operator requires string or slice field value")
	ErrMatchesRequiresString     = apierror.New(apierror.Invalid, "'matches' operator requires string values")
	ErrExpressionEmpty           = apierror.New(apierror.Invalid, "expression cannot be empty")
	ErrConfigNil                 = apierror.New(apierror.Invalid, "config cannot be nil")
)

func NewInvalidPolicyEffectError(effect policyapi.Effect) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("invalid policy effect: %s", effect)).WithRetryable(apierror.False)
}

func NewConditionFieldEmptyError(index int) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("condition[%d]: field cannot be empty", index)).WithRetryable(apierror.False)
}

func NewConditionOperatorEmptyError(index int) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("condition[%d]: operator cannot be empty", index)).WithRetryable(apierror.False)
}

func NewConditionValueRequiredError(index int) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("condition[%d]: either value or value_from must be provided", index)).WithRetryable(apierror.False)
}

func NewConditionInvalidOperatorError(index int, operator string) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("condition[%d]: invalid operator: %s", index, operator)).WithRetryable(apierror.False)
}

func NewInvalidRegexPatternError(pattern string, cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid regex pattern "+pattern).WithCause(cause)
}

func NewInvalidActorFieldPathError(fieldPath string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid actor field path: "+fieldPath)
}

func NewInvalidMetaFieldPathError(fieldPath string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid meta field path: "+fieldPath)
}

func NewUnknownActorFieldError(field string) apierror.Error {
	return apierror.New(apierror.Invalid, "unknown actor field: "+field)
}

func NewUnsupportedOperatorError(operator string) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported operator: "+operator)
}

func NewUnknownNumericOperatorError(operator string) apierror.Error {
	return apierror.New(apierror.Invalid, "unknown numeric operator: "+operator)
}

func NewRegexPatternNotCompiledError(pattern string) apierror.Error {
	return apierror.New(apierror.Internal, "regex pattern "+pattern+" not pre-compiled")
}

func NewExprCompilationError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "expression compilation failed").WithCause(cause)
}

func NewExprEvaluationError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "expression evaluation failed").WithCause(cause)
}

func NewExprNotBooleanError(actualType string) apierror.Error {
	return apierror.New(apierror.Internal, "expression did not return boolean, got "+actualType)
}

func NewCompileExpressionError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to compile expression").WithCause(cause)
}

func NewUnsupportedPolicyKindError(kind registry.Kind) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported policy kind: "+kind)
}

func NewUnsupportedEntryKindError(kind registry.Kind) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported entry kind: "+kind)
}

func NewDecodePolicyConfigError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to decode policy config").WithCause(cause)
}

func NewCreatePolicyError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create policy").WithCause(cause)
}

func NewDecodeExprPolicyConfigError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to decode expr policy config").WithCause(cause)
}

func NewCreateExprPolicyError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create expr policy").WithCause(cause)
}

func NewCreatePolicyEntryError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create policy entry").WithCause(cause)
}
