// SPDX-License-Identifier: MPL-2.0

package policy

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

var (
	ErrEmptyFieldPath            = apierror.New(apierror.Invalid, "empty field path").WithRetryable(apierror.False)
	ErrNoActorFieldSpecified     = apierror.New(apierror.Invalid, "no actor field specified").WithRetryable(apierror.False)
	ErrNoMetadataFieldSpecified  = apierror.New(apierror.Invalid, "no metadata field specified").WithRetryable(apierror.False)
	ErrNilOrEmptyActor           = apierror.New(apierror.Invalid, "nil or empty actor").WithRetryable(apierror.False)
	ErrNumericComparisonRequired = apierror.New(apierror.Invalid, "numeric comparison requires numeric values").WithRetryable(apierror.False)
	ErrInOperatorRequiresSlice   = apierror.New(apierror.Invalid, "'in' operator requires slice or array for comparison").WithRetryable(apierror.False)
	ErrContainsRequiresString    = apierror.New(apierror.Invalid, "'contains' operator requires string or slice field value").WithRetryable(apierror.False)
	ErrMatchesRequiresString     = apierror.New(apierror.Invalid, "'matches' operator requires string values").WithRetryable(apierror.False)
	ErrExpressionEmpty           = apierror.New(apierror.Invalid, "expression cannot be empty").WithRetryable(apierror.False)
	ErrConfigNil                 = apierror.New(apierror.Invalid, "config cannot be nil").WithRetryable(apierror.False)
)

func NewInvalidRegexPatternError(pattern string, cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid regex pattern").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"pattern": pattern,
			"cause":   cause.Error(),
		})).
		WithCause(cause)
}

func NewInvalidActorFieldPathError(fieldPath string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid actor field path").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"field_path": fieldPath}))
}

func NewInvalidMetaFieldPathError(fieldPath string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid meta field path").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"field_path": fieldPath}))
}

func NewUnknownActorFieldError(field string) apierror.Error {
	return apierror.New(apierror.Invalid, "unknown actor field").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"field": field}))
}

func NewUnsupportedOperatorError(operator string) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported operator").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"operator": operator}))
}

func NewUnknownNumericOperatorError(operator string) apierror.Error {
	return apierror.New(apierror.Invalid, "unknown numeric operator").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"operator": operator}))
}

func NewRegexPatternNotCompiledError(pattern string) apierror.Error {
	return apierror.New(apierror.Internal, "regex pattern not pre-compiled").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"pattern": pattern}))
}

func NewExprCompilationError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "expression compilation failed").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}

func NewExprEvaluationError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "expression evaluation failed").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}

func NewExprNotBooleanError(actualType string) apierror.Error {
	return apierror.New(apierror.Internal, "expression did not return boolean").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"actual_type": actualType}))
}

func NewCompileExpressionError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to compile expression").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}

func NewUnsupportedPolicyKindError(kind registry.Kind) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported policy kind").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewUnsupportedEntryKindError(kind registry.Kind) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported entry kind").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewDecodePolicyConfigError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to decode policy config").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}

func NewCreatePolicyError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create policy").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}

func NewDecodeExprPolicyConfigError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to decode expr policy config").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}

func NewCreateExprPolicyError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create expr policy").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}

func NewCreatePolicyEntryError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create policy entry").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}
