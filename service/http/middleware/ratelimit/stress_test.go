package ratelimit

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestStress_HighConcurrency(t *testing.T) {
	middleware := CreateRateLimitMiddleware(map[string]string{
		OptionRequests:   "1000",
		OptionWindow:     "1s",
		OptionBurst:      "100",
		OptionMaxEntries: "10000",
	})

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var wg sync.WaitGroup
	var successCount, limitedCount atomic.Int64
	numGoroutines := 100
	requestsPerGoroutine := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				req := httptest.NewRequest("GET", "http://example.com/test", nil)
				req.RemoteAddr = fmt.Sprintf("192.168.1.%d:12345", goroutineID%256)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)

				if w.Code == http.StatusOK {
					successCount.Add(1)
				} else if w.Code == http.StatusTooManyRequests {
					limitedCount.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()

	totalRequests := int64(numGoroutines * requestsPerGoroutine)
	assert.Equal(t, totalRequests, successCount.Load()+limitedCount.Load(),
		"all requests should be accounted for")
	t.Logf("Total: %d, Success: %d, Limited: %d",
		totalRequests, successCount.Load(), limitedCount.Load())
}

func TestStress_ManyUniqueKeys(t *testing.T) {
	middleware := CreateRateLimitMiddleware(map[string]string{
		OptionRequests:   "10",
		OptionWindow:     "1s",
		OptionBurst:      "10",
		OptionMaxEntries: "1000",
	})

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var wg sync.WaitGroup
	numUniqueIPs := 2000

	for i := 0; i < numUniqueIPs; i++ {
		wg.Add(1)
		go func(ipNum int) {
			defer wg.Done()
			req := httptest.NewRequest("GET", "http://example.com/test", nil)
			req.RemoteAddr = fmt.Sprintf("10.%d.%d.%d:12345",
				(ipNum/65536)%256, (ipNum/256)%256, ipNum%256)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		}(i)
	}

	assert.NotPanics(t, func() {
		wg.Wait()
	})
}

func TestStress_EvictionUnderPressure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := newLimiterStore(ctx, rate.Limit(1), 1, time.Hour, time.Hour, 100)

	var wg sync.WaitGroup
	numGoroutines := 50
	keysPerGoroutine := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < keysPerGoroutine; j++ {
				key := fmt.Sprintf("key-%d-%d", goroutineID, j)
				store.getLimiter(key)
			}
		}(i)
	}

	wg.Wait()

	assert.LessOrEqual(t, store.len(), 100, "store should not exceed max entries")
}

func TestSecurity_KeyInjection(t *testing.T) {
	testCases := []struct {
		name      string
		headerKey string
		value     string
	}{
		{"null byte injection", "X-API-Key", "valid\x00injected"},
		{"newline injection", "X-API-Key", "valid\ninjected"},
		{"very long key", "X-API-Key", string(make([]byte, 10000))},
		{"empty key", "X-API-Key", ""},
		{"unicode", "X-API-Key", "키\u0000\u200b"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			middleware := CreateRateLimitMiddleware(map[string]string{
				OptionKey: "header:X-API-Key",
			})

			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "http://example.com/test", nil)
			req.Header.Set(tc.headerKey, tc.value)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if tc.value == "" {
				assert.Equal(t, http.StatusBadRequest, w.Code,
					"empty key should fail")
			}
		})
	}
}

func TestSecurity_IPSpoofing(t *testing.T) {
	middleware := CreateRateLimitMiddleware(map[string]string{
		OptionRequests: "1",
		OptionBurst:    "1",
	})

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req1 := httptest.NewRequest("GET", "http://example.com/test", nil)
	req1.RemoteAddr = "192.168.1.1:12345"
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusOK, w1.Code)

	req2 := httptest.NewRequest("GET", "http://example.com/test", nil)
	req2.RemoteAddr = "192.168.1.1:12345"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusTooManyRequests, w2.Code)
}

func TestEdge_ZeroValues(t *testing.T) {
	t.Run("zero requests", func(t *testing.T) {
		middleware := CreateRateLimitMiddleware(map[string]string{
			OptionRequests: "0",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("negative values", func(t *testing.T) {
		middleware := CreateRateLimitMiddleware(map[string]string{
			OptionRequests: "-100",
			OptionBurst:    "-10",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestEdge_MalformedRemoteAddr(t *testing.T) {
	testCases := []string{
		"",
		"invalid",
		"192.168.1.1",
		"[::1]",
		"[::1]:80",
		"192.168.1.1:abc",
		":::12345",
	}

	for _, addr := range testCases {
		t.Run(fmt.Sprintf("addr=%s", addr), func(t *testing.T) {
			middleware := CreateRateLimitMiddleware(map[string]string{})

			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "http://example.com/test", nil)
			req.RemoteAddr = addr
			w := httptest.NewRecorder()

			require.NotPanics(t, func() {
				handler.ServeHTTP(w, req)
			})
		})
	}
}

func TestEdge_InvalidStrategy(t *testing.T) {
	testCases := []string{
		"header",
		"query",
		"header:",
		"query:",
		"cookie:session",
		"body:field",
	}

	for _, strategy := range testCases {
		t.Run(fmt.Sprintf("strategy=%s", strategy), func(t *testing.T) {
			middleware := CreateRateLimitMiddleware(map[string]string{
				OptionKey: strategy,
			})

			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "http://example.com/test", nil)
			req.RemoteAddr = "192.168.1.1:12345"
			w := httptest.NewRecorder()

			require.NotPanics(t, func() {
				handler.ServeHTTP(w, req)
			})
		})
	}
}

func TestMemory_NoLeaks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory test in short mode")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := newLimiterStore(ctx, rate.Limit(100), 10, 10*time.Millisecond, 50*time.Millisecond, 1000)

	var startMem, endMem runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&startMem)

	for i := 0; i < 10000; i++ {
		key := fmt.Sprintf("key-%d", i)
		store.getLimiter(key)
	}

	time.Sleep(200 * time.Millisecond)

	runtime.GC()
	runtime.ReadMemStats(&endMem)

	assert.LessOrEqual(t, store.len(), 1000,
		"store should have cleaned up entries")

	memDiff := int64(endMem.HeapAlloc) - int64(startMem.HeapAlloc) //nolint:gosec
	t.Logf("Memory diff: %d bytes, Store len: %d", memDiff, store.len())
}

func TestRace_CleanupDuringAccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := newLimiterStore(ctx, rate.Limit(1), 1, 1*time.Millisecond, 5*time.Millisecond, 100)

	var wg sync.WaitGroup
	done := make(chan struct{})

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					key := fmt.Sprintf("key-%d-%d", id, time.Now().UnixNano())
					limiter := store.getLimiter(key)
					limiter.Allow()
				}
			}
		}(i)
	}

	time.Sleep(100 * time.Millisecond)
	close(done)
	assert.NotPanics(t, func() {
		wg.Wait()
	})
}

func TestRace_ConcurrentSameKey(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := newLimiterStore(ctx, rate.Limit(1000), 100, time.Hour, time.Hour, 1000)

	var wg sync.WaitGroup
	key := "shared-key"

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				limiter := store.getLimiter(key)
				limiter.Allow()
			}
		}()
	}

	wg.Wait()
	assert.Equal(t, 1, store.len(), "should only have one entry for shared key")
}

func BenchmarkGetLimiter_SameKey(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := newLimiterStore(ctx, rate.Limit(1000), 100, time.Hour, time.Hour, 10000)

	key := "benchmark-key"
	store.getLimiter(key)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			store.getLimiter(key)
		}
	})
}

func BenchmarkGetLimiter_UniqueKeys(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := newLimiterStore(ctx, rate.Limit(1000), 100, time.Hour, time.Hour, 100000)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("key-%d", i)
			store.getLimiter(key)
			i++
		}
	})
}

func BenchmarkMiddleware(b *testing.B) {
	middleware := CreateRateLimitMiddleware(map[string]string{
		OptionRequests:   "10000",
		OptionBurst:      "1000",
		OptionMaxEntries: "100000",
	})

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			req := httptest.NewRequest("GET", "http://example.com/test", nil)
			req.RemoteAddr = fmt.Sprintf("192.168.%d.%d:12345", (i/256)%256, i%256)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			i++
		}
	})
}
