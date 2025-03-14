package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestMiddlewareFactory(t *testing.T) {
	logger := zap.NewNop()

	t.Run("create middleware factory", func(t *testing.T) {
		factory := NewDefaultMiddlewareFactory()
		assert.NotNil(t, factory)
		assert.Empty(t, factory.middlewareMap)

		// Create with logger option
		factory = NewDefaultMiddlewareFactory(WithLogger(logger))
		assert.NotNil(t, factory)
		assert.Equal(t, logger, factory.logger)
	})

	t.Run("with middleware", func(t *testing.T) {
		// Create a simple test middleware
		testMiddleware := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Test", "true")
				next.ServeHTTP(w, r)
			})
		}

		factory := NewDefaultMiddlewareFactory(
			WithMiddleware("test", testMiddleware),
		)

		// Test the middleware
		handler := factory.CreateMiddleware("test", nil)
		assert.NotNil(t, handler)

		// Test that it works
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)

		// Create a test handler that the middleware will wrap
		testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// Apply the middleware to the test handler
		wrappedHandler := handler(testHandler)
		wrappedHandler.ServeHTTP(rec, req)

		// Check that our middleware added the header
		assert.Equal(t, "true", rec.Header().Get("X-Test"))
		assert.Equal(t, http.StatusOK, rec.Code)

		// Try to get non-existent middleware
		handler = factory.CreateMiddleware("nonexistent", nil)
		assert.Nil(t, handler)
	})

	t.Run("with middleware creator", func(t *testing.T) {
		// Create a configurable middleware creator
		testCreator := func(options map[string]string) func(http.Handler) http.Handler {
			return func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					value := "default"
					if v, exists := options["value"]; exists {
						value = v
					}
					w.Header().Set("X-Test-Value", value)
					next.ServeHTTP(w, r)
				})
			}
		}

		factory := NewDefaultMiddlewareFactory(
			WithMiddlewareCreator("configurable", testCreator),
		)

		// Test with default options
		handler := factory.CreateMiddleware("configurable", nil)
		assert.NotNil(t, handler)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		wrappedHandler := handler(testHandler)
		wrappedHandler.ServeHTTP(rec, req)

		assert.Equal(t, "default", rec.Header().Get("X-Test-Value"))

		// Test with custom options
		handler = factory.CreateMiddleware("configurable", map[string]string{
			"value": "custom",
		})
		assert.NotNil(t, handler)

		rec = httptest.NewRecorder()
		wrappedHandler = handler(testHandler)
		wrappedHandler.ServeHTTP(rec, req)

		assert.Equal(t, "custom", rec.Header().Get("X-Test-Value"))
	})

	t.Run("middleware creator returning nil", func(t *testing.T) {
		factory := NewDefaultMiddlewareFactory(
			WithLogger(logger),
			WithMiddlewareCreator("nil-creator", func(options map[string]string) func(http.Handler) http.Handler {
				return nil
			}),
		)

		handler := factory.CreateMiddleware("nil-creator", nil)
		assert.Nil(t, handler)
	})
}
