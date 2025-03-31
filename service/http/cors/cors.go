package cors

import (
	"net/http"
	"strings"
)

// CORS middleware option constants
const (
	MiddlewareName              = "cors"
	CORSOptionAllowOrigins      = "allow_origins"         // Comma-separated list of allowed origins
	CORSOptionAllowMethods      = "allow_methods"         // Comma-separated list of allowed methods
	CORSOptionAllowHeaders      = "allow_headers"         // Comma-separated list of allowed headers
	CORSOptionExposeHeaders     = "expose_headers"        // Comma-separated list of headers to expose
	CORSOptionAllowCredentials  = "allow_credentials"     // "true" or "false"
	CORSOptionMaxAge            = "max_age"               // Max age in seconds for preflight requests
	CORSOptionAllowPrivateNetwk = "allow_private_network" // "true" or "false"
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

// CreateCORSMiddleware creates a CORS middleware with the provided options
func CreateCORSMiddleware(options map[string]string) func(http.Handler) http.Handler {
	// Parse options with defaults
	allowOrigins := options[CORSOptionAllowOrigins]
	if allowOrigins == "" {
		allowOrigins = DefaultAllowOrigins
	}

	allowMethods := options[CORSOptionAllowMethods]
	if allowMethods == "" {
		allowMethods = DefaultAllowMethods
	}

	allowHeaders := options[CORSOptionAllowHeaders]
	if allowHeaders == "" {
		allowHeaders = DefaultAllowHeaders
	}

	exposeHeaders := options[CORSOptionExposeHeaders]
	if exposeHeaders == "" {
		exposeHeaders = DefaultExposeHeaders
	}

	allowCredentials := options[CORSOptionAllowCredentials]
	if allowCredentials == "" {
		allowCredentials = DefaultAllowCredentials
	}

	maxAge := options[CORSOptionMaxAge]
	if maxAge == "" {
		maxAge = DefaultMaxAge
	}

	allowPrivateNetwork := options[CORSOptionAllowPrivateNetwk] == "true"

	// Create a list of allowed origins for matching
	origins := parseCommaSeparatedList(allowOrigins)

	// Determine if we need to check origins (not needed for "*")
	checkOrigin := allowOrigins != "*"

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Handle preflight OPTIONS requests
			if r.Method == http.MethodOptions {
				// Check if it's a preflight request with Access-Control-Request-Method header
				reqMethod := r.Header.Get("Access-Control-Request-Method")
				if reqMethod != "" {
					origin := r.Header.Get("Origin")
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
			origin := r.Header.Get("Origin")
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
