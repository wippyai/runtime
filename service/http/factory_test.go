package http

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	apifsLib "github.com/ponyruntime/pony/api/fs"
	"github.com/ponyruntime/pony/api/function"
	"github.com/ponyruntime/pony/api/payload"
	apiregistry "github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	config "github.com/ponyruntime/pony/api/service/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SimpleFunctionRegistry for testing
type SimpleFunctionRegistry struct {
	functions map[apiregistry.ID]function.Func
}

func NewSimpleFunctionRegistry() *SimpleFunctionRegistry {
	return &SimpleFunctionRegistry{
		functions: make(map[apiregistry.ID]function.Func),
	}
}

func (r *SimpleFunctionRegistry) Register(id apiregistry.ID, fn function.Func) {
	r.functions[id] = fn
}

func (r *SimpleFunctionRegistry) Call(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
	fn, exists := r.functions[task.ID]
	if !exists {
		return nil, fmt.Errorf("function not found: %s", task.ID)
	}
	return fn(ctx, task)
}

// MockFS implements apifsLib.FS for testing
type MockFS struct {
	rootDir string
}

func NewMockFS(rootDir string) *MockFS {
	return &MockFS{
		rootDir: rootDir,
	}
}

// Implement ReadFS methods
func (fs *MockFS) Open(name string) (fs.File, error) {
	fullPath := filepath.Join(fs.rootDir, name)
	return os.Open(fullPath)
}

func (fs *MockFS) Stat(name string) (fs.FileInfo, error) {
	fullPath := filepath.Join(fs.rootDir, name)
	return os.Stat(fullPath)
}

func (fs *MockFS) ReadDir(name string) ([]fs.DirEntry, error) {
	fullPath := filepath.Join(fs.rootDir, name)
	return os.ReadDir(fullPath)
}

// Implement WriteFS methods
func (fs *MockFS) OpenFile(name string, flag int, perm fs.FileMode) (apifsLib.File, error) {
	fullPath := filepath.Join(fs.rootDir, name)
	file, err := os.OpenFile(fullPath, flag, perm)
	if err != nil {
		return nil, err
	}
	return &OsFileWrapper{file}, nil
}

func (fs *MockFS) Remove(name string) error {
	fullPath := filepath.Join(fs.rootDir, name)
	return os.Remove(fullPath)
}

func (fs *MockFS) Mkdir(name string, perm fs.FileMode) error {
	fullPath := filepath.Join(fs.rootDir, name)
	return os.Mkdir(fullPath, perm)
}

// OsFileWrapper wraps an os.File to implement apifsLib.File
type OsFileWrapper struct {
	*os.File
}

func (f *OsFileWrapper) Sync() error {
	return f.File.Sync()
}

// SimpleFSRegistry for testing
type SimpleFSRegistry struct {
	filesystems map[string]apifsLib.FS
}

func NewSimpleFSRegistry() *SimpleFSRegistry {
	return &SimpleFSRegistry{
		filesystems: make(map[string]apifsLib.FS),
	}
}

func (r *SimpleFSRegistry) Register(name string, filesystem apifsLib.FS) {
	r.filesystems[name] = filesystem
}

func (r *SimpleFSRegistry) GetFS(name string) (apifsLib.FS, bool) {
	f, exists := r.filesystems[name]
	return f, exists
}

// createFactoryTempDir creates a temporary directory with files
func createFactoryTempDir(t *testing.T, files map[string]string) (string, func()) {
	dir, err := os.MkdirTemp("", "ponytest-*")
	require.NoError(t, err)

	for path, content := range files {
		fullPath := filepath.Join(dir, path)

		// Ensure directory exists
		err := os.MkdirAll(filepath.Dir(fullPath), 0755)
		require.NoError(t, err)

		// Write file content
		//nolint:gosec // used in tests
		err = os.WriteFile(fullPath, []byte(content), 0644)
		require.NoError(t, err)
	}

	cleanup := func() {
		err := os.RemoveAll(dir)
		if err != nil {
			t.Logf("Failed to remove temp dir %s: %v", dir, err)
		}
	}

	return dir, cleanup
}

// Function Registry Tests
func TestEndpointFactory_CreateHandler(t *testing.T) {
	// Setup function registry
	registry := NewSimpleFunctionRegistry()
	factory, err := NewEndpointFactory(registry)
	require.NoError(t, err)

	// Create test endpoint config
	cfg := &config.EndpointConfig{
		Meta: map[string]interface{}{
			config.RouterID: "test:router1",
		},
		Method: "GET",
		Path:   "/test",
		Func:   apiregistry.ID{NS: "test", Name: "func1"},
	}

	t.Run("successful handler creation", func(t *testing.T) {
		// Register test function
		registry.Register(cfg.Func, func(ctx context.Context, _ runtime.Task) (chan *runtime.Result, error) {
			resultCh := make(chan *runtime.Result, 1)

			// Get request context
			rctx := ctx.Value(config.RequestCtx).(*config.RequestContext)

			// send response
			rctx.MarkHandled()
			w := rctx.ResponseWriter()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("success"))

			// Return success
			resultCh <- &runtime.Result{
				Value: payload.New("success"),
				Error: nil,
			}
			close(resultCh)
			return resultCh, nil
		})

		// Create handler
		handler, err := factory.CreateHandler(context.Background(), cfg)
		require.NoError(t, err)

		// Test handler
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "success", w.Body.String())
	})

	t.Run("invalid config", func(t *testing.T) {
		invalidCfg := &config.EndpointConfig{
			// Missing required fields
		}

		// Create handler should fail with invalid config
		_, err := factory.CreateHandler(context.Background(), invalidCfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid endpoint config")
	})

	t.Run("function registry error", func(t *testing.T) {
		// Register test function that returns an error
		registry.Register(cfg.Func, func(_ context.Context, _ runtime.Task) (chan *runtime.Result, error) {
			return nil, fmt.Errorf("function error")
		})

		// Create handler
		handler, err := factory.CreateHandler(context.Background(), cfg)
		require.NoError(t, err)

		// Test handler
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "function error")
	})
}

func TestNewEndpointFactory(t *testing.T) {
	t.Run("with valid registry", func(t *testing.T) {
		registry := NewSimpleFunctionRegistry()
		factory, err := NewEndpointFactory(registry)

		assert.NoError(t, err)
		assert.NotNil(t, factory)
	})

	t.Run("with nil registry", func(t *testing.T) {
		factory, err := NewEndpointFactory(nil)

		assert.Error(t, err)
		assert.Nil(t, factory)
		assert.Contains(t, err.Error(), "function registry is required")
	})
}

// File System Registry Tests
func TestStaticFactory_CreateHandler(t *testing.T) {
	fsRegistry := NewSimpleFSRegistry()
	factory, err := NewStaticFactory(fsRegistry)
	require.NoError(t, err)

	// Create a temp directory with test files
	tempDir, cleanup := createFactoryTempDir(t, map[string]string{
		"index.html": "<html><body>Hello World</body></html>",
		"style.css":  "body { color: red; }",
	})
	defer cleanup()

	// Create and register a filesystem adapter
	mockFS := NewMockFS(tempDir)
	fsRegistry.Register("test:files", mockFS)

	t.Run("standard static handler", func(t *testing.T) {
		cfg := &config.StaticConfig{
			Meta: map[string]interface{}{
				config.ServerID: "test:server1",
			},
			Path:      "/static",
			Directory: "/static", // Add Directory field to match Path
			FS:        apiregistry.ID{NS: "test", Name: "files"},
		}

		handler, err := factory.CreateHandler(context.Background(), cfg)
		require.NoError(t, err)
		assert.NotNil(t, handler)

		// Test handler
		req := httptest.NewRequest("GET", "http://example.com/static/style.css", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "body { color: red; }", w.Body.String())
	})

	t.Run("SPA handler", func(t *testing.T) {
		cfg := &config.StaticConfig{
			Meta: map[string]interface{}{
				config.ServerID: "test:server1",
			},
			Path: "/app",
			FS:   apiregistry.ID{NS: "test", Name: "files"},
			Options: config.StaticOptions{
				SPA:       true,
				IndexFile: "index.html",
			},
		}

		handler, err := factory.CreateHandler(context.Background(), cfg)
		require.NoError(t, err)
		assert.NotNil(t, handler)

		// Test handler with non-existent route (should serve index.html)
		req := httptest.NewRequest("GET", "http://example.com/app/nonexistent", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
		assert.Equal(t, "<html><body>Hello World</body></html>", w.Body.String())
	})

	t.Run("invalid config", func(t *testing.T) {
		invalidCfg := &config.StaticConfig{
			// Missing required fields
		}

		_, err := factory.CreateHandler(context.Background(), invalidCfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid static config")
	})

	t.Run("filesystem not found", func(t *testing.T) {
		cfg := &config.StaticConfig{
			Meta: map[string]interface{}{
				config.ServerID: "test:server1",
			},
			Path: "/static",
			FS:   apiregistry.ID{NS: "test", Name: "nonexistent"},
		}

		_, err := factory.CreateHandler(context.Background(), cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "filesystem not found")
	})

	t.Run("SPA without index file", func(t *testing.T) {
		cfg := &config.StaticConfig{
			Meta: map[string]interface{}{
				config.ServerID: "test:server1",
			},
			Path: "/app",
			FS:   apiregistry.ID{NS: "test", Name: "files"},
			Options: config.StaticOptions{
				SPA: true,
				// No index file specified
			},
		}

		_, err := factory.CreateHandler(context.Background(), cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "index file must be specified for SPA mode")
	})
}

func TestNewStaticFactory(t *testing.T) {
	t.Run("with valid registry", func(t *testing.T) {
		fsRegistry := NewSimpleFSRegistry()
		factory, err := NewStaticFactory(fsRegistry)

		assert.NoError(t, err)
		assert.NotNil(t, factory)
	})

	t.Run("with nil registry", func(t *testing.T) {
		factory, err := NewStaticFactory(nil)

		assert.Error(t, err)
		assert.Nil(t, factory)
		assert.Contains(t, err.Error(), "filesystem registry is required")
	})
}

func TestSPAHandler(t *testing.T) {
	// Create a temp directory with test files
	tempDir, cleanup := createFactoryTempDir(t, map[string]string{
		"index.html": "<html><body>Hello World</body></html>",
		"style.css":  "body { color: red; }",
	})
	defer cleanup()

	// Create a filesystem adapter
	mockFS := NewMockFS(tempDir)

	t.Run("direct file exists", func(t *testing.T) {
		handler := NewSPAHandler(mockFS, "index.html")

		// Create test request for existing file
		req := httptest.NewRequest("GET", "http://example.com/style.css", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "body { color: red; }", w.Body.String())
	})

	t.Run("fallback to index", func(t *testing.T) {
		handler := NewSPAHandler(mockFS, "index.html")

		// Create test request for non-existent file
		req := httptest.NewRequest("GET", "http://example.com/app/route", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
		assert.Equal(t, "<html><body>Hello World</body></html>", w.Body.String())
	})

	t.Run("index file not found", func(t *testing.T) {
		handler := NewSPAHandler(mockFS, "missing.html")

		// Create test request
		req := httptest.NewRequest("GET", "http://example.com/app/route", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "index file not found")
	})
}

func TestWrapWithCacheControl(t *testing.T) {
	mockHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := wrapWithCacheControl(mockHandler, "public, max-age=3600")

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "public, max-age=3600", w.Header().Get("Cache-Control"))
}

func TestServerFactory(t *testing.T) {
	// Create middleware factory for server factory
	middlewareFactory := NewDefaultMiddlewareFactory()

	factory := NewServerFactory(middlewareFactory)

	cfg := &config.ServerConfig{
		Addr: ":8080",
	}

	// Create server with id
	serverID := apiregistry.ID{NS: "test", Name: "server1"}
	server, err := factory.CreateServer(serverID, cfg)
	require.NoError(t, err)
	assert.NotNil(t, server)

	// Verify server has correct config
	serverSvc, ok := server.(*ServerService)
	require.True(t, ok)
	assert.Equal(t, cfg, serverSvc.config)
	assert.Equal(t, serverID, serverSvc.id)
}
