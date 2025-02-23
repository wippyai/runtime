package __ignore

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/ponyruntime/pony/api/registry"
	config "github.com/ponyruntime/pony/api/service/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouterComposition(t *testing.T) {
	// Test handler that returns different responses based on the path
	handler := func(w http.ResponseWriter, r *http.Request) {
		routeInfo, ok := GetRouteInfo(r.Context())
		if !ok {
			http.Error(w, "route info not found", http.StatusInternalServerError)
			return
		}
		w.Header().Set("X-Router-ID", routeInfo.Endpoint.Meta.StringValue(config.RouterID))
		w.WriteHeader(http.StatusOK)
	}

	t.Run("basic composition", func(t *testing.T) {
		router := NewRouter(handler)

		// AddCleanup a new router
		err := router.AddRouter("router1", config.RouterConfig{
			Prefix: "/api/v1",
			Meta:   registry.Metadata{config.RouterID: "router1"},
		})
		require.NoError(t, err)

		// AddCleanup endpoints to different routers
		err = router.AddEndpoint("ep1", config.EndpointConfig{
			Method: http.MethodGet,
			Path:   "/test",
			Meta:   registry.Metadata{config.RouterID: "router1"},
		})
		require.NoError(t, err)

		err = router.AddEndpoint("ep2", config.EndpointConfig{
			Method: http.MethodGet,
			Path:   "/default",
			Meta:   registry.Metadata{config.RouterID: ""},
		})
		require.NoError(t, err)

		// Test the composed routes
		server := httptest.NewServer(router)
		defer server.Close()

		// Test router1 endpoint
		resp, err := http.Get(server.URL + "/api/v1/test") //nolint:noctx
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "router1", resp.Header.Get("X-Router-ID"))
		assert.NoError(t, resp.Body.Close())

		// Test default router endpoint
		resp, err = http.Get(server.URL + "/default") //nolint:noctx
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "", resp.Header.Get("X-Router-ID"))
		assert.NoError(t, resp.Body.Close())
	})

	t.Run("multiple routers", func(t *testing.T) {
		router := NewRouter(handler)

		// AddCleanup multiple routers
		routers := []struct {
			id     string
			prefix string
		}{
			{"router1", "/api/v1"},
			{"router2", "/api/v2"},
			{"router3", "/helpers"},
		}

		for _, r := range routers {
			err := router.AddRouter(r.id, config.RouterConfig{
				Prefix: r.prefix,
				Meta:   registry.Metadata{config.RouterID: r.id},
			})
			require.NoError(t, err)

			// AddCleanup an endpoint to each router
			err = router.AddEndpoint("", config.EndpointConfig{
				Method: http.MethodGet,
				Path:   "/supervisor",
				Meta:   registry.Metadata{config.RouterID: r.id},
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
			{"/api/v1/supervisor", "router1"},
			{"/api/v2/supervisor", "router2"},
			{"/helpers/supervisor", "router3"},
		}

		for _, tt := range tests {
			resp, err := http.Get(server.URL + tt.path) //nolint:noctx
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, tt.routerID, resp.Header.Get("X-Router-ID"))
			assert.NoError(t, resp.Body.Close())
		}
	})

	t.Run("router updates", func(t *testing.T) {
		router := NewRouter(handler)

		// AddCleanup initial router
		err := router.AddRouter("router1", config.RouterConfig{
			Prefix: "/api/v1",
			Meta:   registry.Metadata{config.RouterID: "router1"},
		})
		require.NoError(t, err)

		// AddCleanup endpoint
		err = router.AddEndpoint("ep1", config.EndpointConfig{
			Method: http.MethodGet,
			Path:   "/test",
			Meta:   registry.Metadata{config.RouterID: "router1"},
		})
		require.NoError(t, err)

		// Update router prefix
		err = router.UpdateRouter("router1", config.RouterConfig{
			Prefix: "/api/v2",
			Meta:   registry.Metadata{config.RouterID: "router1"},
		})
		require.NoError(t, err)

		server := httptest.NewServer(router)
		defer server.Close()

		// Old path should not work
		resp, err := http.Get(server.URL + "/api/v1/test") //nolint:noctx
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		assert.NoError(t, resp.Body.Close())

		// New path should work
		resp, err = http.Get(server.URL + "/api/v2/test") //nolint:noctx
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "router1", resp.Header.Get("X-Router-ID"))
		assert.NoError(t, resp.Body.Close())
	})

	t.Run("concurrent router operations", func(t *testing.T) {
		router := NewRouter(handler)
		done := make(chan bool)

		// Concurrent router additions and updates
		go func() {
			for i := 0; i < 10; i++ {
				_ = router.AddRouter("router1", config.RouterConfig{
					Prefix: "/api/v1",
					Meta:   registry.Metadata{config.RouterID: "router1"},
				})
			}
			done <- true
		}()

		go func() {
			for i := 0; i < 10; i++ {
				_ = router.UpdateRouter("router1", config.RouterConfig{
					Prefix: "/api/v2",
					Meta:   registry.Metadata{config.RouterID: "router1"},
				})
			}
			done <- true
		}()

		// wait for operations to complete
		<-done
		<-done

		// Verify router state is consistent
		server := httptest.NewServer(router)
		defer server.Close()

		_ = router.AddEndpoint(uuid.NewString(), config.EndpointConfig{
			Method: http.MethodGet,
			Path:   "/test",
			Meta:   registry.Metadata{config.RouterID: "router1"},
		})

		resp, err := http.Get(server.URL + "/api/v2/test") //nolint:noctx
		require.NoError(t, err)
		assert.True(t, resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusOK, "Expected status code 404 or 200, got %d", resp.StatusCode)
		assert.NoError(t, resp.Body.Close())
	})

	t.Run("middleware composition", func(t *testing.T) {
		router := NewRouter(handler)

		// AddCleanup router with middleware
		err := router.AddRouter("router1", config.RouterConfig{
			Prefix:      "/api/v1",
			Meta:        registry.Metadata{config.RouterID: "router1"},
			Middlewares: []string{"request_id", "real_ip"},
		})
		require.NoError(t, err)

		// AddCleanup endpoint
		err = router.AddEndpoint("ep1", config.EndpointConfig{
			Method: http.MethodGet,
			Path:   "/test",
			Meta:   registry.Metadata{config.RouterID: "router1"},
		})
		require.NoError(t, err)

		server := httptest.NewServer(router)
		defer server.Close()

		resp, err := http.Get(server.URL + "/api/v1/test") //nolint:noctx
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		assert.NotEmpty(t, resp.Header.Get("X-Router-ID"), "Request Alias middleware should be applied")
		assert.Equal(t, "router1", resp.Header.Get("X-Router-ID"))
		assert.NoError(t, resp.Body.Close())
	})
}

// ---------------------------

func TestRouterEndpointOperations(t *testing.T) {
	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}

	t.Run("endpoint supervisor", func(t *testing.T) {
		router := NewRouter(handler)

		// Test adding endpoint without router_id (should go to default router)
		err := router.AddEndpoint("ep1", config.EndpointConfig{
			Method: http.MethodGet,
			Path:   "/test",
		})
		require.NoError(t, err)

		// Test adding endpoint with non-existent router
		err = router.AddEndpoint("ep2", config.EndpointConfig{
			Method: http.MethodGet,
			Path:   "/test2",
			Meta:   registry.Metadata{config.RouterID: "nonexistent"},
		})
		assert.Error(t, err)

		// Test deleting non-existent endpoint
		err = router.DeleteEndpoint("nonexistent")
		assert.Error(t, err)

		// Test updating non-existent endpoint
		err = router.UpdateEndpoint("nonexistent", config.EndpointConfig{})
		assert.Error(t, err)

		// Test updating endpoint with router change
		err = router.AddRouter("router1", config.RouterConfig{
			Prefix: "/api/v1",
			Meta:   registry.Metadata{config.RouterID: "router1"},
		})
		require.NoError(t, err)

		err = router.UpdateEndpoint("ep1", config.EndpointConfig{
			Method: http.MethodGet,
			Path:   "/test",
			Meta:   registry.Metadata{config.RouterID: "router1"},
		})

		require.NoError(t, err)
	})
}

func TestRouterDefaultRouterOperations(t *testing.T) {
	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}

	t.Run("default router operations", func(t *testing.T) {
		router := NewRouter(handler)

		// Test deleting default router (should fail)
		err := router.DeleteRouter(DefaultRouterID)
		assert.Error(t, err)

		// Test updating default router
		err = router.UpdateRouter(DefaultRouterID, config.RouterConfig{
			Prefix: "/v2",
		})
		assert.NoError(t, err)
	})
}

func TestRouterErrorCases(t *testing.T) {
	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}

	t.Run("router error cases", func(t *testing.T) {
		router := NewRouter(handler)

		// Test adding duplicate router
		err := router.AddRouter("router1", config.RouterConfig{
			Prefix: "/api/v1",
		})
		require.NoError(t, err)

		err = router.AddRouter("router1", config.RouterConfig{
			Prefix: "/api/v2",
		})
		assert.Error(t, err)

		// Test updating non-existent router
		err = router.UpdateRouter("nonexistent", config.RouterConfig{
			Prefix: "/api/v2",
		})
		assert.Error(t, err)

		// Test deleting non-existent router
		err = router.DeleteRouter("nonexistent")
		assert.Error(t, err)
	})
}

func TestRouterConcurrencyStress(t *testing.T) {
	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}

	t.Run("concurrent endpoint operations", func(t *testing.T) {
		router := NewRouter(handler)
		done := make(chan bool)

		// AddCleanup initial router
		err := router.AddRouter("router1", config.RouterConfig{
			Prefix: "/api/v1",
		})
		require.NoError(t, err)

		// Concurrent endpoint operations
		go func() {
			for i := 0; i < 100; i++ {
				_ = router.AddEndpoint("", config.EndpointConfig{
					Method: http.MethodGet,
					Path:   "/test",
					Meta:   registry.Metadata{config.RouterID: "router1"},
				})
			}
			done <- true
		}()

		go func() {
			for i := 0; i < 100; i++ {
				_ = router.DeleteEndpoint(fmt.Sprintf("ep%d", i))
			}
			done <- true
		}()

		// wait for operations to complete
		<-done
		<-done
	})
}

func TestRouterMiddlewareConfiguration(t *testing.T) {
	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}

	t.Run("middleware configuration", func(t *testing.T) {
		router := NewRouter(handler)

		// Test router with all middleware types
		err := router.AddRouter("router1", config.RouterConfig{
			Prefix: "/api/v1",
			Middlewares: []string{
				"timeout",
				"recoverer",
				"request_id",
				"real_ip",
			},
			Options: map[string]string{
				"timeout": "30s",
			},
		})
		require.NoError(t, err)

		// AddCleanup test endpoint
		err = router.AddEndpoint("ep1", config.EndpointConfig{
			Method: http.MethodGet,
			Path:   "/test",
			Meta:   registry.Metadata{config.RouterID: "router1"},
		})
		require.NoError(t, err)

		// Test the endpoint with middleware
		server := httptest.NewServer(router)
		defer server.Close()

		resp, err := http.Get(server.URL + "/api/v1/test") //nolint:noctx
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.NoError(t, resp.Body.Close())

		// Test invalid timeout duration
		err = router.AddRouter("router2", config.RouterConfig{
			Prefix: "/api/v2",
			Middlewares: []string{
				"timeout",
			},
			Options: map[string]string{
				"timeout": "invalid",
			},
		})
		require.NoError(t, err) // Should still create router even with invalid timeout
	})
}

func TestRouter_RebuildRouter_ErrorHandling(t *testing.T) {
	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}

	router := NewRouter(handler)

	// AddCleanup a router with invalid timeout middleware option
	err := router.AddRouter("router1", config.RouterConfig{
		Prefix: "/api/v1",
		Middlewares: []string{
			"timeout",
		},
		Options: map[string]string{
			"timeout": "invalid-duration",
		},
	})
	require.NoError(t, err) // Router creation should still succeed

	// AddCleanup an endpoint to the router
	err = router.AddEndpoint("ep1", config.EndpointConfig{
		Method: http.MethodGet,
		Path:   "/test",
		Meta:   registry.Metadata{config.RouterID: "router1"},
	})
	require.NoError(t, err)

	// Rebuild the router (this should handle the error from invalid middleware internally)
	router.rebuildRouter()

	// Test the endpoint (middleware should be skipped due to error)
	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/test") //nolint:noctx
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode) // Endpoint should still be reachable
	assert.NoError(t, resp.Body.Close())
}

func TestRouter_ServeHTTP_Concurrency(t *testing.T) {
	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}

	router := NewRouter(handler)

	// AddCleanup a router and endpoint
	err := router.AddRouter("router1", config.RouterConfig{
		Prefix: "/api/v1",
	})
	require.NoError(t, err)

	err = router.AddEndpoint("ep1", config.EndpointConfig{
		Method: http.MethodGet,
		Path:   "/test",
		Meta:   registry.Metadata{config.RouterID: "router1"},
	})
	require.NoError(t, err)

	// Spawn a wait group for concurrent requests
	var wg sync.WaitGroup

	// Number of concurrent requests
	numRequests := 100

	// Test server
	server := httptest.NewServer(router)
	defer server.Close()

	// Launch concurrent requests
	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(server.URL + "/api/v1/test") //nolint:noctx
			if err == nil {
				defer func() {
					_ = resp.Body.Close()
				}()
				assert.Equal(t, http.StatusOK, resp.StatusCode)
			}
		}()
	}

	// wait for requests to finish
	wg.Wait()
}

func TestRouter_Endpoint_UUID(t *testing.T) {
	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}

	router := NewRouter(handler)

	// Count endpoints before adding new one
	var initialCount int
	router.endpoints.Range(func(_, _ interface{}) bool {
		initialCount++
		return true
	})

	// AddCleanup an endpoint without providing an Alias
	err := router.AddEndpoint("", config.EndpointConfig{
		Method: http.MethodGet,
		Path:   "/test",
	})
	require.NoError(t, err)

	// Find the newly added endpoint
	var foundEndpointID string
	router.endpoints.Range(func(key, _ interface{}) bool {
		if id, ok := key.(string); ok {
			if _, err := uuid.Parse(id); err == nil {
				foundEndpointID = id
				return false // stop iteration
			}
		}
		return true
	})

	// Verify we found exactly one new endpoint with a valid UUID
	var finalCount int
	router.endpoints.Range(func(_, _ interface{}) bool {
		finalCount++
		return true
	})

	assert.Equal(t, initialCount+1, finalCount, "Expected exactly one new endpoint")
	assert.NotEmpty(t, foundEndpointID, "Expected to find an endpoint with UUID")
	_, err = uuid.Parse(foundEndpointID)
	assert.NoError(t, err, "Expected endpoint Alias to be a valid UUID")
}

func TestRouterEdgeCases(t *testing.T) {
	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}

	t.Run("delete router complex scenarios", func(t *testing.T) {
		router := NewRouter(handler)

		// AddCleanup router with multiple endpoints
		err := router.AddRouter("router-to-delete", config.RouterConfig{
			Prefix: "/delete-test",
		})
		require.NoError(t, err)

		// AddCleanup multiple endpoints
		endpoints := []struct {
			id   string
			path string
		}{
			{"ep1", "/test1"},
			{"ep2", "/test2"},
			{"ep3", "/test3"},
		}

		for _, ep := range endpoints {
			err := router.AddEndpoint(ep.id, config.EndpointConfig{
				Method: http.MethodGet,
				Path:   ep.path,
				Meta:   registry.Metadata{config.RouterID: "router-to-delete"},
			})
			require.NoError(t, err)
		}

		// Delete router and verify all endpoints are removed
		err = router.DeleteRouter("router-to-delete")
		require.NoError(t, err)

		// Verify endpoints are deleted
		for _, ep := range endpoints {
			err := router.DeleteEndpoint(ep.id)
			assert.Error(t, err, "Expected endpoint to be already deleted")
		}

		// Try to delete the same router again
		err = router.DeleteRouter("router-to-delete")
		assert.Error(t, err, "Expected error when deleting non-existent router")
	})

	t.Run("update endpoint complex scenarios", func(t *testing.T) {
		router := NewRouter(handler)

		// AddCleanup two routers for testing endpoint updates between routers
		err := router.AddRouter("router1", config.RouterConfig{
			Prefix: "/api/v1",
		})
		require.NoError(t, err)

		err = router.AddRouter("router2", config.RouterConfig{
			Prefix: "/api/v2",
		})
		require.NoError(t, err)

		// AddCleanup initial endpoint
		err = router.AddEndpoint("test-ep", config.EndpointConfig{
			Method: http.MethodGet,
			Path:   "/test",
			Meta:   registry.Metadata{config.RouterID: "router1"},
		})
		require.NoError(t, err)

		// Test updating endpoint to move it between routers
		err = router.UpdateEndpoint("test-ep", config.EndpointConfig{
			Method: http.MethodGet,
			Path:   "/test",
			Meta:   registry.Metadata{config.RouterID: "router2"},
		})
		require.NoError(t, err)

		// Verify endpoint works with new router
		server := httptest.NewServer(router)
		defer server.Close()

		resp, err := http.Get(server.URL + "/api/v2/test") //nolint:noctx
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.NoError(t, resp.Body.Close())

		// Test updating endpoint with invalid router Alias
		err = router.UpdateEndpoint("test-ep", config.EndpointConfig{
			Method: http.MethodGet,
			Path:   "/test",
			Meta:   registry.Metadata{config.RouterID: "non-existent"},
		})
		assert.Error(t, err)

		// Test update with conflicting path
		err = router.AddEndpoint("existing-ep", config.EndpointConfig{
			Method: http.MethodGet,
			Path:   "/conflict",
			Meta:   registry.Metadata{config.RouterID: "router2"},
		})
		require.NoError(t, err)

		err = router.UpdateEndpoint("test-ep", config.EndpointConfig{
			Method: http.MethodGet,
			Path:   "/conflict",
			Meta:   registry.Metadata{config.RouterID: "router2"},
		})
		assert.Error(t, err)
	})
}

func TestRouterUpdateScenarios(t *testing.T) {
	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}

	t.Run("update router with active endpoints", func(t *testing.T) {
		router := NewRouter(handler)

		// AddCleanup initial router
		err := router.AddRouter("update-test", config.RouterConfig{
			Prefix: "/v1",
		})
		require.NoError(t, err)

		// AddCleanup endpoints
		err = router.AddEndpoint("ep1", config.EndpointConfig{
			Method: http.MethodGet,
			Path:   "/test",
			Meta:   registry.Metadata{config.RouterID: "update-test"},
		})
		require.NoError(t, err)

		// Update router with new configuration
		err = router.UpdateRouter("update-test", config.RouterConfig{
			Prefix:      "/v2",
			Middlewares: []string{"timeout"},
			Options:     map[string]string{"timeout": "30s"},
		})
		require.NoError(t, err)

		server := httptest.NewServer(router)
		defer server.Close()

		// Test if the endpoint is accessible at a new path
		resp, err := http.Get(server.URL + "/v2/test") //nolint:noctx
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.NoError(t, resp.Body.Close())

		// Test if an old path is no longer accessible
		resp, err = http.Get(server.URL + "/v1/test") //nolint:noctx
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		assert.NoError(t, resp.Body.Close())
	})
}
