package graph

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrCycleDetected = apierror.New(apierror.KindInvalid, "cycle detected in dependency graph").WithRetryable(apierror.False)
)

func NewNodeNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.KindNotFound, fmt.Sprintf("node not found: %s", id)).WithRetryable(apierror.False)
}

func NewNodeExistsError(id string) apierror.Error {
	return apierror.New(apierror.KindAlreadyExists, fmt.Sprintf("node already exists: %s", id)).WithRetryable(apierror.False)
}

func NewNodeDoesNotExistError(id any) apierror.Error {
	return apierror.New(apierror.KindNotFound, fmt.Sprintf("node does not exist: %v", id)).WithRetryable(apierror.False)
}

func NewNoEdgesFromNodeError(from any) apierror.Error {
	return apierror.New(apierror.KindNotFound, fmt.Sprintf("no edges from node: %v", from)).WithRetryable(apierror.False)
}

func NewEdgeDoesNotExistError(from, to any) apierror.Error {
	return apierror.New(apierror.KindNotFound, fmt.Sprintf("edge does not exist: %v -> %v", from, to)).WithRetryable(apierror.False)
}

func NewCycleDetectedError(cycle any) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("cycle detected: %v", cycle)).WithRetryable(apierror.False)
}

func NewCycleDetectedWithStuckNodesError(info string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "cycle detected with stuck nodes: "+info).WithRetryable(apierror.False)
}

func NewNoPathExistsError(from, to any) apierror.Error {
	return apierror.New(apierror.KindNotFound, fmt.Sprintf("no path exists from %v to %v", from, to)).WithRetryable(apierror.False)
}

func NewInvalidLevelError(level int) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("invalid level: %d", level)).WithRetryable(apierror.False)
}
