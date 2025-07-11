package firewall

import (
	"net/http"

	"github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/security"
	httpapi "github.com/ponyruntime/pony/api/service/http"
	"go.uber.org/zap"
)

// Endpoint firewall option constants
const (
	EndpointMiddlewareName = "endpoint_firewall"
	EndpointDefaultAction  = "access"
	EndpointOptionAction   = "endpoint_action"
)

// CreateEndpointFirewallMiddleware creates a post-match security firewall middleware
// that blocks requests that don't have sufficient permissions for a specific endpoint
func CreateEndpointFirewallMiddleware(options map[string]string) func(http.Handler) http.Handler {
	// Parse options with defaults
	action := options[EndpointOptionAction]
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

			// Get the route information which will provide the endpoint id
			rInfo, ok := httpapi.GetRouteInfo(ctx)
			if !ok {
				logs.GetLogger(ctx).Debug("endpoint firewall: no route info in context",
					zap.String("actor_id", actor.ID),
					zap.String("path", r.URL.Path))
				WriteJSONError(w, http.StatusInternalServerError, false,
					"Internal error",
					"No route information available")
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

			// Permission granted, continue with the request
			logs.GetLogger(ctx).Debug("endpoint firewall: access granted",
				zap.String("actor_id", actor.ID),
				zap.String("endpoint", resourceID))

			next.ServeHTTP(w, r)
		})
	}
}
