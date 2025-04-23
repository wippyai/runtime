package security

import (
	"context"
	"github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/security"
	"go.uber.org/zap"
)

const (
	// STRICT determines if security checks are enforced when context is incomplete
	// When false, incomplete security contexts will default to allow
	STRICT = false
)

// IsAllowed checks if the action on the resource is allowed based on security context
func IsAllowed(ctx context.Context, action, resource string, meta registry.Metadata) bool {
	actor, hasActor := security.GetActor(ctx)
	scope, hasScope := security.GetScope(ctx)

	// Get logger from context
	logger := logs.GetLogger(ctx)

	// If we have both actor and scope, evaluate directly
	if hasActor && hasScope {
		result := scope.Evaluate(actor, action, resource, meta)
		return result == security.Allow
	}

	// Security context is incomplete, log a warning
	if !hasActor {
		logger.Warn("security check with missing actor",
			zap.String("action", action),
			zap.String("resource", resource))
	}

	if !hasScope {
		logger.Warn("security check with missing policy scope",
			zap.String("action", action),
			zap.String("resource", resource))
	}

	// In strict mode, deny access when security context is incomplete
	if STRICT {
		return false
	}

	// In non-strict mode, allow access when security context is incomplete
	return true
}
