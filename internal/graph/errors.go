package graph

import (
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

func NewNodeDoesNotExistError[T comparable](node T) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "node does not exist",
		retryable: apierror.False,
		details: attrs.Bag{
			"node": node,
		},
	}
}

func NewNoEdgesFromNodeError[T comparable](node T) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "no edges exist from node",
		retryable: apierror.False,
		details: attrs.Bag{
			"node": node,
		},
	}
}

func NewEdgeDoesNotExistError[T comparable](from, to T) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "edge does not exist",
		retryable: apierror.False,
		details: attrs.Bag{
			"from": from,
			"to":   to,
		},
	}
}

func NewCycleDetectedError[T comparable](cycle []T) *Error {
	return &Error{
		kind:      apierror.KindConflict,
		message:   "cycle detected",
		retryable: apierror.False,
		details: attrs.Bag{
			"cycle": cycle,
		},
	}
}

func NewCycleDetectedWithStuckNodesError(stuckNodesInfo string) *Error {
	return &Error{
		kind:      apierror.KindConflict,
		message:   "cycle detected but could not identify path",
		retryable: apierror.False,
		details: attrs.Bag{
			"stuck_nodes": stuckNodesInfo,
		},
	}
}

func NewNoPathExistsError[T comparable](from, to T) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "no path exists",
		retryable: apierror.False,
		details: attrs.Bag{
			"from": from,
			"to":   to,
		},
	}
}

func NewInvalidLevelError(level int) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid level",
		retryable: apierror.False,
		details: attrs.Bag{
			"level": level,
		},
	}
}
