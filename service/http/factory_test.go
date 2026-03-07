// SPDX-License-Identifier: MPL-2.0

package http

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	apifsLib "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	apiregistry "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	config "github.com/wippyai/runtime/api/service/http"
	"go.uber.org/zap"
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

func (r *SimpleFunctionRegistry) Call(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
	fn, exists := r.functions[task.ID]
	if !exists {
		return nil, fmt.Errorf("function not found: %s", task.ID)
	}

	// Open a new frame context and apply task context pairs
	// This mimics what the real function registry does
	execCtx, frame := ctxapi.OpenFrameContext(ctx)
	if frame != nil {
		for _, pair := range task.Context {
			_ = frame.Set(pair.Key, pair.Value)
		}
		frame.Seal()
	}

	return fn(execCtx, task)
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

func (fs *MockFS) Lstat(name string) (fs.FileInfo, error) {
	fullPath := filepath.Join(fs.rootDir, name)
	return os.Lstat(fullPath)
}

func (fs *MockFS) Rename(oldname, newname string) error {
	return os.Rename(filepath.Join(fs.rootDir, oldname), filepath.Join(fs.rootDir, newname))
}

func (fs *MockFS) Truncate(name string, size int64) error {
	fullPath := filepath.Join(fs.rootDir, name)
	f, err := os.OpenFile(fullPath, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Truncate(size)
}

func (fs *MockFS) Chtimes(name string, atime, mtime time.Time) error {
	fullPath := filepath.Join(fs.rootDir, name)
	return os.Chtimes(fullPath, atime, mtime)
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
		Meta: map[string]any{
			config.RouterID: "test:router1",
		},
		Method: "GET",
		Path:   "/test",
		Func:   apiregistry.NewID("test", "func1"),
	}

	t.Run("successful handler creation", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		// Register test function
		registry.Register(cfg.Func, func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
			// Get request context from FrameContext
			rctx, ok := config.GetRequestContext(ctx)
			if !ok {
				panic("RequestContext not found")
			}

			// send response
			rctx.MarkHandled()
			w := rctx.ResponseWriter()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("success"))

			// Return success
			return &runtime.Result{
				Value: payload.New("success"),
				Error: nil,
			}, nil
		})

		// Create handler
		handler, err := factory.CreateHandler(context.Background(), cfg)
		require.NoError(t, err)

		// Test handler
		w := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), "GET", "http://example.com/test", nil)

		// Create FrameContext like the HTTP server does
		ctxWithFrame, _ := ctxapi.OpenFrameContext(ctx)
		req = req.WithContext(ctxWithFrame)
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
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid endpoint config")
	})

	t.Run("function registry error", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		// Register test function that returns an error
		registry.Register(cfg.Func, func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return nil, fmt.Errorf("function error")
		})

		// Create handler
		handler, err := factory.CreateHandler(context.Background(), cfg)
		require.NoError(t, err)

		// Test handler
		w := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), "GET", "http://example.com/test", nil)

		// Create FrameContext like the HTTP server does
		ctxWithFrame, _ := ctxapi.OpenFrameContext(ctx)
		req = req.WithContext(ctxWithFrame)
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

		require.Error(t, err)
		assert.Nil(t, factory)
		assert.Contains(t, err.Error(), "function registry is required")
	})
}

// File System Registry Tests
func TestStaticFactory_CreateHandler(t *testing.T) {
	fsRegistry := NewSimpleFSRegistry()
	middlewareFactory := NewMiddlewareRegistry(nil)
	factory, err := NewStaticFactory(fsRegistry, middlewareFactory)
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
			Meta: map[string]any{
				config.ServerID: "test:server1",
			},
			Path:      "/static",
			Directory: "/static", // Add Directory field to match Path
			FS:        apiregistry.NewID("test", "files"),
		}

		handler, err := factory.CreateHandler(context.Background(), cfg)
		require.NoError(t, err)
		assert.NotNil(t, handler)

		// Test handler
		req := httptest.NewRequestWithContext(context.Background(), "GET", "http://example.com/static/style.css", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "body { color: red; }", w.Body.String())
	})

	t.Run("SPA handler", func(t *testing.T) {
		cfg := &config.StaticConfig{
			Meta: map[string]any{
				config.ServerID: "test:server1",
			},
			Path: "/app",
			FS:   apiregistry.NewID("test", "files"),
			StaticOptions: config.StaticOptions{
				SPA:       true,
				IndexFile: "index.html",
			},
		}

		handler, err := factory.CreateHandler(context.Background(), cfg)
		require.NoError(t, err)
		assert.NotNil(t, handler)

		// Test handler with non-existent route (should serve index.html)
		req := httptest.NewRequestWithContext(context.Background(), "GET", "http://example.com/app/nonexistent", nil)
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
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid static config")
	})

	t.Run("filesystem not found", func(t *testing.T) {
		cfg := &config.StaticConfig{
			Meta: map[string]any{
				config.ServerID: "test:server1",
			},
			Path: "/static",
			FS:   apiregistry.NewID("test", "nonexistent"),
		}

		_, err := factory.CreateHandler(context.Background(), cfg)
		require.Error(t, err)
		var apiErr apierror.Error
		ok := errors.As(err, &apiErr)
		require.True(t, ok)
		assert.Contains(t, apiErr.Error(), "filesystem not found")
		assert.Equal(t, "test:nonexistent", apiErr.Details().GetString("filesystem_id", ""))
	})

	t.Run("SPA without index file", func(t *testing.T) {
		cfg := &config.StaticConfig{
			Meta: map[string]any{
				config.ServerID: "test:server1",
			},
			Path: "/app",
			FS:   apiregistry.NewID("test", "files"),
			StaticOptions: config.StaticOptions{
				SPA: true,
				// No index file specified
			},
		}

		_, err := factory.CreateHandler(context.Background(), cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "index file must be specified for SPA mode")
	})
}

func TestNewStaticFactory(t *testing.T) {
	t.Run("with valid registry", func(t *testing.T) {
		fsRegistry := NewSimpleFSRegistry()
		middlewareFactory := NewMiddlewareRegistry(nil)
		factory, err := NewStaticFactory(fsRegistry, middlewareFactory)

		assert.NoError(t, err)
		assert.NotNil(t, factory)
	})

	t.Run("with nil registry", func(t *testing.T) {
		middlewareFactory := NewMiddlewareRegistry(nil)
		factory, err := NewStaticFactory(nil, middlewareFactory)

		require.Error(t, err)
		assert.Nil(t, factory)
		assert.Contains(t, err.Error(), "filesystem registry is required")
	})

	t.Run("with nil middleware factory", func(t *testing.T) {
		fsRegistry := NewSimpleFSRegistry()
		factory, err := NewStaticFactory(fsRegistry, nil)

		require.Error(t, err)
		assert.Nil(t, factory)
		assert.Contains(t, err.Error(), "middleware factory is required")
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
		req := httptest.NewRequestWithContext(context.Background(), "GET", "http://example.com/style.css", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "body { color: red; }", w.Body.String())
	})

	t.Run("fallback to index", func(t *testing.T) {
		handler := NewSPAHandler(mockFS, "index.html")

		// Create test request for non-existent file
		req := httptest.NewRequestWithContext(context.Background(), "GET", "http://example.com/app/route", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
		assert.Equal(t, "<html><body>Hello World</body></html>", w.Body.String())
	})

	t.Run("index file not found", func(t *testing.T) {
		handler := NewSPAHandler(mockFS, "missing.html")

		// Create test request
		req := httptest.NewRequestWithContext(context.Background(), "GET", "http://example.com/app/route", nil)
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

	req := httptest.NewRequestWithContext(context.Background(), "GET", "http://example.com/", nil)
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "public, max-age=3600", w.Header().Get("Cache-Control"))
}

func TestServerFactory(t *testing.T) {
	// Create middleware factory for server factory
	middlewareFactory := NewMiddlewareRegistry(zap.NewNop())

	factory := NewServerFactory(middlewareFactory)

	cfg := &config.ServerConfig{
		Addr: ":8080",
	}

	// Create server with ID
	serverID := apiregistry.NewID("test", "server1")
	server, err := factory.CreateServer(serverID, cfg)
	require.NoError(t, err)
	assert.NotNil(t, server)

	// Verify server has correct config
	serverSvc, ok := server.(*ServerService)
	require.True(t, ok)
	assert.Equal(t, cfg, serverSvc.config)
	assert.Equal(t, serverID, serverSvc.id)
}
