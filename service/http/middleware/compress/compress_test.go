package compress

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateCompressMiddleware(t *testing.T) {
	t.Run("compress with default settings", func(t *testing.T) {
		middleware := CreateCompressMiddleware(map[string]string{})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(strings.Repeat("test data ", 200)))
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"))
	})

	t.Run("compress with fastest level", func(t *testing.T) {
		middleware := CreateCompressMiddleware(map[string]string{
			OptionLevel: "fastest",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("test ", 500)))
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"))
	})

	t.Run("compress with best level", func(t *testing.T) {
		middleware := CreateCompressMiddleware(map[string]string{
			OptionLevel: "best",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("compress me ", 300)))
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"))
	})

	t.Run("no compression for small responses", func(t *testing.T) {
		middleware := CreateCompressMiddleware(map[string]string{
			OptionMinLength: "1024",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("small"))
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, "", w.Header().Get("Content-Encoding"))
		assert.Equal(t, "small", w.Body.String())
	})

	t.Run("compress with custom min length", func(t *testing.T) {
		middleware := CreateCompressMiddleware(map[string]string{
			OptionMinLength: "10",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("this is longer than 10 bytes"))
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"))
	})

	t.Run("no compression without Accept-Encoding", func(t *testing.T) {
		middleware := CreateCompressMiddleware(map[string]string{})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("data ", 500)))
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, "", w.Header().Get("Content-Encoding"))
	})

	t.Run("verify compressed content is valid gzip", func(t *testing.T) {
		middleware := CreateCompressMiddleware(map[string]string{})

		testData := strings.Repeat("compress this data ", 100)
		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(testData))
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		require.Equal(t, "gzip", w.Header().Get("Content-Encoding"))

		// Decompress and verify
		reader, err := gzip.NewReader(w.Body)
		require.NoError(t, err)
		defer func() { _ = reader.Close() }()

		decompressed, err := io.ReadAll(reader)
		require.NoError(t, err)
		assert.Equal(t, testData, string(decompressed))
	})

	t.Run("invalid compression level defaults to default", func(t *testing.T) {
		middleware := CreateCompressMiddleware(map[string]string{
			OptionLevel: "invalid",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("test ", 500)))
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"))
	})

	t.Run("invalid min length defaults to 1024", func(t *testing.T) {
		middleware := CreateCompressMiddleware(map[string]string{
			OptionMinLength: "invalid",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("short"))
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		// Should not compress (below default 1024 bytes)
		assert.Equal(t, "", w.Header().Get("Content-Encoding"))
	})

	t.Run("negative min length defaults to 1024", func(t *testing.T) {
		middleware := CreateCompressMiddleware(map[string]string{
			OptionMinLength: "-100",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("short"))
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, "", w.Header().Get("Content-Encoding"))
	})
}
