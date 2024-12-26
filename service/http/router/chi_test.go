package router

import (
	"context"
	config "github.com/ponyruntime/pony/api/service/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewChiRouter(t *testing.T) {
	cfg := config.RouterConfig{
		Prefix:      "/api",
		Middlewares: []string{"timeout"},
		Options:     map[string]string{"timeout": "30s"},
	}

	router, err := NewChiRouter(cfg)
	require.NoError(t, err)
	assert.NotNil(t, router)
	assert.Equal(t, cfg, router.GetConfig())
	assert.Empty(t, router.GetEndpoints())
}

func TestAddEndpoint(t *testing.T) {
	router, _ := NewChiRouter(config.RouterConfig{})

	tests := []struct {
		name      string
		endpoint  config.EndpointConfig
		wantError bool
	}{
		{
			name: "valid endpoint",
			endpoint: config.EndpointConfig{
				Method: http.MethodGet,
				Path:   "/test",
			},
			wantError: false,
		},
		{
			name: "duplicate endpoint",
			endpoint: config.EndpointConfig{
				Method: http.MethodGet,
				Path:   "/test",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := router.AddEndpoint(tt.endpoint)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDeleteEndpoint(t *testing.T) {
	router, _ := NewChiRouter(config.RouterConfig{})
	endpoint := config.EndpointConfig{
		Method: http.MethodGet,
		Path:   "/test",
	}
	_ = router.AddEndpoint(endpoint)

	tests := []struct {
		name      string
		path      string
		method    string
		wantError bool
	}{
		{
			name:      "existing endpoint",
			path:      "/test",
			method:    http.MethodGet,
			wantError: false,
		},
		{
			name:      "non-existent endpoint",
			path:      "/missing",
			method:    http.MethodGet,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := router.DeleteEndpoint(tt.path, tt.method)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestUpdateEndpoint(t *testing.T) {
	router, _ := NewChiRouter(config.RouterConfig{})
	originalEndpoint := config.EndpointConfig{
		Method: http.MethodGet,
		Path:   "/test",
	}
	_ = router.AddEndpoint(originalEndpoint)

	tests := []struct {
		name      string
		endpoint  config.EndpointConfig
		wantError bool
	}{
		{
			name: "existing endpoint",
			endpoint: config.EndpointConfig{
				Method: http.MethodGet,
				Path:   "/test",
			},
			wantError: false,
		},
		{
			name: "non-existent endpoint",
			endpoint: config.EndpointConfig{
				Method: http.MethodPost,
				Path:   "/missing",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := router.UpdateEndpoint(tt.endpoint)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestBuildRouter(t *testing.T) {
	cfg := config.RouterConfig{
		Prefix:      "/",
		Middlewares: []string{"timeout", "recoverer", "request_id", "real_ip"},
		Options:     map[string]string{"timeout": "30s"},
	}

	router, _ := NewChiRouter(cfg)
	_ = router.AddEndpoint(config.EndpointConfig{
		Method: http.MethodGet,
		Path:   "/api/test",
	})

	handler := func(w http.ResponseWriter, r *http.Request) {
		routeInfo, ok := GetRouteInfo(r.Context())
		require.True(t, ok)
		assert.NotNil(t, routeInfo)
		w.WriteHeader(http.StatusOK)
	}

	chiRouter, err := router.Build(handler)
	require.NoError(t, err)
	assert.NotNil(t, chiRouter)

	// Test the built router
	server := httptest.NewServer(chiRouter)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/test")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Test 404
	resp, err = http.Get(server.URL + "/api/missing")
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	// Test 405
	resp, err = http.Post(server.URL+"/api/test", "application/json", nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestRouteContext(t *testing.T) {
	router, _ := NewChiRouter(config.RouterConfig{Prefix: "/"})
	_ = router.AddEndpoint(config.EndpointConfig{
		Method: http.MethodGet,
		Path:   "/api/users/{id}",
	})

	handler := func(w http.ResponseWriter, r *http.Request) {
		routeInfo, ok := GetRouteInfo(r.Context())
		require.True(t, ok)
		assert.Equal(t, "123", routeInfo.Params["id"])
		w.WriteHeader(http.StatusOK)
	}

	chiRouter, _ := router.Build(handler)
	server := httptest.NewServer(chiRouter)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/users/123")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestClone(t *testing.T) {
	originalCfg := config.RouterConfig{
		Prefix:      "/api",
		Middlewares: []string{"timeout"},
		Options:     map[string]string{"timeout": "30s"},
	}

	originalRouter, _ := NewChiRouter(originalCfg)
	_ = originalRouter.AddEndpoint(config.EndpointConfig{
		Method: http.MethodGet,
		Path:   "/test",
	})

	newCfg := config.RouterConfig{
		Prefix:      "/v2",
		Middlewares: []string{"timeout"},
		Options:     map[string]string{"timeout": "60s"},
	}

	clonedRouter, err := originalRouter.Clone(newCfg)
	require.NoError(t, err)

	assert.Equal(t, newCfg, clonedRouter.GetConfig())
	assert.Equal(t, len(originalRouter.GetEndpoints()), len(clonedRouter.GetEndpoints()))
}

func TestRouteInfoWithParameters(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		requestPath    string
		method         string
		expectedParams map[string]string
		extraChecks    func(*testing.T, *config.RouteInfo)
	}{
		{
			name:        "single URL parameter",
			path:        "/users/{id}",
			requestPath: "/users/123",
			method:      http.MethodGet,
			expectedParams: map[string]string{
				"id": "123",
			},
		},
		{
			name:        "multiple URL parameters",
			path:        "/users/{userID}/posts/{postID}",
			requestPath: "/users/456/posts/789",
			method:      http.MethodGet,
			expectedParams: map[string]string{
				"userID": "456",
				"postID": "789",
			},
		},
		{
			name:        "parameters with special characters",
			path:        "/users/{username}/profile",
			requestPath: "/users/john.doe_123/profile",
			method:      http.MethodGet,
			expectedParams: map[string]string{
				"username": "john.doe_123",
			},
		},
		{
			name:        "nested resource paths",
			path:        "/orgs/{orgID}/teams/{teamID}/members/{memberID}",
			requestPath: "/orgs/org123/teams/team456/members/mem789",
			method:      http.MethodGet,
			expectedParams: map[string]string{
				"orgID":    "org123",
				"teamID":   "team456",
				"memberID": "mem789",
			},
			extraChecks: func(t *testing.T, info *config.RouteInfo) {
				assert.Equal(t, "/orgs/{orgID}/teams/{teamID}/members/{memberID}", info.Endpoint.Path)
				assert.Equal(t, http.MethodGet, info.Endpoint.Method)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create router with test endpoint
			router, err := NewChiRouter(config.RouterConfig{})
			require.NoError(t, err)

			endpoint := config.EndpointConfig{
				Method: tt.method,
				Path:   tt.path,
			}
			err = router.AddEndpoint(endpoint)
			require.NoError(t, err)

			// Create test handler that validates route info
			handler := func(w http.ResponseWriter, r *http.Request) {
				routeInfo, ok := GetRouteInfo(r.Context())
				require.True(t, ok, "Route info should be present in context")
				require.NotNil(t, routeInfo, "Route info should not be nil")

				// Validate parameters
				assert.Equal(t, tt.expectedParams, routeInfo.Params,
					"Parameters should match expected values")

				// Check endpoint information
				assert.Equal(t, tt.path, routeInfo.Endpoint.Path,
					"Endpoint path should match configuration")
				assert.Equal(t, tt.method, routeInfo.Endpoint.Method,
					"Endpoint method should match configuration")

				// Run any additional checks
				if tt.extraChecks != nil {
					tt.extraChecks(t, routeInfo)
				}

				w.WriteHeader(http.StatusOK)
			}

			// Build and test the router
			chiRouter, err := router.Build(handler)
			require.NoError(t, err)

			server := httptest.NewServer(chiRouter)
			defer server.Close()

			// Make request
			req, err := http.NewRequest(tt.method, server.URL+tt.requestPath, nil)
			require.NoError(t, err)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})
	}
}

func TestRouteInfoContext(t *testing.T) {
	// Test direct context manipulation
	t.Run("context value extraction", func(t *testing.T) {
		routeInfo := &config.RouteInfo{
			Params: map[string]string{"test": "value"},
			Endpoint: config.EndpointConfig{
				Method: http.MethodGet,
				Path:   "/test/{test}",
			},
			MatchedURI: "/test/value",
		}

		ctx := context.WithValue(context.Background(), config.RouteInfoCtx, routeInfo)
		extracted, ok := GetRouteInfo(ctx)

		assert.True(t, ok, "Should successfully extract route info")
		assert.Equal(t, routeInfo, extracted, "Extracted route info should match original")
	})

	// Test empty/invalid context
	t.Run("empty context", func(t *testing.T) {
		extracted, ok := GetRouteInfo(context.Background())
		assert.False(t, ok, "Should return false for empty context")
		assert.Nil(t, extracted, "Should return nil for empty context")
	})
}
