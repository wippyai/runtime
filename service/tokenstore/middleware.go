package tokenstore

import (
	"go.uber.org/zap"
	"net/http"
	"strings"

	"github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/api/security"
)

// Middleware option constants
const (
	// MiddlewareName is the name to register this middleware with
	MiddlewareName = "token_auth"

	// OptionTokenStore is the option key for the token store ID
	OptionTokenStore = "token_store"

	// OptionHeaderName is the option key for the header name
	OptionHeaderName = "header_name"

	// OptionHeaderPrefix is the option key for the header prefix
	OptionHeaderPrefix = "header_prefix"

	// OptionQueryParam is the option key for the query parameter name
	OptionQueryParam = "query_param"

	// OptionCookieName is the option key for the cookie name
	OptionCookieName = "cookie_name"

	// Default values
	DefaultHeaderName   = "Authorization"
	DefaultHeaderPrefix = "Bearer "
	DefaultQueryParam   = "token"
	DefaultCookieName   = "token"
)

// CreateTokenAuthMiddleware creates a token authentication middleware that only enriches request context
func CreateTokenAuthMiddleware(options map[string]string) func(http.Handler) http.Handler {
	// Parse options with defaults
	tokenStoreID := registry.ParseID(options[OptionTokenStore])
	headerName := options[OptionHeaderName]
	if headerName == "" {
		headerName = DefaultHeaderName
	}
	headerPrefix := options[OptionHeaderPrefix]
	if headerPrefix == "" {
		headerPrefix = DefaultHeaderPrefix
	}
	queryParam := options[OptionQueryParam]
	if queryParam == "" {
		queryParam = DefaultQueryParam
	}
	cookieName := options[OptionCookieName]
	if cookieName == "" {
		cookieName = DefaultCookieName
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Get resources from context
			resources := resource.GetResources(ctx)
			logger := logs.GetLogger(ctx)

			// Extract token from request
			tokenStr := extractTokenFromRequest(r, headerName, headerPrefix, queryParam, cookieName)

			// If we have a token, try to validate it
			if tokenStr != "" {
				// Get token store
				tokenStoreRes, err := resources.Acquire(ctx, tokenStoreID, resource.ModeNormal)
				if err == nil {
					defer tokenStoreRes.Release()

					if storeImpl, err := tokenStoreRes.Get(); err == nil {
						if tokenStore, ok := storeImpl.(security.TokenStore); ok {
							// Validate token
							if actor, scope, err := tokenStore.Validate(ctx, security.Token(tokenStr)); err == nil {
								// Token is valid - add actor and scope to context
								ctx = security.WithActor(ctx, actor)
								ctx = security.WithScope(ctx, scope)

								// Log the authenticated user at debug level
								logger.Debug("authenticated user",
									zap.String("actor_id", actor.ID))
							} else {
								// Log token validation error at debug level
								logger.Debug("token validation failed", zap.Error(err))
							}
						}
					}
				}
			}

			// Always continue with the request - with enriched context if token was valid
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// extractTokenFromRequest tries to extract a token from the request using configured methods
func extractTokenFromRequest(r *http.Request, headerName, headerPrefix, queryParam, cookieName string) string {
	// Try Authorization header
	if headerName != "" {
		authHeader := r.Header.Get(headerName)
		if authHeader != "" {
			// Check for prefix
			if headerPrefix != "" && strings.HasPrefix(authHeader, headerPrefix) {
				return strings.TrimPrefix(authHeader, headerPrefix)
			} else if headerPrefix == "" {
				return authHeader
			}
		}
	}

	// Try query parameter
	if queryParam != "" {
		if token := r.URL.Query().Get(queryParam); token != "" {
			return token
		}
	}

	// Try cookie
	if cookieName != "" {
		if cookie, err := r.Cookie(cookieName); err == nil && cookie.Value != "" {
			return cookie.Value
		}
	}

	return ""
}
