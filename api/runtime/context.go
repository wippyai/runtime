// Package runtime provides runtime execution and command management.
package runtime

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
)

// Frame context keys
var (
	// FrameIDKey stores the registry ID of the called function/process
	FrameIDKey = &ctxapi.Key{Name: "runtime.frame_id"}

	// FramePIDKey stores the full PID (relay.PID)
	FramePIDKey = &ctxapi.Key{Name: "runtime.frame_pid"}

	// FrameLifecycleOptionsKey stores lifecycle options (attrs.Attributes)
	FrameLifecycleOptionsKey = &ctxapi.Key{Name: "runtime.frame_lifecycle_options"}
)

// SetFrameID sets the registry ID in the FrameContext.
// Returns error if no frame context or frame is sealed.
func SetFrameID(ctx context.Context, id registry.ID) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ErrNoFrameContext
	}
	return fc.Set(FrameIDKey, id)
}

// GetFrameID retrieves the registry ID from the FrameContext.
// Returns zero ID and false if not found.
func GetFrameID(ctx context.Context) (registry.ID, bool) {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return registry.NewID("", ""), false
	}
	if val, ok := fc.Get(FrameIDKey); ok {
		if id, ok := val.(registry.ID); ok {
			return id, true
		}
	}
	return registry.NewID("", ""), false
}

// SetFramePID sets the PID in the FrameContext.
// Returns error if no frame context or frame is sealed.
func SetFramePID(ctx context.Context, pid relay.PID) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ErrNoFrameContext
	}
	return fc.Set(FramePIDKey, pid)
}

// GetFramePID retrieves the PID from the FrameContext.
// Returns zero PID and false if not found.
func GetFramePID(ctx context.Context) (relay.PID, bool) {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return relay.PID{}, false
	}
	if val, ok := fc.Get(FramePIDKey); ok {
		if pid, ok := val.(relay.PID); ok {
			return pid, true
		}
	}
	return relay.PID{}, false
}

// GetFrameLifecycleOptions retrieves lifecycle options from the FrameContext.
// Returns nil if not found.
func GetFrameLifecycleOptions(ctx context.Context) any {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(FrameLifecycleOptionsKey); ok {
		return val
	}
	return nil
}
