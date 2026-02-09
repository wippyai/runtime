package security

import (
	"context"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	"go.uber.org/zap"
)

// IsAllowed checks if the action on the resource is allowed based on security context
func IsAllowed(ctx context.Context, action, resource string, meta attrs.Bag) bool {
	actor, hasActor := security.GetActor(ctx)
	scope, hasScope := security.GetScope(ctx)

	// Get logger from context
	logger := logs.GetLogger(ctx)

	// If we have both actor and scope, evaluate directly
	if hasActor && hasScope {
		result := scope.Evaluate(actor, action, resource, meta)
		return result == security.Allow
	}

	// Security context is incomplete - get PID and ID for debugging
	pid, hasPID := runtime.GetFramePID(ctx)
	pidStr := ""
	if hasPID {
		pidStr = pid.String()
	}

	frameID, hasFrameID := runtime.GetFrameID(ctx)
	idStr := ""
	if hasFrameID {
		idStr = frameID.String()
	}

	if !hasActor {
		logger.Debug("security check with missing actor",
			zap.String("action", action),
			zap.String("resource", resource),
			zap.String("pid", pidStr),
			zap.String("id", idStr))
	}

	if !hasScope {
		logger.Debug("security check with missing policy scope",
			zap.String("action", action),
			zap.String("resource", resource),
			zap.String("pid", pidStr),
			zap.String("id", idStr))
	}

	// In strict mode, deny access when security context is incomplete
	if security.IsStrictMode(ctx) {
		return false
	}

	// In non-strict mode, allow access when security context is incomplete
	return true
}
