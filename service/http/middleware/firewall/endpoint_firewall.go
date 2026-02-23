// SPDX-License-Identifier: MPL-2.0

package firewall

import (
	"net/http"

	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/security"
	httpapi "github.com/wippyai/runtime/api/service/http"
	"go.uber.org/zap"
)

// Endpoint firewall option constants
const (
	EndpointMiddlewareName = "endpoint_firewall"
	EndpointDefaultAction  = "access"

	// EndpointOptionAction is an option key (dot-separated, preferred)
	EndpointOptionAction = "endpoint_firewall.action"

	// Legacy option keys (deprecated, for backward compatibility)
	legacyEndpointAction = "endpoint_action"
)

// CreateEndpointFirewallMiddleware creates a post-match security firewall middleware
// that blocks requests that don't have sufficient permissions for a specific endpoint
func CreateEndpointFirewallMiddleware(options map[string]string) func(http.Handler) http.Handler {
	// Parse options with defaults (check new keys first, fall back to legacy)
	action := getOption(options, EndpointOptionAction, legacyEndpointAction)
	if action == "" {
		action = EndpointDefaultAction
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Get current actor from context (should have been set by token_auth middleware)
			actor, hasActor := security.GetActor(ctx)

			// If there's no actor, deny access
			if !hasActor || actor.ID == "" {
				logs.GetLogger(ctx).Debug("endpoint firewall: no actor in context")
				WriteJSONError(w, http.StatusUnauthorized, false,
					"Authentication required",
					"You must be authenticated to access this endpoint")
				return
			}

			// Check if the actor has permission to access the endpoint
			scope, hasScope := security.GetScope(ctx)
			if !hasScope || scope == nil {
				logs.GetLogger(ctx).Debug("endpoint firewall: no scope in context",
					zap.String("actor_id", actor.ID))
				WriteJSONError(w, http.StatusForbidden, false,
					"Access denied",
					"No authorization scope available")
				return
			}

			// Get the route information which will provide the endpoint ID
			rInfo, ok := httpapi.GetRouteInfo(ctx)
			if !ok {
				logs.GetLogger(ctx).Error("endpoint firewall: no route info in context - this middleware must be used as post-middleware on endpoints, not as router-level middleware",
					zap.String("actor_id", actor.ID),
					zap.String("path", r.URL.Path))
				WriteJSONError(w, http.StatusInternalServerError, false,
					"Configuration error",
					"endpoint_firewall middleware must be configured as post-middleware on endpoints, not as router-level middleware")
				return
			}

			resourceID := rInfo.Endpoint.String()

			// Now check the permission
			result := scope.Evaluate(actor, action, resourceID, nil)
			if result != security.Allow {
				// Permission denied
				logs.GetLogger(ctx).Warn("endpoint firewall: permission denied",
					zap.String("actor_id", actor.ID),
					zap.String("action", action),
					zap.String("endpoint", resourceID))

				WriteJSONError(w, http.StatusForbidden, false,
					"Access denied",
					"You don't have permission to access this endpoint")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
