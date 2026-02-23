// SPDX-License-Identifier: MPL-2.0

// Package http provides HTTP service configuration.
package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
)

type mockResponseWriter struct {
	http.ResponseWriter
	written bool
}

func (m *mockResponseWriter) Write([]byte) (int, error) {
	m.written = true
	return 0, nil
}

func TestNewRequestContext(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	t.Run("creates new request context", func(t *testing.T) {
		reqCtx := NewRequestContext(req, w)
		assert.NotNil(t, reqCtx)
		assert.Equal(t, req, reqCtx.r)
		assert.Equal(t, w, reqCtx.w)
		assert.False(t, reqCtx.responseHandled)
	})
}

func TestRequestContext_Request(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	reqCtx := NewRequestContext(req, w)

	t.Run("returns original request", func(t *testing.T) {
		assert.Equal(t, req, reqCtx.Request())
	})
}

func TestRequestContext_ResponseWriter(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	reqCtx := NewRequestContext(req, w)

	t.Run("returns original response writer", func(t *testing.T) {
		assert.Equal(t, w, reqCtx.ResponseWriter())
	})
}

func TestRequestContext_MarkHandled(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	reqCtx := NewRequestContext(req, w)

	t.Run("marks response as handled", func(t *testing.T) {
		assert.False(t, reqCtx.ResponseHandled())
		reqCtx.MarkHandled()
		assert.True(t, reqCtx.ResponseHandled())
	})
}

func TestRequestContext_ResponseHandled(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	reqCtx := NewRequestContext(req, w)

	t.Run("initially not handled", func(t *testing.T) {
		assert.False(t, reqCtx.ResponseHandled())
	})

	t.Run("reports handled after marking", func(t *testing.T) {
		reqCtx.MarkHandled()
		assert.True(t, reqCtx.ResponseHandled())
	})
}

func TestRouteInfo(t *testing.T) {
	t.Run("creates and stores route info", func(t *testing.T) {
		routeInfo := RouteInfo{
			Params: map[string]string{
				"id":   "123",
				"name": "test",
			},
			Func: registry.ID{
				NS:   "test-ns",
				Name: "test-handler",
			},
			MatchedURI: "/test/123",
		}

		assert.Equal(t, "123", routeInfo.Params["id"])
		assert.Equal(t, "test", routeInfo.Params["name"])
		assert.Equal(t, "test-ns", routeInfo.Func.NS)
		assert.Equal(t, "test-handler", routeInfo.Func.Name)
		assert.Equal(t, "/test/123", routeInfo.MatchedURI)
	})
}

func TestContextKeys(t *testing.T) {
	t.Run("request context key is unique", func(t *testing.T) {
		assert.Equal(t, "http.request", requestCtx.Name)
	})

	t.Run("route context key is unique", func(t *testing.T) {
		assert.Equal(t, "http.route", routeCtx.Name)
	})

	t.Run("context keys are different", func(t *testing.T) {
		assert.NotEqual(t, requestCtx, routeCtx)
	})
}

func TestContextIntegration(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	reqCtx := NewRequestContext(req, w)
	routeInfo := &RouteInfo{
		Params: map[string]string{"id": "123"},
		Func: registry.ID{
			NS:   "test-ns",
			Name: "test-handler",
		},
		MatchedURI: "/test/123",
	}

	t.Run("stores and retrieves from context", func(t *testing.T) {
		ctx := context.Background()
		ctx, _ = ctxapi.OpenFrameContext(ctx)

		fc := ctxapi.FrameFromContext(ctx)
		require.NotNil(t, fc)

		err := fc.SetMultiple(
			ctxapi.Pair{Key: requestCtx, Value: reqCtx},
			ctxapi.Pair{Key: routeCtx, Value: routeInfo},
		)
		require.NoError(t, err)

		// Retrieve and verify RequestContext
		retrievedReqCtx, ok := GetRequestContext(ctx)
		require.True(t, ok)
		assert.Equal(t, req, retrievedReqCtx.Request())
		assert.Equal(t, w, retrievedReqCtx.ResponseWriter())

		// Retrieve and verify RouteInfo
		retrievedRouteInfo, ok := GetRouteInfo(ctx)
		require.True(t, ok)
		assert.Equal(t, routeInfo.Params, retrievedRouteInfo.Params)
		assert.Equal(t, routeInfo.Func, retrievedRouteInfo.Func)
		assert.Equal(t, routeInfo.MatchedURI, retrievedRouteInfo.MatchedURI)
	})
}

func TestRequestContext_WithCustomResponseWriter(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	w := &mockResponseWriter{}
	reqCtx := NewRequestContext(req, w)

	t.Run("works with custom response writer", func(t *testing.T) {
		assert.False(t, w.written)
		_, err := reqCtx.ResponseWriter().Write([]byte("test"))
		assert.NoError(t, err)
		assert.True(t, w.written)
	})
}

func TestRequestContext_Pooling(t *testing.T) {
	req1 := httptest.NewRequest("GET", "/test1", nil)
	req2 := httptest.NewRequest("POST", "/test2", nil)
	w1 := httptest.NewRecorder()
	w2 := httptest.NewRecorder()

	reqCtx := NewRequestContext(req1, w1)
	reqCtx.MarkHandled()

	t.Run("SetRequest replaces request", func(t *testing.T) {
		reqCtx.SetRequest(req2)
		assert.Equal(t, req2, reqCtx.Request())
	})

	t.Run("SetResponseWriter replaces writer", func(t *testing.T) {
		reqCtx.SetResponseWriter(w2)
		assert.Equal(t, w2, reqCtx.ResponseWriter())
	})

	t.Run("ResetHandled clears handled flag", func(t *testing.T) {
		assert.True(t, reqCtx.ResponseHandled())
		reqCtx.ResetHandled()
		assert.False(t, reqCtx.ResponseHandled())
	})
}

func TestContextKeyAccessors(t *testing.T) {
	t.Run("RequestKey returns request key", func(t *testing.T) {
		key := RequestKey()
		assert.NotNil(t, key)
		assert.Equal(t, "http.request", key.Name)
	})

	t.Run("ServerIDKey returns server ID key", func(t *testing.T) {
		key := ServerIDKey()
		assert.NotNil(t, key)
		assert.Equal(t, "http.server_id", key.Name)
	})

	t.Run("ServerKey returns server key", func(t *testing.T) {
		key := ServerKey()
		assert.NotNil(t, key)
		assert.Equal(t, "http.server", key.Name)
	})
}

func TestGetRouteLabel(t *testing.T) {
	t.Run("returns empty when no frame context", func(t *testing.T) {
		ctx := context.Background()
		label, ok := GetRouteLabel(ctx)
		assert.False(t, ok)
		assert.Empty(t, label)
	})

	t.Run("returns empty when not set", func(t *testing.T) {
		ctx := context.Background()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		label, ok := GetRouteLabel(ctx)
		assert.False(t, ok)
		assert.Empty(t, label)
	})

	t.Run("returns label when set", func(t *testing.T) {
		ctx := context.Background()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		err := SetRouteLabel(ctx, "GET /api/users")
		require.NoError(t, err)

		label, ok := GetRouteLabel(ctx)
		assert.True(t, ok)
		assert.Equal(t, "GET /api/users", label)
	})
}

func TestSetRouteInfo(t *testing.T) {
	t.Run("returns nil when no frame context", func(t *testing.T) {
		ctx := context.Background()
		err := SetRouteInfo(ctx, &RouteInfo{})
		assert.NoError(t, err)
	})

	t.Run("sets and retrieves route info", func(t *testing.T) {
		ctx := context.Background()
		ctx, _ = ctxapi.OpenFrameContext(ctx)

		info := &RouteInfo{
			Params:     map[string]string{"id": "42"},
			MatchedURI: "/users/42",
		}
		err := SetRouteInfo(ctx, info)
		require.NoError(t, err)

		retrieved, ok := GetRouteInfo(ctx)
		assert.True(t, ok)
		assert.Equal(t, info, retrieved)
	})
}

func TestSetRouteLabel(t *testing.T) {
	t.Run("returns nil when no frame context", func(t *testing.T) {
		ctx := context.Background()
		err := SetRouteLabel(ctx, "test")
		assert.NoError(t, err)
	})

	t.Run("sets label successfully", func(t *testing.T) {
		ctx := context.Background()
		ctx, _ = ctxapi.OpenFrameContext(ctx)

		err := SetRouteLabel(ctx, "POST /api/data")
		require.NoError(t, err)

		label, ok := GetRouteLabel(ctx)
		assert.True(t, ok)
		assert.Equal(t, "POST /api/data", label)
	})
}

func TestSetServerID(t *testing.T) {
	t.Run("returns nil when no frame context", func(t *testing.T) {
		ctx := context.Background()
		err := SetServerID(ctx, "server-1")
		assert.NoError(t, err)
	})

	t.Run("sets server ID successfully", func(t *testing.T) {
		ctx := context.Background()
		ctx, _ = ctxapi.OpenFrameContext(ctx)

		err := SetServerID(ctx, "server-main")
		require.NoError(t, err)

		fc := ctxapi.FrameFromContext(ctx)
		val, ok := fc.Get(serverIDCtx)
		assert.True(t, ok)
		assert.Equal(t, "server-main", val)
	})
}

func TestSetServerHost(t *testing.T) {
	t.Run("returns nil when no frame context", func(t *testing.T) {
		ctx := context.Background()
		err := SetServerHost(ctx, "localhost:8080")
		assert.NoError(t, err)
	})

	t.Run("sets server host successfully", func(t *testing.T) {
		ctx := context.Background()
		ctx, _ = ctxapi.OpenFrameContext(ctx)

		err := SetServerHost(ctx, "api.example.com")
		require.NoError(t, err)

		fc := ctxapi.FrameFromContext(ctx)
		val, ok := fc.Get(serverHostCtx)
		assert.True(t, ok)
		assert.Equal(t, "api.example.com", val)
	})
}

type mockMiddlewareRegistry struct {
	middlewares map[string]MiddlewareCreator
}

func (m *mockMiddlewareRegistry) Register(name string, creator MiddlewareCreator) error {
	m.middlewares[name] = creator
	return nil
}

func (m *mockMiddlewareRegistry) Unregister(name string) error {
	delete(m.middlewares, name)
	return nil
}

func (m *mockMiddlewareRegistry) CreateMiddleware(name string, _ map[string]string) (func(http.Handler) http.Handler, error) {
	if creator, ok := m.middlewares[name]; ok {
		return creator(nil), nil
	}
	return func(h http.Handler) http.Handler { return h }, nil
}

func TestMiddlewareRegistry(t *testing.T) {
	t.Run("WithMiddlewareRegistry with no app context", func(t *testing.T) {
		ctx := context.Background()
		reg := &mockMiddlewareRegistry{middlewares: make(map[string]MiddlewareCreator)}
		result := WithMiddlewareRegistry(ctx, reg)
		assert.Equal(t, ctx, result)
	})

	t.Run("GetMiddlewareRegistry with no app context", func(t *testing.T) {
		ctx := context.Background()
		result := GetMiddlewareRegistry(ctx)
		assert.Nil(t, result)
	})

	t.Run("WithMiddlewareRegistry and GetMiddlewareRegistry", func(t *testing.T) {
		ctx := context.Background()
		appCtx := ctxapi.NewAppContext()
		ctx = ctxapi.WithAppContext(ctx, appCtx)

		reg := &mockMiddlewareRegistry{middlewares: make(map[string]MiddlewareCreator)}
		ctx = WithMiddlewareRegistry(ctx, reg)

		retrieved := GetMiddlewareRegistry(ctx)
		assert.NotNil(t, retrieved)
		assert.Equal(t, reg, retrieved)
	})

	t.Run("WithMiddlewareRegistry only sets once", func(t *testing.T) {
		ctx := context.Background()
		appCtx := ctxapi.NewAppContext()
		ctx = ctxapi.WithAppContext(ctx, appCtx)

		reg1 := &mockMiddlewareRegistry{middlewares: make(map[string]MiddlewareCreator)}
		reg2 := &mockMiddlewareRegistry{middlewares: make(map[string]MiddlewareCreator)}

		ctx = WithMiddlewareRegistry(ctx, reg1)
		ctx = WithMiddlewareRegistry(ctx, reg2)

		retrieved := GetMiddlewareRegistry(ctx)
		assert.Equal(t, reg1, retrieved)
	})

	t.Run("GetMiddlewareRegistry with non-registry value", func(t *testing.T) {
		ctx := context.Background()
		appCtx := ctxapi.NewAppContext()
		ctx = ctxapi.WithAppContext(ctx, appCtx)
		appCtx.With(middlewareRegistry, "not a registry")

		result := GetMiddlewareRegistry(ctx)
		assert.Nil(t, result)
	})
}
