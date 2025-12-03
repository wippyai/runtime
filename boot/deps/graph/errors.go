package graph

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
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
	ErrNoLabelsAvailable = &Error{
		kind:    apierror.KindNotFound,
		message: "no labels available",
	}
	ErrNoConstraints = &Error{
		kind:    apierror.KindInvalid,
		message: "no constraints to merge",
	}
)

func NewParseConstraintError(constraint string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("failed to parse constraint %q", constraint),
		cause:   cause,
	}
}

func NewNoMatchingVersionError(constraint string) *Error {
	return &Error{
		kind:    apierror.KindNotFound,
		message: fmt.Sprintf("no version matches constraint %q", constraint),
	}
}

func NewIncompatibleConstraintsError(c1, c2 string) *Error {
	return &Error{
		kind:    apierror.KindConflict,
		message: fmt.Sprintf("constraints %q and %q are incompatible", c1, c2),
	}
}

var ErrManifestProviderRequired = &Error{
	kind:    apierror.KindInvalid,
	message: "manifest provider is required",
}

func NewInvalidModuleNameError(name string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "invalid module name format: " + name + " (expected org/module)",
		details: attrs.NewBagFrom(map[string]any{"name": name}),
	}
}

func NewEmptyModuleNameError(name string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "empty organization or module name: " + name,
		details: attrs.NewBagFrom(map[string]any{"name": name}),
	}
}

func NewFetchManifestsError(level int, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("fetch manifests level %d", level),
		cause:   cause,
		details: attrs.NewBagFrom(map[string]any{"level": level}),
	}
}

func NewFetchManifestError(name Name, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "fetch manifest " + name.String(),
		cause:   cause,
		details: attrs.NewBagFrom(map[string]any{"module": name.String()}),
	}
}

func NewNoMatchingVersionForModuleError(name Name, constraint string) *Error {
	return &Error{
		kind:    apierror.KindNotFound,
		message: "no matching version for " + name.String() + " with constraint " + constraint,
		details: attrs.NewBagFrom(map[string]any{"module": name.String(), "constraint": constraint}),
	}
}

func NewCircularDependencyError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "circular dependency detected",
		cause:   cause,
	}
}
