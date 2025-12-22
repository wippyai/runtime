package code

import (
	"fmt"
	"strings"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
	"github.com/yuin/gopher-lua/types"
)

var (
	ErrNodeNil = apierror.New(apierror.Invalid, "node cannot be nil").WithRetryable(apierror.False)

	ErrModuleNotCompiled = apierror.New(apierror.Invalid, "module nodes are not compiled").WithRetryable(apierror.False)

	ErrCycleDetected = apierror.New(apierror.Invalid, "adding dependency would create a cycle").WithRetryable(apierror.False)
)

func NewNodeNotFoundError(id registry.ID) apierror.Error {
	return apierror.New(apierror.NotFound, fmt.Sprintf("node with ID %v not found", id)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"node_id": id.String()}))
}

func NewNodeExistsError(id registry.ID) apierror.Error {
	return apierror.New(apierror.AlreadyExists, fmt.Sprintf("node with ID %v already exists", id)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"node_id": id.String()}))
}

func NewDependencyExistsError(from, to registry.ID) apierror.Error {
	return apierror.New(apierror.AlreadyExists, fmt.Sprintf("dependency from %v to %v already exists", from, to)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"from": from.String(), "to": to.String()}))
}

func NewDependencyNotFoundError(from, to registry.ID) apierror.Error {
	return apierror.New(apierror.NotFound, fmt.Sprintf("dependency from %v to %v not found", from, to)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"from": from.String(), "to": to.String()}))
}

func NewAliasCollisionError(alias string, nodeID registry.ID, existingTarget registry.ID, existingDirect bool, newTarget registry.ID, newDirect bool) apierror.Error {
	describeSource := func(target registry.ID, direct bool) string {
		if direct {
			if target.NS == "" {
				return "direct module"
			}
			return "direct import"
		}
		return fmt.Sprintf("transitive via %v", target)
	}

	return apierror.New(apierror.Conflict, fmt.Sprintf(
		"alias '%s' collision: %v (%s) vs %v (%s)",
		alias, existingTarget, describeSource(existingTarget, existingDirect), newTarget, describeSource(newTarget, newDirect))).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"alias":           alias,
			"node_id":         nodeID.String(),
			"existing_target": existingTarget.String(),
			"existing_direct": existingDirect,
			"new_target":      newTarget.String(),
			"new_direct":      newDirect,
		}))
}

func NewIncomingDependencyError(nodeID, dependentID registry.ID) apierror.Error {
	return apierror.New(apierror.Conflict, fmt.Sprintf("cannot remove node %v: it has incoming dependencies from node %v", nodeID, dependentID)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"node_id": nodeID.String(), "dependent_id": dependentID.String()}))
}

func NewBuildValidationError(reason string, id registry.ID) apierror.Error {
	return apierror.New(apierror.PermissionDenied, reason).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"node_id": id.String()}))
}

func NewCompileError(id registry.ID, err error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to compile node %v", id)).
		WithCause(err).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"node_id": id.String()}))
}

// ParseError is a parse error with Rust-style rendering.
type ParseError struct {
	cause  error
	source string
}

func (e *ParseError) Error() string {
	// Check if the cause has a Render method (parse.Error)
	if renderer, ok := e.cause.(interface{ Render() string }); ok {
		return renderer.Render()
	}
	return e.cause.Error()
}

func (e *ParseError) Unwrap() error { return e.cause }

func NewParseError(cause error, source string) apierror.Error {
	// Create parse error that uses Render() for formatted output
	pe := &ParseError{cause: cause, source: source}
	return apierror.New(apierror.Invalid, pe.Error()).WithRetryable(apierror.False)
}

func NewAddModuleNodeError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to add module node").WithCause(cause).WithRetryable(apierror.False)
}

func NewAddNodeErrorWithCause(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to add node").WithCause(cause).WithRetryable(apierror.False)
}

func NewAddDependencyError(from, to registry.ID, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to add dependency %s -> %s", from, to)).WithCause(cause).WithRetryable(apierror.False)
}

func NewGetOldDependenciesError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to get old dependencies").WithCause(cause).WithRetryable(apierror.False)
}

func NewRemoveOldDependencyError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to remove old dependency").WithCause(cause).WithRetryable(apierror.False)
}

func NewAddNewDependencyError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to add new dependency").WithCause(cause).WithRetryable(apierror.False)
}

func NewRemoveNodeError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to remove node").WithCause(cause).WithRetryable(apierror.False)
}

// TypeCheckErrorItem represents a single type check error
type TypeCheckErrorItem struct {
	Line    int
	Column  int
	Message string
}

// TypeCheckErrorList wraps multiple type check errors
type TypeCheckErrorList struct {
	Errors []TypeCheckErrorItem
}

func (e *TypeCheckErrorList) Error() string {
	if len(e.Errors) == 0 {
		return "type check failed"
	}
	return e.Errors[0].Message
}

// NewTypeCheckError creates an error for type checking failures
func NewTypeCheckError(id registry.ID, cause error) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("type check failed for %s: %v", id, cause)).
		WithCause(cause).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"node_id": id.String()}))
}

// NewTypeCheckErrorFromDiagnostics creates an error from type checker diagnostics
func NewTypeCheckErrorFromDiagnostics(id registry.ID, diagnostics []types.Diagnostic, source string) apierror.Error {
	sourceLines := types.ParseSource(source)

	var rendered strings.Builder
	var items []TypeCheckErrorItem

	for _, d := range diagnostics {
		if d.Severity == types.SeverityError {
			rendered.WriteString(d.Render(sourceLines))
			rendered.WriteString("\n")
			items = append(items, TypeCheckErrorItem{
				Line:    d.Position.Line,
				Column:  d.Position.Column,
				Message: d.Message,
			})
		}
	}

	cause := &TypeCheckErrorList{Errors: items}

	details := map[string]any{"node_id": id.String(), "rendered": rendered.String()}
	if len(items) > 0 {
		details["error_count"] = len(items)
		details["first_line"] = items[0].Line
		details["first_message"] = items[0].Message
	}

	return apierror.New(apierror.Invalid, rendered.String()).
		WithCause(cause).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(details))
}
