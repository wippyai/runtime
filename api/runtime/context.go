package runtime

import (
	"context"
	"fmt"

	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
)

// Frame context keys
var (
	// FrameIDKey stores the registry ID of the called function/process
	FrameIDKey = &ctxapi.Key{Name: "runtime.frame_id"}

	// FramePIDKey stores the full PID (pubsub.PID)
	FramePIDKey = &ctxapi.Key{Name: "runtime.frame_pid"}

	// FrameHostKey stores the Host instance
	FrameHostKey = &ctxapi.Key{Name: "runtime.frame_host"}
)

// SetFrameID sets the registry ID in the FrameContext.
// Returns error if no frame context or frame is sealed.
func SetFrameID(ctx context.Context, id registry.ID) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return fmt.Errorf("no frame context available")
	}
	return fc.Set(FrameIDKey, id)
}

// GetFrameID retrieves the registry ID from the FrameContext.
// Returns zero ID and false if not found.
func GetFrameID(ctx context.Context) (registry.ID, bool) {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return registry.ID{}, false
	}
	if val, ok := fc.Get(FrameIDKey); ok {
		if id, ok := val.(registry.ID); ok {
			return id, true
		}
	}
	return registry.ID{}, false
}

// SetFramePID sets the PID in the FrameContext.
// Returns error if no frame context or frame is sealed.
func SetFramePID(ctx context.Context, pid pubsub.PID) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return fmt.Errorf("no frame context available")
	}
	return fc.Set(FramePIDKey, pid)
}

// GetFramePID retrieves the PID from the FrameContext.
// Returns zero PID and false if not found.
func GetFramePID(ctx context.Context) (pubsub.PID, bool) {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return pubsub.PID{}, false
	}
	if val, ok := fc.Get(FramePIDKey); ok {
		if pid, ok := val.(pubsub.PID); ok {
			return pid, true
		}
	}
	return pubsub.PID{}, false
}

// SetFrameHost sets the Host in the FrameContext.
// Returns error if no frame context or frame is sealed.
func SetFrameHost(ctx context.Context, host pubsub.Host) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return fmt.Errorf("no frame context available")
	}
	return fc.Set(FrameHostKey, host)
}

// GetFrameHost retrieves the Host from the FrameContext.
// Returns nil and false if not found.
func GetFrameHost(ctx context.Context) (pubsub.Host, bool) {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil, false
	}
	if val, ok := fc.Get(FrameHostKey); ok {
		if host, ok := val.(pubsub.Host); ok {
			return host, true
		}
	}
	return nil, false
}
