package queue

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Sentinel errors for queue operations.
var (
	ErrDriverNotStarted = apierror.New(apierror.Unavailable, "queue driver not started").WithRetryable(apierror.True)
	ErrQueueFull        = apierror.New(apierror.Unavailable, "queue is full").WithRetryable(apierror.True)
	ErrQueueClosed      = apierror.New(apierror.Unavailable, "queue is closed").WithRetryable(apierror.False)
	ErrConsumerClosed   = apierror.New(apierror.Unavailable, "consumer closed").WithRetryable(apierror.False)
	ErrNoPublishFunc    = apierror.New(apierror.Unavailable, "no publish function configured").WithRetryable(apierror.False)
)

// NewQueueClosedError creates a queue closed error with ID.
func NewQueueClosedError(id registry.ID) apierror.Error {
	return apierror.E(
		apierror.Unavailable,
		"queue is closed: "+id.String(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"queue_id": id.String()}),
		nil,
	)
}
