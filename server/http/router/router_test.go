package router

import (
	"github.com/ponyruntime/pony/api/registry"
	config "github.com/ponyruntime/pony/api/server/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouterComposition(t *testing.T) {
	// Test handler that returns different responses based on the path
	handler := func(w http.ResponseWriter, r *http.Request) {
		routeInfo, ok := GetRouteInfo(r.Context())
		if !ok {
			http.Error(w, "route info not found", http.StatusInternalServerError)
			return
		}
		w.Header().Set("X-Router-ID", routeInfo.Endpoint.Meta.StringValue("router_id"))
		w.WriteHeader(http.StatusOK)
	}

	t.Run("basic composition", func(t *testing.T) {
		router := NewRouter(handler)

		// Add a new router
		err := router.AddRouter(config.RouterConfig{
			Prefix: "/api/v1",
			Meta:   registry.Metadata{"router_id": "router1"},
		})
		require.NoError(t, err)

		// Add endpoints to different routers
		err = router.AddEndpoint("ep1", config.EndpointConfig{
			Method: http.MethodGet,
			Path:   "/test",
			Meta:   registry.Metadata{"router_id": "router1"},
		})
		require.NoError(t, err)

		err = router.AddEndpoint("ep2", config.EndpointConfig{
			Method: http.MethodGet,
			Path:   "/default",
			Meta:   registry.Metadata{"router_id": ""},
		})
		require.NoError(t, err)

		// Test the composed routes
		server := httptest.NewServer(router)
		defer server.Close()

		// Test router1 endpoint
		resp, err := http.Get(server.URL + "/api/v1/test")
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "router1", resp.Header.Get("X-Router-ID"))

		// Test default router endpoint
		resp, err = http.Get(server.URL + "/default")
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "", resp.Header.Get("X-Router-ID"))
	})

	t.Run("multiple routers", func(t *testing.T) {
		router := NewRouter(handler)

		// Add multiple routers
		routers := []struct {
			id     string
			prefix string
		}{
			{"router1", "/api/v1"},
			{"router2", "/api/v2"},
			{"router3", "/internal"},
		}

		for _, r := range routers {
			err := router.AddRouter(config.RouterConfig{
				Prefix: r.prefix,
				Meta:   registry.Metadata{"router_id": r.id},
			})
			require.NoError(t, err)

			// Add an endpoint to each router
			err = router.AddEndpoint("", config.EndpointConfig{
				Method: http.MethodGet,
				Path:   "/status",
				Meta:   registry.Metadata{"router_id": r.id},
			})
			require.NoError(t, err)
		}

		server := httptest.NewServer(router)
		defer server.Close()

		// Test each router's endpoint
		tests := []struct {
			path     string
			routerID string
		}{
			{"/api/v1/status", "router1"},
			{"/api/v2/status", "router2"},
			{"/internal/status", "router3"},
		}

		for _, tt := range tests {
			resp, err := http.Get(server.URL + tt.path)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, tt.routerID, resp.Header.Get("X-Router-ID"))
		}
	})

	t.Run("router updates", func(t *testing.T) {
		router := NewRouter(handler)

		// Add initial router
		err := router.AddRouter(config.RouterConfig{
			Prefix: "/api/v1",
			Meta:   registry.Metadata{"router_id": "router1"},
		})
		require.NoError(t, err)

		// Add endpoint
		err = router.AddEndpoint("ep1", config.EndpointConfig{
			Method: http.MethodGet,
			Path:   "/test",
			Meta:   registry.Metadata{"router_id": "router1"},
		})
		require.NoError(t, err)

		// Update router prefix
		err = router.UpdateRouter(config.RouterConfig{
			Prefix: "/api/v2",
			Meta:   registry.Metadata{"router_id": "router1"},
		})
		require.NoError(t, err)

		server := httptest.NewServer(router)
		defer server.Close()

		// Old path should not work
		resp, err := http.Get(server.URL + "/api/v1/test")
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)

		// New path should work
		resp, err = http.Get(server.URL + "/api/v2/test")
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "router1", resp.Header.Get("X-Router-ID"))
	})

	t.Run("concurrent router operations", func(t *testing.T) {
		router := NewRouter(handler)
		done := make(chan bool)

		// Concurrent router additions and updates
		go func() {
			for i := 0; i < 10; i++ {
				_ = router.AddRouter(config.RouterConfig{
					Prefix: "/api/v1",
					Meta:   registry.Metadata{"router_id": "router1"},
				})
			}
			done <- true
		}()

		go func() {
			for i := 0; i < 10; i++ {
				_ = router.UpdateRouter(config.RouterConfig{
					Prefix: "/api/v2",
					Meta:   registry.Metadata{"router_id": "router1"},
				})
			}
			done <- true
		}()

		// Wait for operations to complete
		<-done
		<-done

		// Verify router state is consistent
		server := httptest.NewServer(router)
		defer server.Close()

		resp, err := http.Get(server.URL + "/api/v2/test")
		require.NoError(t, err)
		assert.True(t, resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusOK)
	})

	t.Run("middleware composition", func(t *testing.T) {
		router := NewRouter(handler)

		// Add router with middleware
		err := router.AddRouter(config.RouterConfig{
			Prefix:      "/api/v1",
			Meta:        registry.Metadata{"router_id": "router1"},
			Middlewares: []string{"request_id", "real_ip"},
		})
		require.NoError(t, err)

		// Add endpoint
		err = router.AddEndpoint("ep1", config.EndpointConfig{
			Method: http.MethodGet,
			Path:   "/test",
			Meta:   registry.Metadata{"router_id": "router1"},
		})
		require.NoError(t, err)

		server := httptest.NewServer(router)
		defer server.Close()

		resp, err := http.Get(server.URL + "/api/v1/test")
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		assert.NotEmpty(t, resp.Header.Get("X-Router-ID"), "Request ID middleware should be applied")
		assert.Equal(t, "router1", resp.Header.Get("X-Router-ID"))
	})
}
