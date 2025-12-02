package firewall

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	httpapi "github.com/wippyai/runtime/api/service/http"
)

// mockScope implements security.Scope for testing
type mockScope struct {
	result   security.Result
	policies []security.Policy
}

func (m *mockScope) With(policy security.Policy) security.Scope {
	return &mockScope{result: m.result, policies: append(m.policies, policy)}
}

func (m *mockScope) Without(_ registry.ID) security.Scope {
	return m
}

func (m *mockScope) Evaluate(_ security.Actor, _, _ string, _ registry.Metadata) security.Result {
	return m.result
}

func (m *mockScope) Contains(_ registry.ID) bool {
	return false
}

func (m *mockScope) Policies() []security.Policy {
	return m.policies
}

func TestResourceFirewallOptions(t *testing.T) {
	t.Run("default action when not specified", func(t *testing.T) {
		middleware := CreateResourceFirewallMiddleware(map[string]string{
			ResourceOptionTarget: "doc:1",
		})

		assert.NotNil(t, middleware)
	})

	t.Run("custom action specified", func(t *testing.T) {
		middleware := CreateResourceFirewallMiddleware(map[string]string{
			ResourceOptionAction: "read",
			ResourceOptionTarget: "document:123",
		})

		assert.NotNil(t, middleware)
	})

	t.Run("legacy key fallback", func(t *testing.T) {
		middleware := CreateResourceFirewallMiddleware(map[string]string{
			legacyResourceAction: "write",
			legacyResourceTarget: "doc:2",
		})

		assert.NotNil(t, middleware)
	})

	t.Run("new keys take precedence over legacy", func(t *testing.T) {
		middleware := CreateResourceFirewallMiddleware(map[string]string{
			ResourceOptionAction: "read",
			legacyResourceAction: "write",
			ResourceOptionTarget: "new:target",
			legacyResourceTarget: "old:target",
		})

		assert.NotNil(t, middleware)
	})

	t.Run("deny without actor", func(t *testing.T) {
		middleware := CreateResourceFirewallMiddleware(map[string]string{})

		handler := middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			t.Fatal("should not reach handler")
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "Authentication required")
	})
}

func TestEndpointFirewallOptions(t *testing.T) {
	t.Run("default action when not specified", func(t *testing.T) {
		middleware := CreateEndpointFirewallMiddleware(map[string]string{})

		assert.NotNil(t, middleware)
	})

	t.Run("custom action specified", func(t *testing.T) {
		middleware := CreateEndpointFirewallMiddleware(map[string]string{
			EndpointOptionAction: "execute",
		})

		assert.NotNil(t, middleware)
	})

	t.Run("legacy key fallback", func(t *testing.T) {
		middleware := CreateEndpointFirewallMiddleware(map[string]string{
			legacyEndpointAction: "call",
		})

		assert.NotNil(t, middleware)
	})

	t.Run("new key takes precedence over legacy", func(t *testing.T) {
		middleware := CreateEndpointFirewallMiddleware(map[string]string{
			EndpointOptionAction: "execute",
			legacyEndpointAction: "call",
		})

		assert.NotNil(t, middleware)
	})

	t.Run("deny without actor", func(t *testing.T) {
		middleware := CreateEndpointFirewallMiddleware(map[string]string{})

		handler := middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			t.Fatal("should not reach handler")
		}))

		req := httptest.NewRequest("POST", "http://example.com/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("deny without route info", func(t *testing.T) {
		middleware := CreateEndpointFirewallMiddleware(map[string]string{})

		handler := middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			t.Fatal("should not reach handler")
		}))

		req := httptest.NewRequest("POST", "http://example.com/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestGetOption(t *testing.T) {
	t.Run("prefer new key over legacy", func(t *testing.T) {
		options := map[string]string{
			"resource_firewall.action": "new-value",
			"resource_action":          "legacy-value",
		}

		result := getOption(options, "resource_firewall.action", "resource_action")
		assert.Equal(t, "new-value", result)
	})

	t.Run("fallback to legacy when new key missing", func(t *testing.T) {
		options := map[string]string{
			"resource_action": "legacy-value",
		}

		result := getOption(options, "resource_firewall.action", "resource_action")
		assert.Equal(t, "legacy-value", result)
	})

	t.Run("return empty when both missing", func(t *testing.T) {
		options := map[string]string{}

		result := getOption(options, "resource_firewall.action", "resource_action")
		assert.Equal(t, "", result)
	})
}

func TestEndpointResourceFormatting(t *testing.T) {
	t.Run("format endpoint ID correctly", func(t *testing.T) {
		id := registry.ID{NS: "api", Name: "create_user"}
		assert.Equal(t, "api:create_user", id.String())
	})

	t.Run("handle empty namespace", func(t *testing.T) {
		id := registry.ID{NS: "", Name: "simple_endpoint"}
		assert.Equal(t, ":simple_endpoint", id.String())
	})

	t.Run("handle empty name", func(t *testing.T) {
		id := registry.ID{NS: "api", Name: ""}
		assert.Equal(t, "api:", id.String())
	})
}

func TestResourceFirewallWithActorAndScope(t *testing.T) {
	t.Run("allow access with valid actor and scope", func(t *testing.T) {
		middleware := CreateResourceFirewallMiddleware(map[string]string{
			ResourceOptionTarget: "doc:1",
		})

		handlerCalled := false
		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			handlerCalled = true
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		ctx, _ := ctxapi.OpenFrameContext(req.Context())

		actor := security.Actor{ID: "user123", Meta: nil}
		_ = security.SetActor(ctx, actor)
		_ = security.SetScope(ctx, &mockScope{result: security.Allow})

		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.True(t, handlerCalled)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("deny access when scope denies", func(t *testing.T) {
		middleware := CreateResourceFirewallMiddleware(map[string]string{
			ResourceOptionTarget: "doc:1",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		ctx, _ := ctxapi.OpenFrameContext(req.Context())

		actor := security.Actor{ID: "user123", Meta: nil}
		_ = security.SetActor(ctx, actor)
		_ = security.SetScope(ctx, &mockScope{result: security.Deny})

		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "Access denied")
	})

	t.Run("deny access when actor has empty ID", func(t *testing.T) {
		middleware := CreateResourceFirewallMiddleware(map[string]string{
			ResourceOptionTarget: "doc:1",
		})

		handler := middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			t.Fatal("should not reach handler")
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		ctx, _ := ctxapi.OpenFrameContext(req.Context())

		actor := security.Actor{ID: "", Meta: nil}
		_ = security.SetActor(ctx, actor)

		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("deny access when scope is nil", func(t *testing.T) {
		middleware := CreateResourceFirewallMiddleware(map[string]string{
			ResourceOptionTarget: "doc:1",
		})

		handler := middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			t.Fatal("should not reach handler")
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		ctx, _ := ctxapi.OpenFrameContext(req.Context())

		actor := security.Actor{ID: "user123", Meta: nil}
		_ = security.SetActor(ctx, actor)

		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "No authorization scope")
	})
}

func TestEndpointFirewallWithActorAndScope(t *testing.T) {
	t.Run("allow access with valid actor, scope and route info", func(t *testing.T) {
		middleware := CreateEndpointFirewallMiddleware(map[string]string{})

		handlerCalled := false
		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			handlerCalled = true
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		ctx, _ := ctxapi.OpenFrameContext(req.Context())

		actor := security.Actor{ID: "user123", Meta: nil}
		routeInfo := &httpapi.RouteInfo{
			Endpoint: registry.ID{NS: "api", Name: "test"},
		}

		_ = security.SetActor(ctx, actor)
		_ = security.SetScope(ctx, &mockScope{result: security.Allow})
		_ = httpapi.SetRouteInfo(ctx, routeInfo)

		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.True(t, handlerCalled)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("deny access when missing route info", func(t *testing.T) {
		middleware := CreateEndpointFirewallMiddleware(map[string]string{})

		handler := middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			t.Fatal("should not reach handler")
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		ctx, _ := ctxapi.OpenFrameContext(req.Context())

		actor := security.Actor{ID: "user123", Meta: nil}
		_ = security.SetActor(ctx, actor)
		_ = security.SetScope(ctx, &mockScope{result: security.Allow})

		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "No route information")
	})

	t.Run("deny access when scope denies", func(t *testing.T) {
		middleware := CreateEndpointFirewallMiddleware(map[string]string{})

		handler := middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			t.Fatal("should not reach handler")
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		ctx, _ := ctxapi.OpenFrameContext(req.Context())

		actor := security.Actor{ID: "user123", Meta: nil}
		routeInfo := &httpapi.RouteInfo{
			Endpoint: registry.ID{NS: "api", Name: "test"},
		}

		_ = security.SetActor(ctx, actor)
		_ = security.SetScope(ctx, &mockScope{result: security.Deny})
		_ = httpapi.SetRouteInfo(ctx, routeInfo)

		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "Access denied")
	})
}

func TestWriteJSONError(t *testing.T) {
	t.Run("writes valid JSON response", func(t *testing.T) {
		w := httptest.NewRecorder()

		WriteJSONError(w, http.StatusForbidden, false, "Access denied", "Missing permissions")

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, false, response["success"])
		assert.Equal(t, "Access denied", response["error"])
		assert.Equal(t, "Missing permissions", response["details"])
	})

	t.Run("writes success response", func(t *testing.T) {
		w := httptest.NewRecorder()

		WriteJSONError(w, http.StatusOK, true, "", "Operation completed")

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, true, response["success"])
	})

	t.Run("handles various status codes", func(t *testing.T) {
		codes := []int{
			http.StatusBadRequest,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusNotFound,
			http.StatusInternalServerError,
		}

		for _, code := range codes {
			w := httptest.NewRecorder()
			WriteJSONError(w, code, false, "error", "details")
			assert.Equal(t, code, w.Code)
		}
	})
}

func TestConstants(t *testing.T) {
	t.Run("middleware names are defined", func(t *testing.T) {
		assert.Equal(t, "endpoint_firewall", EndpointMiddlewareName)
		assert.Equal(t, "resource_firewall", ResourceMiddlewareName)
	})

	t.Run("default actions are defined", func(t *testing.T) {
		assert.Equal(t, "access", EndpointDefaultAction)
		assert.Equal(t, "access", ResourceDefaultAction)
	})

	t.Run("option keys are defined", func(t *testing.T) {
		assert.Equal(t, "endpoint_firewall.action", EndpointOptionAction)
		assert.Equal(t, "resource_firewall.action", ResourceOptionAction)
		assert.Equal(t, "resource_firewall.target", ResourceOptionTarget)
	})
}
