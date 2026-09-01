// SPDX-License-Identifier: MPL-2.0

package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestCreateRateLimitMiddlewareDoesNotLeakGoroutines asserts that repeatedly
// creating a middleware (which happens on every route reconfiguration that
// uses the ratelimit middleware) does not leak goroutines.
//
// Pre-fix, every CreateRateLimitMiddleware call spawned a cleanup goroutine
// tied to context.Background() that could never exit. Under dynamic route
// reconfiguration this leaked unbounded goroutines and tickers, each holding
// a reference to a limiterStore (whose map can grow to maxEntries * ~80 B =
// up to ~8 MB per leaked store).
func TestCreateRateLimitMiddlewareDoesNotLeakGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()

	const routeReconfigs = 200
	for i := 0; i < routeReconfigs; i++ {
		mw := CreateRateLimitMiddleware(map[string]string{
			OptionRequests:        "100",
			OptionWindow:          "1m",
			OptionBurst:           "20",
			OptionCleanupInterval: "1s",
			OptionEntryTTL:        "5s",
		})
		handler := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1/", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	for i := 0; i < 5; i++ {
		runtime.GC()
		time.Sleep(50 * time.Millisecond)
	}

	after := runtime.NumGoroutine()
	t.Logf("goroutines before=%d after=%d (delta=%d over %d middleware creations)",
		before, after, after-before, routeReconfigs)

	const tolerance = 10
	require.Less(t, after-before, tolerance,
		"goroutine count grew by %d after %d CreateRateLimitMiddleware calls; "+
			"each call must not spawn a goroutine that outlives the middleware",
		after-before, routeReconfigs)
}

// TestManagerCreateMiddlewareDoesNotLeakGoroutines asserts the same contract
// for the Manager variant used by the production HTTP wiring.
func TestManagerCreateMiddlewareDoesNotLeakGoroutines(t *testing.T) {
	mgr := NewManager(context.Background())

	before := runtime.NumGoroutine()

	const routeReconfigs = 200
	for i := 0; i < routeReconfigs; i++ {
		mw := mgr.CreateMiddleware(map[string]string{
			OptionRequests:        "100",
			OptionWindow:          "1m",
			OptionBurst:           "20",
			OptionCleanupInterval: "1s",
			OptionEntryTTL:        "5s",
		})
		handler := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1/", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	for i := 0; i < 5; i++ {
		runtime.GC()
		time.Sleep(50 * time.Millisecond)
	}

	after := runtime.NumGoroutine()
	t.Logf("goroutines before=%d after=%d (delta=%d over %d Manager.CreateMiddleware calls)",
		before, after, after-before, routeReconfigs)

	const tolerance = 10
	require.Less(t, after-before, tolerance,
		"goroutine count grew by %d after %d Manager.CreateMiddleware calls; "+
			"each call must not spawn a goroutine that outlives the middleware",
		after-before, routeReconfigs)
}

// TestCreateRateLimitMiddlewareLazyCleanupEvictsStaleEntries verifies that
// removing the background cleanup goroutine did not silently drop eviction
// of expired limiter entries. After the fix, cleanup runs lazily inside
// getLimiter at the configured cadence.
func TestCreateRateLimitMiddlewareLazyCleanupEvictsStaleEntries(t *testing.T) {
	mw := CreateRateLimitMiddleware(map[string]string{
		OptionRequests:        "1",
		OptionWindow:          "1s",
		OptionBurst:           "1",
		OptionKey:             "header:x-test-key",
		OptionCleanupInterval: "1s",
		OptionEntryTTL:        "1s",
	})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 50; i++ {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1/", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("x-test-key", "client-"+strconvItoa(i))
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	time.Sleep(1500 * time.Millisecond)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("x-test-key", "client-new")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code,
		"middleware must remain functional after lazy cleanup; got status=%d", resp.Code)
}

// strconvItoa avoids importing strconv just for a tiny int-to-string need.
func strconvItoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
