package clock

import (
	"context"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var timeReferenceKey = &ctxapi.Key{Name: "clock.timereference"}

// TimeReference provides deterministic time for workflow execution
type TimeReference interface {
	// Now returns the current workflow time
	Now() time.Time

	// StartTime returns the workflow start time (used for os.clock())
	StartTime() time.Time
}

// WithTimeReference stores a TimeReference in the context frame
func WithTimeReference(ctx context.Context, ref TimeReference) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	return fc.Set(timeReferenceKey, ref)
}

// GetTimeReference retrieves the TimeReference from context
func GetTimeReference(ctx context.Context) TimeReference {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}

	if val, ok := fc.Get(timeReferenceKey); ok {
		if ref, ok := val.(TimeReference); ok {
			return ref
		}
	}

	return nil
}
