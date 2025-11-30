package ratelimit

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

const (
	// MiddlewareName is the name to register this middleware with
	MiddlewareName = "ratelimit"

	// Option keys (dot-separated)
	OptionRequests        = "ratelimit.requests"
	OptionWindow          = "ratelimit.window"
	OptionBurst           = "ratelimit.burst"
	OptionKey             = "ratelimit.key"
	OptionCleanupInterval = "ratelimit.cleanup_interval"
	OptionEntryTTL        = "ratelimit.entry_ttl"
	OptionMaxEntries      = "ratelimit.max_entries"

	// Default values
	DefaultRequests        = 100
	DefaultWindow          = "1m"
	DefaultBurst           = 20
	DefaultKey             = "ip"
	DefaultCleanupInterval = "5m"
	DefaultEntryTTL        = "10m"
	DefaultMaxEntries      = 100000
)

// limiterEntry holds a rate limiter with last access time
type limiterEntry struct {
	limiter    *rate.Limiter
	lastAccess int64 // Unix nano timestamp
}

// limiterStore holds rate limiters per key with TTL-based cleanup
type limiterStore struct {
	mu         sync.RWMutex
	limiters   map[string]*limiterEntry
	limit      rate.Limit
	burst      int
	ttl        time.Duration
	maxEntries int
	stopCh     chan struct{}
	stopped    bool
}

func newLimiterStore(limit rate.Limit, burst int, cleanupInterval, ttl time.Duration, maxEntries int) *limiterStore {
	s := &limiterStore{
		limiters:   make(map[string]*limiterEntry),
		limit:      limit,
		burst:      burst,
		ttl:        ttl,
		maxEntries: maxEntries,
		stopCh:     make(chan struct{}),
	}

	go s.cleanupLoop(cleanupInterval)
	return s
}

func (s *limiterStore) cleanupLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.cleanup()
		case <-s.stopCh:
			return
		}
	}
}

func (s *limiterStore) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixNano()
	ttlNano := s.ttl.Nanoseconds()

	for key, entry := range s.limiters {
		if now-atomic.LoadInt64(&entry.lastAccess) > ttlNano {
			delete(s.limiters, key)
		}
	}
}

func (s *limiterStore) stop() {
	s.mu.Lock()
	if !s.stopped {
		s.stopped = true
		close(s.stopCh)
	}
	s.mu.Unlock()
}

func (s *limiterStore) getLimiter(key string) *rate.Limiter {
	now := time.Now().UnixNano()

	s.mu.RLock()
	entry, exists := s.limiters[key]
	s.mu.RUnlock()

	if exists {
		atomic.StoreInt64(&entry.lastAccess, now)
		return entry.limiter
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock
	if entry, exists := s.limiters[key]; exists {
		atomic.StoreInt64(&entry.lastAccess, now)
		return entry.limiter
	}

	// Enforce max entries limit - evict oldest if at capacity
	if len(s.limiters) >= s.maxEntries {
		s.evictOldest()
	}

	limiter := rate.NewLimiter(s.limit, s.burst)
	s.limiters[key] = &limiterEntry{
		limiter:    limiter,
		lastAccess: now,
	}
	return limiter
}

func (s *limiterStore) evictOldest() {
	var oldestKey string
	oldestTime := time.Now().UnixNano()

	for key, entry := range s.limiters {
		lastAccess := atomic.LoadInt64(&entry.lastAccess)
		if lastAccess < oldestTime {
			oldestTime = lastAccess
			oldestKey = key
		}
	}

	if oldestKey != "" {
		delete(s.limiters, oldestKey)
	}
}

func (s *limiterStore) len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.limiters)
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

	// Parse cleanup interval
	cleanupIntervalStr := options[OptionCleanupInterval]
	if cleanupIntervalStr == "" {
		cleanupIntervalStr = DefaultCleanupInterval
	}
	cleanupInterval, err := parseDuration(cleanupIntervalStr)
	if err != nil {
		cleanupInterval = 5 * time.Minute
	}

	// Parse entry TTL
	entryTTLStr := options[OptionEntryTTL]
	if entryTTLStr == "" {
		entryTTLStr = DefaultEntryTTL
	}
	entryTTL, err := parseDuration(entryTTLStr)
	if err != nil {
		entryTTL = 10 * time.Minute
	}

	// Parse max entries
	maxEntries := DefaultMaxEntries
	if maxStr := options[OptionMaxEntries]; maxStr != "" {
		if parsed, err := strconv.Atoi(maxStr); err == nil && parsed > 0 {
			maxEntries = parsed
		}
	}

	// Calculate rate limit
	limit := rate.Limit(float64(requests) / window.Seconds())

	store := newLimiterStore(limit, burst, cleanupInterval, entryTTL, maxEntries)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract key based on strategy
			key := extractKey(r, keyStrategy)
			if key == "" {
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
