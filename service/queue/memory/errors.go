package memory

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

func newQueueClosedError(queueID registry.ID) error {
	details := attrs.NewBag()
	details.Set("queue_id", queueID.String())
	return &Error{
		kind:      apierror.KindUnavailable,
		message:   "queue " + queueID.String() + " is closed",
		retryable: apierror.False,
		details:   details,
	}
}

func newDriverStoppedError() error {
	return &Error{
		kind:      apierror.KindUnavailable,
		message:   "driver is stopped",
		retryable: apierror.False,
	}
}

func newQueueClosedRequeueError() error {
	return &Error{
		kind:      apierror.KindUnavailable,
		message:   "queue is closed, cannot requeue message",
		retryable: apierror.False,
	}
}

func newQueueFullError() error {
	return &Error{
		kind:      apierror.KindUnavailable,
		message:   "queue is full, cannot requeue message",
		retryable: apierror.True,
	}
}

func newUnsupportedEntryKindError(kind string) error {
	details := attrs.NewBag()
	details.Set("kind", kind)
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "unsupported entry kind: " + kind,
		retryable: apierror.False,
		details:   details,
	}
}

func newDriverExistsError(id registry.ID) error {
	details := attrs.NewBag()
	details.Set("driver_id", id.String())
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "driver " + id.String() + " already exists",
		retryable: apierror.False,
		details:   details,
	}
}

func newDriverNotFoundError(id registry.ID) error {
	details := attrs.NewBag()
	details.Set("driver_id", id.String())
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "driver " + id.String() + " does not exist",
		retryable: apierror.False,
		details:   details,
	}
}
