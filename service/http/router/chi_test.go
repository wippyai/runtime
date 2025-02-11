package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	config "github.com/ponyruntime/pony/api/service/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		id        string
		endpoint  config.EndpointConfig
		wantError bool
	}{
		{
			name: "valid endpoint",
			id:   "test1",
			endpoint: config.EndpointConfig{
				Method: http.MethodGet,
				Path:   "/test",
			},
			wantError: false,
		},
		{
			name: "duplicate endpoint",
			id:   "test1",
			endpoint: config.EndpointConfig{
				Method: http.MethodGet,
				Path:   "/test",
			},
			wantError: true,
		},
		{
			name: "different Alias same path and method",
			id:   "test2",
			endpoint: config.EndpointConfig{
				Method: http.MethodGet,
				Path:   "/test",
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := router.AddEndpoint(tt.id, tt.endpoint)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				endpoints := router.GetEndpoints()
				assert.Contains(t, endpoints, tt.id)
				assert.Equal(t, tt.endpoint, endpoints[tt.id])
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
	err := router.AddEndpoint("test1", endpoint)
	require.NoError(t, err)

	tests := []struct {
		name       string
		endpointID string
		wantError  bool
	}{
		{
			name:       "existing endpoint",
			endpointID: "test1",
			wantError:  false,
		},
		{
			name:       "non-existent endpoint",
			endpointID: "missing",
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := router.DeleteEndpoint(tt.endpointID)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				endpoints := router.GetEndpoints()
				assert.NotContains(t, endpoints, tt.endpointID)
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
	err := router.AddEndpoint("test1", originalEndpoint)
	require.NoError(t, err)

	err = router.AddEndpoint("test2", config.EndpointConfig{
		Method: http.MethodPost,
		Path:   "/other",
	})
	require.NoError(t, err)

	tests := []struct {
		name      string
		id        string
		endpoint  config.EndpointConfig
		wantError bool
	}{
		{
			name: "existing endpoint - change method",
			id:   "test1",
			endpoint: config.EndpointConfig{
				Method: http.MethodPost,
				Path:   "/test",
			},
			wantError: false,
		},
		{
			name: "existing endpoint - change path",
			id:   "test1",
			endpoint: config.EndpointConfig{
				Method: http.MethodGet,
				Path:   "/test-updated",
			},
			wantError: false,
		},
		{
			name: "path and method conflict with existing endpoint",
			id:   "test1",
			endpoint: config.EndpointConfig{
				Method: http.MethodPost,
				Path:   "/other",
			},
			wantError: true,
		},
		{
			name: "non-existent endpoint Alias",
			id:   "missing",
			endpoint: config.EndpointConfig{
				Method: http.MethodGet,
				Path:   "/test",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := router.UpdateEndpoint(tt.id, tt.endpoint)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				endpoints := router.GetEndpoints()
				assert.Contains(t, endpoints, tt.id)
				assert.Equal(t, tt.endpoint, endpoints[tt.id])
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
	endpoint := config.EndpointConfig{
		Method: http.MethodGet,
		Path:   "/api/test",
	}
	err := router.AddEndpoint("test1", endpoint)
	require.NoError(t, err)

	var capturedRouteInfo *config.RouteInfo
	handler := func(w http.ResponseWriter, r *http.Request) {
		routeInfo, ok := r.Context().Value(config.RouteCtx).(*config.RouteInfo)
		require.True(t, ok)
		capturedRouteInfo = routeInfo
		w.WriteHeader(http.StatusOK)
	}

	chiRouter, err := router.Build(handler)
	require.NoError(t, err)
	assert.NotNil(t, chiRouter)

	// Test the built router
	server := httptest.NewServer(chiRouter)
	defer server.Close()

	// Test successful route
	resp, err := http.Get(server.URL + "/api/test") //nolint:noctx
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NotNil(t, capturedRouteInfo)
	assert.Equal(t, "test1", capturedRouteInfo.EndpointID)
	assert.Equal(t, endpoint.Method, capturedRouteInfo.Endpoint.Method)
	assert.Equal(t, endpoint.Path, capturedRouteInfo.Endpoint.Path)
	assert.NoError(t, resp.Body.Close())

	// Test 404 for non-existent route
	resp, err = http.Get(server.URL + "/api/missing") //nolint:noctx
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.NoError(t, resp.Body.Close())

	// Test 405 for wrong method
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL+"/api/test", nil)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
	assert.NoError(t, resp.Body.Close())
}

func TestRouteContext(t *testing.T) {
	router, _ := NewChiRouter(config.RouterConfig{Prefix: "/"})
	endpoint := config.EndpointConfig{
		Method: http.MethodGet,
		Path:   "/api/users/{id}",
	}
	err := router.AddEndpoint("user1", endpoint)
	require.NoError(t, err)

	var capturedRouteInfo *config.RouteInfo
	handler := func(w http.ResponseWriter, r *http.Request) {
		routeInfo, ok := r.Context().Value(config.RouteCtx).(*config.RouteInfo)
		require.True(t, ok)
		capturedRouteInfo = routeInfo
		w.WriteHeader(http.StatusOK)
	}

	chiRouter, err := router.Build(handler)
	require.NoError(t, err)

	server := httptest.NewServer(chiRouter)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/users/123") //nolint:noctx
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "user1", capturedRouteInfo.EndpointID)
	assert.Equal(t, "123", capturedRouteInfo.Params["id"])
	assert.NoError(t, resp.Body.Close())
}

func TestClone(t *testing.T) {
	originalCfg := config.RouterConfig{
		Prefix:      "/api",
		Middlewares: []string{"timeout"},
		Options:     map[string]string{"timeout": "30s"},
	}

	originalRouter, _ := NewChiRouter(originalCfg)
	endpoint := config.EndpointConfig{
		Method: http.MethodGet,
		Path:   "/test",
	}
	err := originalRouter.AddEndpoint("test1", endpoint)
	require.NoError(t, err)

	newCfg := config.RouterConfig{
		Prefix:      "/v2",
		Middlewares: []string{"timeout"},
		Options:     map[string]string{"timeout": "60s"},
	}

	clonedRouter, err := originalRouter.Clone(newCfg)
	require.NoError(t, err)

	assert.Equal(t, newCfg, clonedRouter.GetConfig())
	assert.Equal(t, len(originalRouter.GetEndpoints()), len(clonedRouter.GetEndpoints()))

	// Verify endpoint was cloned correctly
	clonedEndpoints := clonedRouter.GetEndpoints()
	assert.Contains(t, clonedEndpoints, "test1")
	assert.Equal(t, endpoint, clonedEndpoints["test1"])
}

func TestRouteInfoContext(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		requestPath    string
		method         string
		endpointID     string
		expectedParams map[string]string
	}{
		{
			name:        "single parameter",
			path:        "/users/{id}",
			requestPath: "/users/123",
			method:      http.MethodGet,
			endpointID:  "user1",
			expectedParams: map[string]string{
				"id": "123",
			},
		},
		{
			name:        "multiple parameters",
			path:        "/users/{userID}/posts/{postID}",
			requestPath: "/users/456/posts/789",
			method:      http.MethodGet,
			endpointID:  "user_post1",
			expectedParams: map[string]string{
				"userID": "456",
				"postID": "789",
			},
		},
		{
			name:        "nested resources",
			path:        "/orgs/{orgID}/teams/{teamID}/members/{memberID}",
			requestPath: "/orgs/org123/teams/team456/members/mem789",
			method:      http.MethodGet,
			endpointID:  "org_member1",
			expectedParams: map[string]string{
				"orgID":    "org123",
				"teamID":   "team456",
				"memberID": "mem789",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, err := NewChiRouter(config.RouterConfig{})
			require.NoError(t, err)

			endpoint := config.EndpointConfig{
				Method: tt.method,
				Path:   tt.path,
			}
			err = router.AddEndpoint(tt.endpointID, endpoint)
			require.NoError(t, err)

			var capturedRouteInfo *config.RouteInfo
			handler := func(w http.ResponseWriter, r *http.Request) {
				routeInfo, ok := r.Context().Value(config.RouteCtx).(*config.RouteInfo)
				require.True(t, ok)
				capturedRouteInfo = routeInfo
				w.WriteHeader(http.StatusOK)
			}

			chiRouter, err := router.Build(handler)
			require.NoError(t, err)

			server := httptest.NewServer(chiRouter)
			defer server.Close()

			resp, err := http.Get(server.URL + tt.requestPath) //nolint:noctx
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			assert.NotNil(t, capturedRouteInfo)
			assert.Equal(t, tt.endpointID, capturedRouteInfo.EndpointID)
			assert.Equal(t, tt.expectedParams, capturedRouteInfo.Params)
			assert.Equal(t, endpoint.Path, capturedRouteInfo.Endpoint.Path)
			assert.Equal(t, endpoint.Method, capturedRouteInfo.Endpoint.Method)
			assert.NoError(t, resp.Body.Close())
		})
	}
}
