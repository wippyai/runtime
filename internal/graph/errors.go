package graph

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

func NewNodeDoesNotExistError(id any) apierror.Error {
	return apierror.New(apierror.NotFound, fmt.Sprintf("node does not exist: %v", id)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"node": id}))
}

func NewNoEdgesFromNodeError(from any) apierror.Error {
	return apierror.New(apierror.NotFound, fmt.Sprintf("no edges from node: %v", from)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"from": from}))
}

func NewEdgeDoesNotExistError(from, to any) apierror.Error {
	return apierror.New(apierror.NotFound, fmt.Sprintf("edge does not exist: %v -> %v", from, to)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"from": from, "to": to}))
}

func NewCycleDetectedError(cycle any) apierror.Error {
	details := attrs.NewBagFrom(map[string]any{"cycle": cycle})
	return apierror.WithDetails(apierror.Invalid, fmt.Sprintf("cycle detected: %v", cycle), details)
}

func NewCycleDetectedWithStuckNodesError(info string) apierror.Error {
	details := attrs.NewBagFrom(map[string]any{"stuck_nodes": info})
	return apierror.WithDetails(apierror.Invalid, "cycle detected with stuck nodes: "+info, details)
}

func NewNoPathExistsError(from, to any) apierror.Error {
	return apierror.New(apierror.NotFound, fmt.Sprintf("no path exists from %v to %v", from, to)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"from": from, "to": to}))
}

func NewInvalidLevelError(level int) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("invalid level: %d", level)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"level": level}))
}
