package policy

import (
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

var (
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
