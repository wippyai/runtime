package cors

import (
	"net/http"
	"strings"
)

// Middleware option constants (dot-separated, preferred)
const (
	MiddlewareName            = "cors"
	OptionAllowOrigins        = "cors.allow.origins"
	OptionAllowMethods        = "cors.allow.methods"
	OptionAllowHeaders        = "cors.allow.headers"
	OptionExposeHeaders       = "cors.expose.headers"
	OptionAllowCredentials    = "cors.allow.credentials" //nolint:gosec // G101: Not a credential - this is a configuration key name
	OptionMaxAge              = "cors.max.age"
	OptionAllowPrivateNetwork = "cors.allow.private.network"

	// Legacy option constants (deprecated, for backward compatibility)
	legacyAllowOrigins      = "allow_origins"
	legacyAllowMethods      = "allow_methods"
	legacyAllowHeaders      = "allow_headers"
	legacyExposeHeaders     = "expose_headers"
	legacyAllowCredentials  = "allow_credentials" //nolint:gosec // G101: Not a credential - this is a configuration key name
	legacyMaxAge            = "max_age"
	legacyAllowPrivateNetwk = "allow_private_network"
)

// CORS default values
const (
	DefaultAllowOrigins     = "*"
	DefaultAllowMethods     = "GET,POST,PUT,DELETE,OPTIONS,PATCH"
	DefaultAllowHeaders     = "Origin,Content-Type,Accept,Authorization,X-Requested-With"
	DefaultExposeHeaders    = ""
	DefaultAllowCredentials = "false"
	DefaultMaxAge           = "86400" // 24 hours
)

// getOption retrieves an option value, checking the new dot-separated key first,
// then falling back to the legacy underscore key for backward compatibility
func getOption(options map[string]string, newKey, legacyKey string) string {
	if val, ok := options[newKey]; ok {
		return val
	}
	return options[legacyKey]
}

// CreateCORSMiddleware creates a CORS middleware with the provided options
func CreateCORSMiddleware(options map[string]string) func(http.Handler) http.Handler {
	// Parse options with defaults (check new keys first, fall back to legacy)
	allowOrigins := getOption(options, OptionAllowOrigins, legacyAllowOrigins)
	if allowOrigins == "" {
		allowOrigins = DefaultAllowOrigins
	}

	allowMethods := getOption(options, OptionAllowMethods, legacyAllowMethods)
	if allowMethods == "" {
		allowMethods = DefaultAllowMethods
	}

	allowHeaders := getOption(options, OptionAllowHeaders, legacyAllowHeaders)
	if allowHeaders == "" {
		allowHeaders = DefaultAllowHeaders
	}

	exposeHeaders := getOption(options, OptionExposeHeaders, legacyExposeHeaders)
	if exposeHeaders == "" {
		exposeHeaders = DefaultExposeHeaders
	}

	allowCredentials := getOption(options, OptionAllowCredentials, legacyAllowCredentials)
	if allowCredentials == "" {
		allowCredentials = DefaultAllowCredentials
	}

	maxAge := getOption(options, OptionMaxAge, legacyMaxAge)
	if maxAge == "" {
		maxAge = DefaultMaxAge
	}

	allowPrivateNetwork := getOption(options, OptionAllowPrivateNetwork, legacyAllowPrivateNetwk) == "true"

	// Create a list of allowed origins for matching
	origins := parseCommaSeparatedList(allowOrigins)

	// Determine if we need to check origins (not needed for "*")
	checkOrigin := allowOrigins != "*"

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Handle preflight OPTIONS requests
			if r.Method == http.MethodOptions {
				// Check if it's a preflight request with Access-Control-Request-Method header
				reqMethod := r.Header.Get("Access-Control-Request-Method")
				if reqMethod != "" {
					if origin == "" {
						// Not a CORS request, continue without CORS headers
						next.ServeHTTP(w, r)
						return
					}

					// Check if origin is allowed
					if checkOrigin && !isAllowedOrigin(origin, origins) {
						// Origin not allowed, continue without CORS headers
						next.ServeHTTP(w, r)
						return
					}

					// Set CORS headers for preflight
					setPreflightHeaders(w, origin, allowMethods, allowHeaders, maxAge, allowCredentials, allowPrivateNetwork)

					// Respond to preflight request with 204 No Content
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}

			// For all requests, handle CORS headers
			if origin != "" {
				// Check if origin is allowed
				if !checkOrigin || isAllowedOrigin(origin, origins) {
					// Set CORS headers for actual request
					setActualHeaders(w, origin, exposeHeaders, allowCredentials, allowPrivateNetwork)
				}
			}

			// Proceed with the request
			next.ServeHTTP(w, r)
		})
	}
}

// isAllowedOrigin checks if the origin is in the list of allowed origins
func isAllowedOrigin(origin string, allowedOrigins []string) bool {
	if len(allowedOrigins) == 0 {
		return false
	}

	for _, allowed := range allowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}

		// Support wildcard subdomains (e.g., "*.example.com")
		if strings.HasPrefix(allowed, "*.") {
			suffix := allowed[1:] // Remove the "*"
			if strings.HasSuffix(origin, suffix) {
				// Check that it's actually a subdomain
				withoutSuffix := origin[:len(origin)-len(suffix)]
				if len(withoutSuffix) > 0 {
					return true
				}
			}
		}

		// Support "localhost" to match any localhost origin (any port)
		if allowed == "localhost" {
			if strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "https://localhost:") ||
				origin == "http://localhost" || origin == "https://localhost" {
				return true
			}
		}
	}

	return false
}

// setPreflightHeaders sets the CORS headers for preflight requests
func setPreflightHeaders(w http.ResponseWriter, origin, methods, headers, maxAge, credentials string, allowPrivateNetwork bool) {
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", methods)

	if headers != "" {
		w.Header().Set("Access-Control-Allow-Headers", headers)
	}

	if maxAge != "" {
		w.Header().Set("Access-Control-Max-Age", maxAge)
	}

	if credentials == "true" {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}

	if allowPrivateNetwork {
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
	}

	// Vary header is important for correct caching
	w.Header().Set("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")
}

// setActualHeaders sets the CORS headers for actual requests
func setActualHeaders(w http.ResponseWriter, origin, exposeHeaders, credentials string, allowPrivateNetwork bool) {
	w.Header().Set("Access-Control-Allow-Origin", origin)

	if exposeHeaders != "" {
		w.Header().Set("Access-Control-Expose-Headers", exposeHeaders)
	}

	if credentials == "true" {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}

	if allowPrivateNetwork {
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
	}

	// Vary header is important for correct caching
	w.Header().Set("Vary", "Origin")
}

// parseCommaSeparatedList parses a comma-separated string into a slice of strings
func parseCommaSeparatedList(list string) []string {
	if list == "" {
		return nil
	}

	parts := strings.Split(list, ",")
	result := make([]string, 0, len(parts))

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result
}
