// SPDX-License-Identifier: MPL-2.0

package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	contextapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/http"
)

func TestRouteManager_BasicOperations(t *testing.T) {
	rm, err := NewRouteManager()
	require.NoError(t, err)

	t.Run("add and update router", func(t *testing.T) {
		routerID := registry.NewID("test", "router1")

		// Add initial router
		err := rm.AddRouter(routerID, "/api/v1", nil, nil)
		require.NoError(t, err)

		// Update existing router with new prefix - should not error
		err = rm.AddRouter(routerID, "/api/v2", nil, nil)
		require.NoError(t, err)

		// We could add verification here to check the router was updated
		// but that would require adding a getter method to the RouteManager
	})

	t.Run("add and remove route", func(t *testing.T) {
		routerID := registry.NewID("test", "router1")
		funcID := registry.NewID("test", "func1")
		endpointID := registry.NewID("test", "endpoint1")

		// Add route to router
		err := rm.AddRoute(routerID, endpointID, "GET", "/test", funcID, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		require.NoError(t, err)

		// Update existing route (upsert behavior)
		err = rm.AddRoute(routerID, endpointID, "POST", "/test2", funcID, nil)
		assert.NoError(t, err)

		// Add different endpoint with same path and method is allowed
		endpointID2 := registry.NewID("test", "endpoint2")
		err = rm.AddRoute(routerID, endpointID2, "GET", "/test", funcID, nil)
		assert.NoError(t, err)

		// Done route
		err = rm.RemoveRoute(routerID, endpointID)
		require.NoError(t, err)

		// Try removing non-existent route
		err = rm.RemoveRoute(routerID, endpointID)
		assert.Error(t, err)
	})

	t.Run("mount and unmount handler", func(t *testing.T) {
		err := rm.Mount("/static", http.FileServer(http.Dir(".")))
		require.NoError(t, err)

		// Try mounting to same path
		err = rm.Mount("/static", http.FileServer(http.Dir(".")))
		assert.Error(t, err)

		// Replace existing mount in place
		replacement := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("replacement"))
		})
		err = rm.ReplaceMount("/static", replacement)
		require.NoError(t, err)
		rec := httptest.NewRecorder()
		rm.mounts["/static"].ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/static/app.js", nil))
		assert.Equal(t, "replacement", rec.Body.String())

		// Replace validates paths and only updates existing mounts
		err = rm.ReplaceMount("", replacement)
		assert.Error(t, err)
		err = rm.ReplaceMount("static", replacement)
		assert.Error(t, err)
		err = rm.ReplaceMount("/missing", replacement)
		assert.Error(t, err)

		// Unmount
		err = rm.Unmount("/static")
		require.NoError(t, err)

		// Try unmounting non-existent path
		err = rm.Unmount("/static")
		assert.Error(t, err)
	})

	t.Run("remove router", func(t *testing.T) {
		routerID := registry.NewID("test", "router1")
		err := rm.RemoveRouter(routerID)
		require.NoError(t, err)

		// Try removing non-existent router
		err = rm.RemoveRouter(routerID)
		assert.Error(t, err)
	})
}

func TestRouteManager_ServeHTTP(t *testing.T) {
	rm, err := NewRouteManager()
	require.NoError(t, err)

	// Add test router
	routerID := registry.NewID("test", "router1")
	err = rm.AddRouter(routerID, "/api", nil, nil)
	require.NoError(t, err)

	// Add test endpoint
	funcID := registry.NewID("test", "func1")
	endpointID := registry.NewID("test", "endpoint1")

	// Create a handler that checks for request context values
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for RouteInfo from FrameContext
		routeInfo, ok := config.GetRouteInfo(r.Context())
		require.True(t, ok, "RouteInfo should be set")
		assert.Equal(t, funcID, routeInfo.Func)

		// Check params
		assert.Equal(t, "123", routeInfo.Params["id"])

		w.WriteHeader(http.StatusOK)
	})

	err = rm.AddRoute(routerID, endpointID, "GET", "/users/{id}", funcID, handler)
	require.NoError(t, err)

	// Build the router
	err = rm.Build()
	require.NoError(t, err)

	// Wrap router with FrameContext creation (like HTTP server does)
	wrappedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, _ := contextapi.OpenFrameContext(r.Context())
		rm.ServeHTTP(w, r.WithContext(ctx))
	})

	// Create a test server
	server := httptest.NewServer(wrappedHandler)
	resp, err := testGet(t, server.URL+"/api/users/123")
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NoError(t, resp.Body.Close())

	// Test 404 for non-existent route
	resp, err = testGet(t, server.URL+"/api/nonexistent")
	require.NoError(t, err)

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.NoError(t, resp.Body.Close())

	server.Close()
}

func TestRouteManager_MethodAgnosticCatchAllCoexistsWithStaticMount(t *testing.T) {
	rm, err := NewRouteManager()
	require.NoError(t, err)

	routerID := registry.NewID("test", "router")
	require.NoError(t, rm.AddRouter(routerID, "", nil, nil))
	require.NoError(t, rm.AddRoute(
		routerID,
		registry.NewID("test", "fallback"),
		config.MethodAny,
		"/{path...}",
		registry.NewID("test", "fallback_handler"),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("dynamic:" + r.Method))
		}),
	))
	require.NoError(t, rm.Mount("/app", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("static"))
	})))
	require.NoError(t, rm.Build())

	tests := []struct {
		name     string
		method   string
		path     string
		expected string
	}{
		{name: "GET deep link", method: http.MethodGet, path: "/home", expected: "dynamic:GET"},
		{name: "POST deep link", method: http.MethodPost, path: "/home", expected: "dynamic:POST"},
		{name: "OPTIONS deep link", method: http.MethodOptions, path: "/home", expected: "dynamic:OPTIONS"},
		{name: "extension method deep link", method: "PROPFIND", path: "/home", expected: "dynamic:PROPFIND"},
		{name: "GET static asset", method: http.MethodGet, path: "/app/main.js", expected: "static"},
		{name: "POST static asset", method: http.MethodPost, path: "/app/main.js", expected: "static"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(context.Background(), tt.method, tt.path, nil)
			rm.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, tt.expected, rec.Body.String())
		})
	}
}

func TestRouteManager_BuildReturnsMethodAgnosticConflicts(t *testing.T) {
	rm, err := NewRouteManager()
	require.NoError(t, err)

	routerID := registry.NewID("test", "router")
	require.NoError(t, rm.AddRouter(routerID, "", nil, nil))
	require.NoError(t, rm.AddRoute(
		routerID,
		registry.NewID("test", "index"),
		config.MethodAny,
		"/index.html",
		registry.NewID("test", "index_handler"),
		http.NotFoundHandler(),
	))
	require.NoError(t, rm.AddRoute(
		routerID,
		registry.NewID("test", "get_fallback"),
		http.MethodGet,
		"/{path...}",
		registry.NewID("test", "fallback_handler"),
		http.NotFoundHandler(),
	))

	var buildErr error
	require.NotPanics(t, func() {
		buildErr = rm.Build()
	})
	require.Error(t, buildErr)

	var apiErr apierror.Error
	require.ErrorAs(t, buildErr, &apiErr)
	assert.Equal(t, apierror.Conflict, apiErr.Kind())
}

func TestRouteManager_MultipleRouters(t *testing.T) {
	rm, err := NewRouteManager()
	require.NoError(t, err)

	// Add multiple test routers
	routerIDs := []struct {
		id     registry.ID
		prefix string
		name   string
	}{
		{registry.NewID("test", "router1"), "/api/v1", "router1"},
		{registry.NewID("test", "router2"), "/api/v2", "router2"},
	}

	for _, r := range routerIDs {
		err := rm.AddRouter(r.id, r.prefix, nil, nil)
		require.NoError(t, err)

		// Add a test endpoint to each router
		funcID := registry.NewID("test", "func1")
		endpointID := registry.ID{NS: "test", Name: r.id.Name + "-endpoint"}

		// Save router name in a closure variable to avoid sharing across iterations
		routerName := r.name
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Router", routerName)
			w.WriteHeader(http.StatusOK)
		})

		err = rm.AddRoute(r.id, endpointID, "GET", "/test", funcID, handler)
		require.NoError(t, err)
	}

	// Build the router
	err = rm.Build()
	require.NoError(t, err)

	// Wrap router with FrameContext creation (like HTTP server does)
	wrappedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, _ := contextapi.OpenFrameContext(r.Context())
		rm.ServeHTTP(w, r.WithContext(ctx))
	})

	// Create a test server
	server := httptest.NewServer(wrappedHandler)
	defer server.Close()

	// Test each router's endpoint
	for _, r := range routerIDs {
		resp, err := testGet(t, server.URL+r.prefix+"/test")
		require.NoError(t, err)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, r.name, resp.Header.Get("X-Router"))
		assert.NoError(t, resp.Body.Close())
	}
}

func TestRouteManager_Middleware(t *testing.T) {
	rm, err := NewRouteManager()
	require.NoError(t, err)

	// Create middleware that adds a header
	testMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Test-Middleware", "applied")
			next.ServeHTTP(w, r)
		})
	}

	// Add router with middleware
	routerID := registry.NewID("test", "router1")
	middleware := []func(http.Handler) http.Handler{testMiddleware}
	err = rm.AddRouter(routerID, "/api", middleware, nil)
	require.NoError(t, err)

	// Add test endpoint
	funcID := registry.NewID("test", "func1")
	endpointID := registry.NewID("test", "endpoint1")

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	err = rm.AddRoute(routerID, endpointID, "GET", "/test", funcID, handler)
	require.NoError(t, err)

	// Build the router
	err = rm.Build()
	require.NoError(t, err)

	// Wrap router with FrameContext creation (like HTTP server does)
	wrappedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, _ := contextapi.OpenFrameContext(r.Context())
		rm.ServeHTTP(w, r.WithContext(ctx))
	})

	// Create a test server
	server := httptest.NewServer(wrappedHandler)
	defer server.Close()

	// Test middleware application
	//nolint:noctx // noctx is not needed because we are not reading the body
	resp, err := http.Get(server.URL + "/api/test")
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "applied", resp.Header.Get("X-Test-Middleware"))
	assert.NoError(t, resp.Body.Close())
}

func TestRouteManager_OptionsRouteLabelVisibleToMiddleware(t *testing.T) {
	rm, err := NewRouteManager()
	require.NoError(t, err)

	var observedLabel string
	testMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if label, ok := config.GetRouteLabel(r.Context()); ok {
				observedLabel = label
			}
			next.ServeHTTP(w, r)
		})
	}

	routerID := registry.NewID("test", "router1")
	err = rm.AddRouter(routerID, "/api", []func(http.Handler) http.Handler{testMiddleware}, nil)
	require.NoError(t, err)

	funcID := registry.NewID("test", "func1")
	endpointID := registry.NewID("test", "endpoint1")
	err = rm.AddRoute(routerID, endpointID, http.MethodGet, "/users/{id}", funcID, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	require.NoError(t, err)
	require.NoError(t, rm.Build())

	wrappedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, _ := contextapi.OpenFrameContext(r.Context())
		rm.ServeHTTP(w, r.WithContext(ctx))
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodOptions, "/api/users/123", nil)
	wrappedHandler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, funcID.String(), observedLabel)
}

func TestRouteManager_RouteUpdates(t *testing.T) {
	rm, err := NewRouteManager()
	require.NoError(t, err)

	// Add router
	routerID := registry.NewID("test", "router1")
	err = rm.AddRouter(routerID, "/api", nil, nil)
	require.NoError(t, err)

	// Add first test endpoint
	funcID1 := registry.NewID("test", "func1")
	endpointID1 := registry.NewID("test", "endpoint1")
	handler1 := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Add first route
	err = rm.AddRoute(routerID, endpointID1, "GET", "/test", funcID1, handler1)
	require.NoError(t, err)

	// Update route (upsert behavior - should succeed)
	err = rm.AddRoute(routerID, endpointID1, "POST", "/test2", funcID1, handler1)
	require.NoError(t, err)

	// Try to add duplicate router prefix - should still error
	routerID2 := registry.NewID("test", "router2")
	err = rm.AddRouter(routerID2, "/api", nil, nil)
	require.Error(t, err)
	var apiErr apierror.Error
	ok := errors.As(err, &apiErr)
	require.True(t, ok)
	assert.Contains(t, apiErr.Error(), "router prefix already exists")
	assert.Equal(t, "/api", apiErr.Details().GetString("prefix", ""))

	err = rm.Build()
	assert.NoError(t, err)
}

type benchmarkResponseWriter struct {
	header http.Header
}

func (w *benchmarkResponseWriter) Header() http.Header {
	return w.header
}

func (w *benchmarkResponseWriter) Write(body []byte) (int, error) {
	return len(body), nil
}

func (w *benchmarkResponseWriter) WriteHeader(int) {}

func BenchmarkRouteManagerServeHTTPConcreteCatchAll(b *testing.B) {
	benchmarkRouteManagerServeHTTPCatchAll(b, http.MethodGet, false)
}

func BenchmarkRouteManagerServeHTTPMethodAgnosticCatchAll(b *testing.B) {
	benchmarkRouteManagerServeHTTPCatchAll(b, config.MethodAny, true)
}

func benchmarkRouteManagerServeHTTPCatchAll(b *testing.B, method string, withStaticMount bool) {
	rm, err := NewRouteManager()
	require.NoError(b, err)

	routerID := registry.NewID("benchmark", "router")
	require.NoError(b, rm.AddRouter(routerID, "", nil, nil))
	require.NoError(b, rm.AddRoute(
		routerID,
		registry.NewID("benchmark", "endpoint"),
		method,
		"/{path...}",
		registry.NewID("benchmark", "handler"),
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	))
	if withStaticMount {
		require.NoError(b, rm.Mount("/app", http.NotFoundHandler()))
	}
	require.NoError(b, rm.Build())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/deep/link", nil)
	w := benchmarkResponseWriter{header: make(http.Header)}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		rm.ServeHTTP(&w, req)
	}
}

func BenchmarkRouteManagerBuild(b *testing.B) {
	rm, err := NewRouteManager()
	require.NoError(b, err)

	routerID := registry.NewID("benchmark", "router")
	require.NoError(b, rm.AddRouter(routerID, "", nil, nil))
	for i := range 100 {
		require.NoError(b, rm.AddRoute(
			routerID,
			registry.NewID("benchmark", fmt.Sprintf("endpoint_%d", i)),
			http.MethodGet,
			fmt.Sprintf("/routes/%d/{id}", i),
			registry.NewID("benchmark", "handler"),
			http.NotFoundHandler(),
		))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := rm.Build(); err != nil {
			b.Fatal(err)
		}
	}
}

func testGet(t *testing.T, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}
