package code

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Error implements apierror.Error for code graph errors
type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
	cause     error
}

// Error implements error interface
func (e *Error) Error() string {
	if e.cause != nil {
		return e.message + ": " + e.cause.Error()
	}
	return e.message
}

// Kind implements apierror.Error
func (e *Error) Kind() apierror.Kind {
	return e.kind
}

// Retryable implements apierror.Error
func (e *Error) Retryable() apierror.Ternary {
	return e.retryable
}

// Details implements apierror.Error
func (e *Error) Details() attrs.Attributes {
	return e.details
}

// Unwrap implements error unwrapping
func (e *Error) Unwrap() error {
	return e.cause
}

// Sentinel errors
var (
	ErrNodeNil = &Error{
		kind:      apierror.KindInvalid,
		message:   "node cannot be nil",
		retryable: apierror.False,
	}

	ErrModuleNotCompiled = &Error{
		kind:      apierror.KindInvalid,
		message:   "module nodes are not compiled",
		retryable: apierror.False,
	}

	ErrCycleDetected = &Error{
		kind:      apierror.KindInvalid,
		message:   "adding dependency would create a cycle",
		retryable: apierror.False,
	}
)

// NewNodeNotFoundError creates an error for missing node
func NewNodeNotFoundError(id registry.ID) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   fmt.Sprintf("node with ID %v not found", id),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"node_id": id.String()}),
	}
}

// NewNodeExistsError creates an error for duplicate node
func NewNodeExistsError(id registry.ID) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   fmt.Sprintf("node with ID %v already exists", id),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"node_id": id.String()}),
	}
}

// NewDependencyExistsError creates an error for duplicate dependency
func NewDependencyExistsError(from, to registry.ID) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   fmt.Sprintf("dependency from %v to %v already exists", from, to),
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"from": from.String(),
			"to":   to.String(),
		}),
	}
}

// NewDependencyNotFoundError creates an error for missing dependency
func NewDependencyNotFoundError(from, to registry.ID) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   fmt.Sprintf("dependency from %v to %v not found", from, to),
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"from": from.String(),
			"to":   to.String(),
		}),
	}
}

// NewAliasCollisionError creates an error for alias conflicts
func NewAliasCollisionError(alias string, nodeID registry.ID, existingTarget registry.ID, existingDirect bool, newTarget registry.ID, newDirect bool) *Error {
	describeSource := func(target registry.ID, direct bool) string {
		if direct {
			if target.NS == "" {
				return "direct module"
			}
			return "direct import"
		}
		return fmt.Sprintf("transitive via %v", target)
	}

	return &Error{
		kind: apierror.KindConflict,
		message: fmt.Sprintf(
			"alias '%s' collision: %v (%s) vs %v (%s)",
			alias, existingTarget, describeSource(existingTarget, existingDirect), newTarget, describeSource(newTarget, newDirect)),
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"alias":           alias,
			"node_id":         nodeID.String(),
			"existing_target": existingTarget.String(),
			"existing_direct": existingDirect,
			"new_target":      newTarget.String(),
			"new_direct":      newDirect,
		}),
	}
}

// NewIncomingDependencyError creates an error when removing node with dependents
func NewIncomingDependencyError(nodeID, dependentID registry.ID) *Error {
	return &Error{
		kind:      apierror.KindConflict,
		message:   fmt.Sprintf("cannot remove node %v: it has incoming dependencies from node %v", nodeID, dependentID),
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"node_id":      nodeID.String(),
			"dependent_id": dependentID.String(),
		}),
	}
}

// NewBuildValidationError creates an error for build option validation failures
func NewBuildValidationError(reason string, id registry.ID) *Error {
	return &Error{
		kind:      apierror.KindPermissionDenied,
		message:   reason,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"node_id": id.String()}),
	}
}

// NewCompileError creates an error for compilation failures
func NewCompileError(id registry.ID, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   fmt.Sprintf("failed to compile node %v: %v", id, err),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"node_id": id.String()}),
	}
}

// WrapError wraps a standard error with code graph error context
func WrapError(kind apierror.Kind, err error, retryable apierror.Ternary) *Error {
	return &Error{
		kind:      kind,
		message:   err.Error(),
		retryable: retryable,
		cause:     err,
	}
}

func NewParseError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "parse error",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewAddModuleNodeError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to add module node",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewAddNodeErrorWithCause(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to add node",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewAddDependencyError(from, to registry.ID, cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   fmt.Sprintf("failed to add dependency %s -> %s", from, to),
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewGetOldDependenciesError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to get old dependencies",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewRemoveOldDependencyError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to remove old dependency",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewAddNewDependencyError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to add new dependency",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewRemoveNodeError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to remove node",
		retryable: apierror.False,
		cause:     cause,
	}
}
