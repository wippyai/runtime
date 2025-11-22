package firewall

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/registry"
)

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
