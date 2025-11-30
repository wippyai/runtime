package firewall

import (
	"net/http"

	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/security"
	"go.uber.org/zap"
)

// Resource firewall option constants
const (
	ResourceMiddlewareName = "resource_firewall"
	ResourceDefaultAction  = "access"

	// Option keys (dot-separated, preferred)
	ResourceOptionAction = "resource_firewall.action"
	ResourceOptionTarget = "resource_firewall.target"

	// Legacy option keys (deprecated, for backward compatibility)
	legacyResourceAction = "resource_action"
	legacyResourceTarget = "resource_target"
)

// CreateResourceFirewallMiddleware creates a security firewall middleware that blocks requests
// that don't have sufficient permissions to access a resource specified in the options
func CreateResourceFirewallMiddleware(options map[string]string) func(http.Handler) http.Handler {
	// Parse options with defaults (check new keys first, fall back to legacy)
	action := getOption(options, ResourceOptionAction, legacyResourceAction)
	if action == "" {
		action = ResourceDefaultAction
	}

	// Get the resource to check access to
	resourceID := getOption(options, ResourceOptionTarget, legacyResourceTarget)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Get current actor from context (should have been set by token_auth middleware)
			actor, hasActor := security.GetActor(ctx)

			// If there's no actor, deny access
			if !hasActor || actor.ID == "" {
				logs.GetLogger(ctx).Debug("resource firewall: no actor in context")
				WriteJSONError(w, http.StatusUnauthorized, false,
					"Authentication required",
					"You must be authenticated to access this resource")
				return
			}

			// Check if the actor has permission to access the endpoint
			scope, hasScope := security.GetScope(ctx)
			if !hasScope || scope == nil {
				logs.GetLogger(ctx).Debug("resource firewall: no scope in context",
					zap.String("actor_id", actor.ID))
				WriteJSONError(w, http.StatusForbidden, false,
					"Access denied",
					"No authorization scope available")
				return
			}

			// Now check the permission
			result := scope.Evaluate(actor, action, resourceID, nil)
			if result != security.Allow {
				// Permission denied
				logs.GetLogger(ctx).Warn("resource firewall: permission denied",
					zap.String("actor_id", actor.ID),
					zap.String("action", action),
					zap.String("resource", resourceID))

				WriteJSONError(w, http.StatusForbidden, false,
					"Access denied",
					"You don't have permission to access this resource")
				return
			}

			// Permission granted, continue with the request
			logs.GetLogger(ctx).Debug("resource firewall: access granted",
				zap.String("actor_id", actor.ID),
				zap.String("resource", resourceID))

			next.ServeHTTP(w, r)
		})
	}
}
