// Package workflow provides support for deterministic execution in workflow contexts.
// When deterministic mode is enabled, modules that perform non-deterministic operations
// (UUID generation, random numbers, time) yield their operations to be handled
// by an external dispatcher that can record and replay results.
package workflow

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var deterministicKey = &ctxapi.Key{Name: "workflow.deterministic"}

// SetDeterministic marks the frame context as requiring deterministic execution.
// Non-deterministic operations will yield to the dispatcher for recording/replay.
func SetDeterministic(ctx context.Context) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctxapi.ErrNoFrameContext
	}
	return fc.Set(deterministicKey, true)
}

// IsDeterministic returns true if the context requires deterministic execution.
func IsDeterministic(ctx context.Context) bool {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return false
	}
	if val, ok := fc.Get(deterministicKey); ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return false
}
