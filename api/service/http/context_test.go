package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		assert.Equal(t, "http.request", RequestCtx.Name)
	})

	t.Run("route context key is unique", func(t *testing.T) {
		assert.Equal(t, "http.route", RouteCtx.Name)
	})

	t.Run("context keys are different", func(t *testing.T) {
		assert.NotEqual(t, RequestCtx, RouteCtx)
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
		ctx = context.WithValue(ctx, RequestCtx, reqCtx)
		ctx = context.WithValue(ctx, RouteCtx, routeInfo)

		// Retrieve and verify RequestContext
		retrievedReqCtx, ok := ctx.Value(RequestCtx).(*RequestContext)
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
