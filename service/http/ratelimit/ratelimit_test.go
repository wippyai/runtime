package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestCreateRateLimitMiddleware(t *testing.T) {
	t.Run("allow requests within limit", func(t *testing.T) {
		middleware := CreateRateLimitMiddleware(map[string]string{
			OptionRequests: "10",
			OptionWindow:   "1m",
			OptionBurst:    "5",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"

		// First request should succeed
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("block requests exceeding limit", func(t *testing.T) {
		middleware := CreateRateLimitMiddleware(map[string]string{
			OptionRequests: "2",
			OptionWindow:   "1m",
			OptionBurst:    "2",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"

		// First 2 requests should succeed (burst)
		for i := 0; i < 2; i++ {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "request %d should succeed", i+1)
		}

		// Third request should be rate limited
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusTooManyRequests, w.Code)
		assert.Contains(t, w.Body.String(), "Rate limit exceeded")
	})

	t.Run("different IPs have separate limits", func(t *testing.T) {
		middleware := CreateRateLimitMiddleware(map[string]string{
			OptionRequests: "1",
			OptionWindow:   "1m",
			OptionBurst:    "1",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		// IP 1 makes request
		req1 := httptest.NewRequest("GET", "http://example.com/test", nil)
		req1.RemoteAddr = "192.168.1.1:12345"
		w1 := httptest.NewRecorder()
		handler.ServeHTTP(w1, req1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// IP 1 second request should be rate limited
		w1b := httptest.NewRecorder()
		handler.ServeHTTP(w1b, req1)
		assert.Equal(t, http.StatusTooManyRequests, w1b.Code)

		// IP 2 should have its own limit
		req2 := httptest.NewRequest("GET", "http://example.com/test", nil)
		req2.RemoteAddr = "192.168.1.2:12345"
		w2 := httptest.NewRecorder()
		handler.ServeHTTP(w2, req2)
		assert.Equal(t, http.StatusOK, w2.Code)
	})

	t.Run("rate limit by header", func(t *testing.T) {
		middleware := CreateRateLimitMiddleware(map[string]string{
			OptionRequests: "1",
			OptionWindow:   "1m",
			OptionBurst:    "1",
			OptionKey:      "header:X-API-Key",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		// First request with API key
		req1 := httptest.NewRequest("GET", "http://example.com/test", nil)
		req1.Header.Set("X-API-Key", "key123")
		w1 := httptest.NewRecorder()
		handler.ServeHTTP(w1, req1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// Second request with same API key should be limited
		req2 := httptest.NewRequest("GET", "http://example.com/test", nil)
		req2.Header.Set("X-API-Key", "key123")
		w2 := httptest.NewRecorder()
		handler.ServeHTTP(w2, req2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)

		// Different API key should have separate limit
		req3 := httptest.NewRequest("GET", "http://example.com/test", nil)
		req3.Header.Set("X-API-Key", "key456")
		w3 := httptest.NewRecorder()
		handler.ServeHTTP(w3, req3)
		assert.Equal(t, http.StatusOK, w3.Code)
	})

	t.Run("rate limit by query parameter", func(t *testing.T) {
		middleware := CreateRateLimitMiddleware(map[string]string{
			OptionRequests: "1",
			OptionWindow:   "1m",
			OptionBurst:    "1",
			OptionKey:      "query:user_id",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		// First request for user1
		req1 := httptest.NewRequest("GET", "http://example.com/test?user_id=user1", nil)
		w1 := httptest.NewRecorder()
		handler.ServeHTTP(w1, req1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// Second request for user1 should be limited
		req2 := httptest.NewRequest("GET", "http://example.com/test?user_id=user1", nil)
		w2 := httptest.NewRecorder()
		handler.ServeHTTP(w2, req2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)

		// Different user should have separate limit
		req3 := httptest.NewRequest("GET", "http://example.com/test?user_id=user2", nil)
		w3 := httptest.NewRecorder()
		handler.ServeHTTP(w3, req3)
		assert.Equal(t, http.StatusOK, w3.Code)
	})

	t.Run("default configuration", func(t *testing.T) {
		middleware := CreateRateLimitMiddleware(map[string]string{})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("burst allows temporary spike", func(t *testing.T) {
		middleware := CreateRateLimitMiddleware(map[string]string{
			OptionRequests: "10",
			OptionWindow:   "1m",
			OptionBurst:    "5",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"

		// Burst of 5 should all succeed
		for i := 0; i < 5; i++ {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "burst request %d should succeed", i+1)
		}

		// 6th request exceeds burst
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusTooManyRequests, w.Code)
	})

	t.Run("missing key returns error", func(t *testing.T) {
		middleware := CreateRateLimitMiddleware(map[string]string{
			OptionKey: "header:X-Missing",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Rate limit key extraction failed")
	})

	t.Run("invalid window format defaults to 1m", func(t *testing.T) {
		middleware := CreateRateLimitMiddleware(map[string]string{
			OptionWindow: "invalid",
		})

		// Should not panic and use default
		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("concurrent requests respect rate limit", func(t *testing.T) {
		middleware := CreateRateLimitMiddleware(map[string]string{
			OptionRequests: "10",
			OptionWindow:   "1m",
			OptionBurst:    "10",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		var wg sync.WaitGroup
		successCount := 0
		var mu sync.Mutex

		// Send 20 concurrent requests
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				req := httptest.NewRequest("GET", "http://example.com/test", nil)
				req.RemoteAddr = "192.168.1.1:12345"
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)

				mu.Lock()
				if w.Code == http.StatusOK {
					successCount++
				}
				mu.Unlock()
			}()
		}

		wg.Wait()

		// Only 10 should succeed (burst limit)
		assert.LessOrEqual(t, successCount, 10)
	})

	t.Run("response headers include rate limit info", func(t *testing.T) {
		middleware := CreateRateLimitMiddleware(map[string]string{
			OptionRequests: "5",
			OptionWindow:   "30s",
			OptionBurst:    "5",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"

		// Exhaust limit
		for i := 0; i < 5; i++ {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		}

		// Next request should be rate limited with headers
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusTooManyRequests, w.Code)
		assert.Equal(t, "5", w.Header().Get("X-RateLimit-Limit"))
		assert.Equal(t, "30s", w.Header().Get("X-RateLimit-Window"))
	})
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"seconds", "30s", 30 * time.Second, false},
		{"minutes", "5m", 5 * time.Minute, false},
		{"hours", "2h", 2 * time.Hour, false},
		{"invalid unit", "10x", 0, true},
		{"no unit", "10", 0, true},
		{"empty", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDuration(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestLimiterStoreCleanup(t *testing.T) {
	t.Run("cleanup removes expired entries", func(t *testing.T) {
		store := newLimiterStore(rate.Limit(1), 1, 50*time.Millisecond, 100*time.Millisecond, 1000)
		defer store.stop()

		// Add some limiters
		store.getLimiter("key1")
		store.getLimiter("key2")

		assert.Equal(t, 2, store.len())

		// Wait for TTL + cleanup interval
		time.Sleep(200 * time.Millisecond)

		assert.Equal(t, 0, store.len())
	})

	t.Run("active entries not cleaned up", func(t *testing.T) {
		store := newLimiterStore(rate.Limit(1), 1, 50*time.Millisecond, 100*time.Millisecond, 1000)
		defer store.stop()

		// Add and keep accessing key1
		store.getLimiter("key1")

		for i := 0; i < 5; i++ {
			time.Sleep(30 * time.Millisecond)
			store.getLimiter("key1") // keep it alive
		}

		// key1 should still exist since we kept accessing it
		assert.Equal(t, 1, store.len())
	})

	t.Run("max entries enforced with eviction", func(t *testing.T) {
		store := newLimiterStore(rate.Limit(1), 1, time.Hour, time.Hour, 3)
		defer store.stop()

		store.getLimiter("key1")
		time.Sleep(10 * time.Millisecond)
		store.getLimiter("key2")
		time.Sleep(10 * time.Millisecond)
		store.getLimiter("key3")

		assert.Equal(t, 3, store.len())

		// Adding 4th should evict oldest (key1)
		store.getLimiter("key4")
		assert.Equal(t, 3, store.len())
	})

	t.Run("stop prevents goroutine leak", func(_ *testing.T) {
		store := newLimiterStore(rate.Limit(1), 1, time.Millisecond, time.Millisecond, 1000)
		store.stop()
		store.stop() // double stop should be safe
	})
}

func TestExtractKey(t *testing.T) {
	t.Run("extract IP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"

		key := extractKey(req, "ip")
		assert.Equal(t, "192.168.1.1", key)
	})

	t.Run("extract from header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.Header.Set("X-API-Key", "secret123")

		key := extractKey(req, "header:X-API-Key")
		assert.Equal(t, "secret123", key)
	})

	t.Run("extract from query", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/test?token=abc123", nil)

		key := extractKey(req, "query:token")
		assert.Equal(t, "abc123", key)
	})

	t.Run("invalid strategy defaults to IP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"

		key := extractKey(req, "invalid")
		assert.Equal(t, "192.168.1.1", key)
	})

	t.Run("missing header returns empty", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/test", nil)

		key := extractKey(req, "header:X-Missing")
		assert.Equal(t, "", key)
	})

	t.Run("header without name returns empty", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/test", nil)

		key := extractKey(req, "header")
		assert.Equal(t, "", key)
	})
}
