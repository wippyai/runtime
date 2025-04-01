package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ponyruntime/pony/api/registry"
	config "github.com/ponyruntime/pony/api/service/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouteManager_BasicOperations(t *testing.T) {
	rm := NewRouteManager()

	t.Run("add and update router", func(t *testing.T) {
		routerID := registry.ID{NS: "test", Name: "router1"}

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
		routerID := registry.ID{NS: "test", Name: "router1"}
		funcID := registry.ID{NS: "test", Name: "func1"}
		endpointID := registry.ID{NS: "test", Name: "endpoint1"}

		// Add route to router
		err := rm.AddRoute(routerID, endpointID, "GET", "/test", funcID, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		require.NoError(t, err)

		// Try adding duplicate route Source
		err = rm.AddRoute(routerID, endpointID, "POST", "/test2", funcID, nil)
		assert.Error(t, err)

		// Try adding route with same path and method
		endpointID2 := registry.ID{NS: "test", Name: "endpoint2"}
		err = rm.AddRoute(routerID, endpointID2, "GET", "/test", funcID, nil)
		assert.Error(t, err)

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

		// Unmount
		err = rm.Unmount("/static")
		require.NoError(t, err)

		// Try unmounting non-existent path
		err = rm.Unmount("/static")
		assert.Error(t, err)
	})

	t.Run("remove router", func(t *testing.T) {
		routerID := registry.ID{NS: "test", Name: "router1"}
		err := rm.RemoveRouter(routerID)
		require.NoError(t, err)

		// Try removing non-existent router
		err = rm.RemoveRouter(routerID)
		assert.Error(t, err)
	})
}

func TestRouteManager_ServeHTTP(t *testing.T) {
	rm := NewRouteManager()

	// Add test router
	routerID := registry.ID{NS: "test", Name: "router1"}
	err := rm.AddRouter(routerID, "/api", nil, nil)
	require.NoError(t, err)

	// Add test endpoint
	funcID := registry.ID{NS: "test", Name: "func1"}
	endpointID := registry.ID{NS: "test", Name: "endpoint1"}

	// Create a handler that checks for request context values
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for RouteInfo
		routeInfo := r.Context().Value(config.RouteCtx).(*config.RouteInfo)
		assert.Equal(t, funcID, routeInfo.Func)

		// Check params
		assert.Equal(t, "123", routeInfo.Params["id"])

		w.WriteHeader(http.StatusOK)
	})

	err = rm.AddRoute(routerID, endpointID, "GET", "/users/{id}", funcID, handler)
	require.NoError(t, err)

	// Build the router
	rm.Build()

	// Create a test server
	server := httptest.NewServer(rm)
	resp, err := http.Get(server.URL + "/api/users/123")
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NoError(t, resp.Body.Close())

	// Test 404 for non-existent route
	resp, err = http.Get(server.URL + "/api/nonexistent")
	require.NoError(t, err)

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.NoError(t, resp.Body.Close())

	server.Close()
}

func TestRouteManager_MultipleRouters(t *testing.T) {
	rm := NewRouteManager()

	// Add multiple test routers
	routerIDs := []struct {
		id     registry.ID
		prefix string
		name   string
	}{
		{registry.ID{NS: "test", Name: "router1"}, "/api/v1", "router1"},
		{registry.ID{NS: "test", Name: "router2"}, "/api/v2", "router2"},
	}

	for _, r := range routerIDs {
		err := rm.AddRouter(r.id, r.prefix, nil, nil)
		require.NoError(t, err)

		// Add a test endpoint to each router
		funcID := registry.ID{NS: "test", Name: "func1"}
		endpointID := registry.ID{NS: "test", Name: r.id.Name + "-endpoint"}

		// Save router name in a closure variable to avoid sharing across iterations
		routerName := r.name
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Router", routerName)
			w.WriteHeader(http.StatusOK)
		})

		err = rm.AddRoute(r.id, endpointID, "GET", "/test", funcID, handler)
		require.NoError(t, err)
	}

	// Build the router
	rm.Build()

	// Create a test server
	server := httptest.NewServer(rm)
	defer server.Close()

	// Test each router's endpoint
	for _, r := range routerIDs {
		resp, err := http.Get(server.URL + r.prefix + "/test")
		require.NoError(t, err)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, r.name, resp.Header.Get("X-Router"))
		assert.NoError(t, resp.Body.Close())
	}
}

func TestRouteManager_Middleware(t *testing.T) {
	rm := NewRouteManager()

	// Create middleware that adds a header
	testMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Test-Middleware", "applied")
			next.ServeHTTP(w, r)
		})
	}

	// Add router with middleware
	routerID := registry.ID{NS: "test", Name: "router1"}
	middleware := []func(http.Handler) http.Handler{testMiddleware}
	err := rm.AddRouter(routerID, "/api", middleware, nil)
	require.NoError(t, err)

	// Add test endpoint
	funcID := registry.ID{NS: "test", Name: "func1"}
	endpointID := registry.ID{NS: "test", Name: "endpoint1"}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	err = rm.AddRoute(routerID, endpointID, "GET", "/test", funcID, handler)
	require.NoError(t, err)

	// Build the router
	rm.Build()

	// Create a test server
	server := httptest.NewServer(rm)
	defer server.Close()

	// Test middleware application
	resp, err := http.Get(server.URL + "/api/test")
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "applied", resp.Header.Get("X-Test-Middleware"))
	assert.NoError(t, resp.Body.Close())
}
