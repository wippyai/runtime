package cors

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCreateCORSMiddleware(t *testing.T) {
	t.Run("exact origin match", func(t *testing.T) {
		middleware := CreateCORSMiddleware(map[string]string{
			CORSOptionAllowOrigins: "https://example.com",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "http://api.example.com/test", nil)
		req.Header.Set("Origin", "https://example.com")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, "https://example.com", w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("wildcard origin allows all", func(t *testing.T) {
		middleware := CreateCORSMiddleware(map[string]string{
			CORSOptionAllowOrigins: "*",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "http://api.example.com/test", nil)
		req.Header.Set("Origin", "https://anything.com")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, "https://anything.com", w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("wildcard subdomain matching", func(t *testing.T) {
		middleware := CreateCORSMiddleware(map[string]string{
			CORSOptionAllowOrigins: "*.example.com",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "http://api.example.com/test", nil)
		req.Header.Set("Origin", "https://app.example.com")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, "https://app.example.com", w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("preflight OPTIONS request", func(t *testing.T) {
		middleware := CreateCORSMiddleware(map[string]string{
			CORSOptionAllowOrigins: "https://example.com",
			CORSOptionAllowMethods: "GET,POST,PUT",
			CORSOptionMaxAge:       "3600",
		})

		handler := middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			t.Fatal("should not reach handler for preflight")
		}))

		req := httptest.NewRequest("OPTIONS", "http://api.example.com/test", nil)
		req.Header.Set("Origin", "https://example.com")
		req.Header.Set("Access-Control-Request-Method", "POST")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Equal(t, "https://example.com", w.Header().Get("Access-Control-Allow-Origin"))
		assert.Equal(t, "GET,POST,PUT", w.Header().Get("Access-Control-Allow-Methods"))
		assert.Equal(t, "3600", w.Header().Get("Access-Control-Max-Age"))
	})

	t.Run("credentials support", func(t *testing.T) {
		middleware := CreateCORSMiddleware(map[string]string{
			CORSOptionAllowOrigins:     "https://example.com",
			CORSOptionAllowCredentials: "true",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "http://api.example.com/test", nil)
		req.Header.Set("Origin", "https://example.com")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
	})

	t.Run("expose headers", func(t *testing.T) {
		middleware := CreateCORSMiddleware(map[string]string{
			CORSOptionExposeHeaders: "X-Request-ID,X-Total-Count",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "http://api.example.com/test", nil)
		req.Header.Set("Origin", "https://example.com")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, "X-Request-ID,X-Total-Count", w.Header().Get("Access-Control-Expose-Headers"))
	})

	t.Run("legacy key fallback", func(t *testing.T) {
		middleware := CreateCORSMiddleware(map[string]string{
			legacyAllowOrigins:     "https://legacy.com",
			legacyAllowMethods:     "GET,POST",
			legacyAllowCredentials: "true",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "http://api.example.com/test", nil)
		req.Header.Set("Origin", "https://legacy.com")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, "https://legacy.com", w.Header().Get("Access-Control-Allow-Origin"))
		assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
	})

	t.Run("new keys take precedence over legacy", func(t *testing.T) {
		middleware := CreateCORSMiddleware(map[string]string{
			CORSOptionAllowOrigins: "https://new.com",
			legacyAllowOrigins:     "https://old.com",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "http://api.example.com/test", nil)
		req.Header.Set("Origin", "https://new.com")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, "https://new.com", w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("reject disallowed origin", func(t *testing.T) {
		middleware := CreateCORSMiddleware(map[string]string{
			CORSOptionAllowOrigins: "https://allowed.com",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "http://api.example.com/test", nil)
		req.Header.Set("Origin", "https://evil.com")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, "", w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("no origin header passes through", func(t *testing.T) {
		middleware := CreateCORSMiddleware(map[string]string{})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "http://api.example.com/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, "", w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("allow private network", func(t *testing.T) {
		middleware := CreateCORSMiddleware(map[string]string{
			CORSOptionAllowPrivateNetwk: "true",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "http://api.example.com/test", nil)
		req.Header.Set("Origin", "https://example.com")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Private-Network"))
	})

	t.Run("default values", func(t *testing.T) {
		middleware := CreateCORSMiddleware(map[string]string{})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("OPTIONS", "http://api.example.com/test", nil)
		req.Header.Set("Origin", "https://anything.com")
		req.Header.Set("Access-Control-Request-Method", "POST")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, DefaultAllowMethods, w.Header().Get("Access-Control-Allow-Methods"))
		assert.Equal(t, DefaultMaxAge, w.Header().Get("Access-Control-Max-Age"))
	})
}
