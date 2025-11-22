package ratelimit

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	// MiddlewareName is the name to register this middleware with
	MiddlewareName = "ratelimit"

	// Option keys (dot-separated)
	OptionRequests = "ratelimit.requests"
	OptionWindow   = "ratelimit.window"
	OptionBurst    = "ratelimit.burst"
	OptionKey      = "ratelimit.key"

	// Default values
	DefaultRequests = 100
	DefaultWindow   = "1m"
	DefaultBurst    = 20
	DefaultKey      = "ip"
)

// limiterStore holds rate limiters per key
type limiterStore struct {
	mu       sync.RWMutex
	limiters map[string]*rate.Limiter
	limit    rate.Limit
	burst    int
}

func newLimiterStore(limit rate.Limit, burst int) *limiterStore {
	return &limiterStore{
		limiters: make(map[string]*rate.Limiter),
		limit:    limit,
		burst:    burst,
	}
}

func (s *limiterStore) getLimiter(key string) *rate.Limiter {
	s.mu.RLock()
	limiter, exists := s.limiters[key]
	s.mu.RUnlock()

	if exists {
		return limiter
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock
	if limiter, exists := s.limiters[key]; exists {
		return limiter
	}

	limiter = rate.NewLimiter(s.limit, s.burst)
	s.limiters[key] = limiter
	return limiter
}

// CreateRateLimitMiddleware creates a rate limiting middleware using token bucket algorithm
func CreateRateLimitMiddleware(options map[string]string) func(http.Handler) http.Handler {
	// Parse requests per window
	requests := DefaultRequests
	if reqStr := options[OptionRequests]; reqStr != "" {
		if parsed, err := strconv.Atoi(reqStr); err == nil && parsed > 0 {
			requests = parsed
		}
	}

	// Parse window duration
	windowStr := options[OptionWindow]
	if windowStr == "" {
		windowStr = DefaultWindow
	}
	window, err := parseDuration(windowStr)
	if err != nil {
		window = time.Minute
	}

	// Parse burst capacity
	burst := DefaultBurst
	if burstStr := options[OptionBurst]; burstStr != "" {
		if parsed, err := strconv.Atoi(burstStr); err == nil && parsed > 0 {
			burst = parsed
		}
	}

	// Parse key strategy
	keyStrategy := options[OptionKey]
	if keyStrategy == "" {
		keyStrategy = DefaultKey
	}

	// Calculate rate limit
	limit := rate.Limit(float64(requests) / window.Seconds())

	store := newLimiterStore(limit, burst)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract key based on strategy
			key := extractKey(r, keyStrategy)
			if key == "" {
				// If key extraction fails, reject request
				http.Error(w, "Rate limit key extraction failed", http.StatusBadRequest)
				return
			}

			// Get limiter for this key
			limiter := store.getLimiter(key)

			// Check if request is allowed
			if !limiter.Allow() {
				w.Header().Set("X-RateLimit-Limit", strconv.Itoa(requests))
				w.Header().Set("X-RateLimit-Window", windowStr)
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			// Request allowed, continue
			next.ServeHTTP(w, r)
		})
	}
}

// extractKey extracts the rate limit key from the request based on strategy
func extractKey(r *http.Request, strategy string) string {
	parts := strings.SplitN(strategy, ":", 2)
	keyType := parts[0]

	switch keyType {
	case "ip":
		return extractIP(r)
	case "header":
		if len(parts) < 2 {
			return ""
		}
		return r.Header.Get(parts[1])
	case "query":
		if len(parts) < 2 {
			return ""
		}
		return r.URL.Query().Get(parts[1])
	default:
		return extractIP(r)
	}
}

// extractIP extracts the IP address from the request
func extractIP(r *http.Request) string {
	// RemoteAddr should already be set by real_ip middleware if configured
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// parseDuration parses a duration string like "1s", "5m", "1h"
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid duration: %s", s)
	}

	valueStr := s[:len(s)-1]
	unit := s[len(s)-1:]

	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return 0, fmt.Errorf("invalid duration value: %s", s)
	}

	switch unit {
	case "s":
		return time.Duration(value) * time.Second, nil
	case "m":
		return time.Duration(value) * time.Minute, nil
	case "h":
		return time.Duration(value) * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid duration unit: %s (use s, m, or h)", unit)
	}
}
