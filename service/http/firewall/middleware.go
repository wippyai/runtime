package firewall

import (
	"encoding/json"
	"net/http"

	"github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/security"
	httpapi "github.com/ponyruntime/pony/api/service/http"
	"go.uber.org/zap"
)

// Firewall option constants
const (
	// MiddlewareName is the name to register this middleware with
	MiddlewareName = "firewall"

	// OptionEndpointAction is the option key for the default action
	OptionEndpointAction = "endpoint_action"

	// Default values
	DefaultFirewallAction = "access"

	// Metadata tag keys for endpoint configuration
	MetaGuardResource = "guard_resource"
	MetaGuardAction   = "guard_action"
)

// WriteJSONError sends a JSON error response with the specified status code
func WriteJSONError(w http.ResponseWriter, status int, success bool, error string, details string, extra map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	response := map[string]interface{}{
		"success": success,
		"error":   error,
		"details": details,
	}

	// Add any extra fields
	for k, v := range extra {
		response[k] = v
	}

	_ = json.NewEncoder(w).Encode(response)
}

// CreateFirewallMiddleware creates a security firewall middleware that blocks requests
// that don't have sufficient permissions
func CreateFirewallMiddleware(options map[string]string) func(http.Handler) http.Handler {
	// Parse options with defaults
	defaultAction := options[OptionEndpointAction]
	if defaultAction == "" {
		defaultAction = DefaultFirewallAction
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Get current actor from context (should have been set by token_auth middleware)
			actor, hasActor := security.GetActor(ctx)

			// If there's no actor, deny access
			if !hasActor || actor.ID == "" {
				logs.GetLogger(ctx).Debug("firewall: no actor in context")
				WriteJSONError(w, http.StatusUnauthorized, false,
					"Authentication required",
					"You must be authenticated to access this resource",
					nil)
				return
			}

			// Check if the actor has permission to access the endpoint
			scope, hasScope := security.GetScope(ctx)
			if !hasScope || scope == nil {
				logs.GetLogger(ctx).Debug("firewall: no scope in context",
					zap.String("actor_id", actor.ID))
				WriteJSONError(w, http.StatusForbidden, false,
					"Access denied",
					"No authorization scope available",
					nil)
				return
			}

			// Check for endpoint-specific permission
			routeInfo, hasRouteInfo := httpapi.GetRouteInfo(ctx)
			endpointConfig, hasEndpointConfig := httpapi.GetEndpointConfig(ctx)

			// Determine the resource to check permission against
			// First try to get it from endpoint config metadata if available
			var resourceID string
			var action string = defaultAction

			if hasEndpointConfig && endpointConfig.Meta != nil {
				// Check if endpoint has a specific resource defined
				if guardResource := endpointConfig.Meta.StringValue(MetaGuardResource); guardResource != "" {
					resourceID = guardResource
				}

				// Check if endpoint has a specific action defined
				if guardAction := endpointConfig.Meta.StringValue(MetaGuardAction); guardAction != "" {
					action = guardAction
				}
			}

			// If no resource specified in metadata, use the endpoint ID
			if resourceID == "" && hasRouteInfo && routeInfo.Func.Name != "" {
				resourceID = routeInfo.Func.String()
			}

			// If we still don't have a resource ID, use the request path
			if resourceID == "" {
				resourceID = r.URL.Path
			}

			// Now check the permission
			result := scope.Evaluate(actor, action, resourceID, nil)
			if result != security.Allow {
				// Permission denied
				logs.GetLogger(ctx).Debug("firewall: permission denied",
					zap.String("actor_id", actor.ID),
					zap.String("action", action),
					zap.String("resource", resourceID))

				extra := map[string]interface{}{
					"actor": map[string]interface{}{
						"id":       actor.ID,
						"metadata": actor.Meta,
					},
					"permission": map[string]interface{}{
						"action":   action,
						"resource": resourceID,
						"result":   "deny",
					},
				}

				WriteJSONError(w, http.StatusForbidden, false,
					"Access denied",
					"You don't have permission to access this resource",
					extra)
				return
			}

			// Permission granted, continue with the request
			logs.GetLogger(ctx).Debug("firewall: access granted",
				zap.String("actor_id", actor.ID),
				zap.String("resource", resourceID))

			next.ServeHTTP(w, r)
		})
	}
}
