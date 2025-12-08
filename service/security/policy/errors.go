package policy

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
	cause     error
}

func (e *Error) Error() string               { return e.message }
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }
func (e *Error) Unwrap() error               { return e.cause }

var (
	ErrEmptyFieldPath            = &Error{kind: apierror.KindInvalid, message: "empty field path"}
	ErrNoActorFieldSpecified     = &Error{kind: apierror.KindInvalid, message: "no actor field specified"}
	ErrNoMetadataFieldSpecified  = &Error{kind: apierror.KindInvalid, message: "no metadata field specified"}
	ErrNilOrEmptyActor           = &Error{kind: apierror.KindInvalid, message: "nil or empty actor"}
	ErrNumericComparisonRequired = &Error{kind: apierror.KindInvalid, message: "numeric comparison requires numeric values"}
	ErrInOperatorRequiresSlice   = &Error{kind: apierror.KindInvalid, message: "'in' operator requires slice or array for comparison"}
	ErrContainsRequiresString    = &Error{kind: apierror.KindInvalid, message: "'contains' operator requires string or slice field value"}
	ErrMatchesRequiresString     = &Error{kind: apierror.KindInvalid, message: "'matches' operator requires string values"}
	ErrExpressionEmpty           = &Error{kind: apierror.KindInvalid, message: "expression cannot be empty"}
	ErrConfigNil                 = &Error{kind: apierror.KindInvalid, message: "config cannot be nil"}
)

func NewInvalidRegexPatternError(pattern string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "invalid regex pattern " + pattern,
		cause:   cause,
	}
}

func NewInvalidActorFieldPathError(fieldPath string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "invalid actor field path: " + fieldPath,
	}
}

func NewInvalidMetaFieldPathError(fieldPath string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "invalid meta field path: " + fieldPath,
	}
}

func NewUnknownActorFieldError(field string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "unknown actor field: " + field,
	}
}

func NewUnsupportedOperatorError(operator string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "unsupported operator: " + operator,
	}
}

func NewUnknownNumericOperatorError(operator string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "unknown numeric operator: " + operator,
	}
}

func NewRegexPatternNotCompiledError(pattern string) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "regex pattern " + pattern + " not pre-compiled",
	}
}

func NewExprCompilationError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "expression compilation failed",
		cause:   cause,
	}
}

func NewExprEvaluationError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "expression evaluation failed",
		cause:   cause,
	}
}

func NewExprNotBooleanError(actualType string) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "expression did not return boolean, got " + actualType,
	}
}

func NewCompileExpressionError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "failed to compile expression",
		cause:   cause,
	}
}

func NewUnsupportedPolicyKindError(kind registry.Kind) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "unsupported policy kind: " + kind,
	}
}

func NewUnsupportedEntryKindError(kind registry.Kind) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "unsupported entry kind: " + kind,
	}
}

func NewDecodePolicyConfigError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "failed to decode policy config",
		cause:   cause,
	}
}

func NewCreatePolicyError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to create policy",
		cause:   cause,
	}
}

func NewDecodeExprPolicyConfigError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "failed to decode expr policy config",
		cause:   cause,
	}
}

func NewCreateExprPolicyError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to create expr policy",
		cause:   cause,
	}
}

func NewCreatePolicyEntryError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to create policy entry",
		cause:   cause,
	}
}
