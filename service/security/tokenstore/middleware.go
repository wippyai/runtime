package tokenstore

import (
	"net/http"
	"strings"

	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/security"
)

// Middleware option constants
const (
	// MiddlewareName is the name to register this middleware with
	MiddlewareName = "token_auth"

	// Option keys (dot-separated, preferred)
	OptionTokenStore   = "token_auth.store" //nolint:gosec // config key, not credentials
	OptionHeaderName   = "token_auth.header.name"
	OptionHeaderPrefix = "token_auth.header.prefix"
	OptionQueryParam   = "token_auth.query.param"
	OptionCookieName   = "token_auth.cookie.name"

	// Legacy option keys (deprecated, for backward compatibility)
	legacyTokenStore   = "token_store"
	legacyHeaderName   = "header_name"
	legacyHeaderPrefix = "header_prefix"
	legacyQueryParam   = "query_param"
	legacyCookieName   = "cookie_name"

	// Default values
	DefaultHeaderName   = "Authorization"
	DefaultHeaderPrefix = "Bearer "
	DefaultQueryParam   = "x-auth-token"
	DefaultCookieName   = "x-auth-token"
)

// getOption retrieves an option value, checking the new dot-separated key first,
// then falling back to the legacy underscore key for backward compatibility
func getOption(options map[string]string, newKey, legacyKey string) string {
	if val, ok := options[newKey]; ok {
		return val
	}
	return options[legacyKey]
}

// CreateTokenAuthMiddleware creates a token authentication middleware that only enriches request context
func CreateTokenAuthMiddleware(options map[string]string) func(http.Handler) http.Handler {
	// Parse options with defaults (check new keys first, fall back to legacy)
	tokenStoreID := registry.ParseID(getOption(options, OptionTokenStore, legacyTokenStore))
	headerName := getOption(options, OptionHeaderName, legacyHeaderName)
	if headerName == "" {
		headerName = DefaultHeaderName
	}
	headerPrefix := getOption(options, OptionHeaderPrefix, legacyHeaderPrefix)
	if headerPrefix == "" {
		headerPrefix = DefaultHeaderPrefix
	}
	queryParam := getOption(options, OptionQueryParam, legacyQueryParam)
	if queryParam == "" {
		queryParam = DefaultQueryParam
	}
	cookieName := getOption(options, OptionCookieName, legacyCookieName)
	if cookieName == "" {
		cookieName = DefaultCookieName
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Get resources from context
			resources := resource.GetRegistry(ctx)
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
								if err := security.SetActor(ctx, actor); err != nil {
									logger.Error("failed to set actor in context",
										zap.Error(err),
										zap.String("actor_id", actor.ID))
								}

								if err := security.SetScope(ctx, scope); err != nil {
									logger.Error("failed to set scope in context",
										zap.Error(err),
										zap.Int("policies", len(scope.Policies())))
								}
							} else {
								// Log token validation error at debug level
								logger.Debug("token validation failed", zap.Error(err), zap.String("actor_id", actor.ID))
							}
						}
					}
				} else {
					logger.Error("invalid auth context",
						zap.String("store", tokenStoreID.String()),
						zap.Error(err))
				}
			}

			// Always continue with the request - with enriched context if token was valid
			next.ServeHTTP(w, r)
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
