// SPDX-License-Identifier: MPL-2.0

package http

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	contextapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/http"
	"go.uber.org/zap"
)

// findFreePort finds an available port on the local machine
func findFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	err = l.Close()
	if err != nil {
		return 0, err
	}

	return port, nil
}

// createServerTempDir creates a temporary directory with files
func createServerTempDir(t *testing.T, files map[string]string) (string, func()) {
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

func TestServerService_Basic(t *testing.T) {
	t.Run("create server", func(t *testing.T) {
		cfg := &config.ServerConfig{
			Addr: ":0", // Use dynamic port allocation
			Timeouts: config.TimeoutConfig{
				ReadTimeout:  5 * time.Second,
				WriteTimeout: 5 * time.Second,
				IdleTimeout:  60 * time.Second,
			},
		}

		id := registry.NewID("test", "server1")
		middleware := NewMiddlewareRegistry(zap.NewNop())
		server, err := NewServerService(id, cfg, middleware)
		require.NoError(t, err)

		assert.NotNil(t, server)
		assert.Equal(t, cfg, server.config)
		assert.Equal(t, id, server.id)
	})

	t.Run("update config", func(t *testing.T) {
		port, err := findFreePort()
		require.NoError(t, err)

		cfg := &config.ServerConfig{
			Addr: fmt.Sprintf("127.0.0.1:%d", port),
			Timeouts: config.TimeoutConfig{
				ReadTimeout: 5 * time.Second,
			},
		}

		id := registry.NewID("test", "server1")
		middleware := NewMiddlewareRegistry(zap.NewNop())
		server, err := NewServerService(id, cfg, middleware)
		require.NoError(t, err)

		// Update config before starting server
		newCfg := &config.ServerConfig{
			Addr: fmt.Sprintf("127.0.0.1:%d", port), // Same address
			Timeouts: config.TimeoutConfig{
				ReadTimeout:  10 * time.Second,
				WriteTimeout: 10 * time.Second,
			},
		}
		err = server.UpdateConfig(newCfg)
		assert.NoError(t, err)
		assert.Equal(t, newCfg, server.config)

		// Try changing address while not started - should work
		port2, err := findFreePort()
		require.NoError(t, err)
		newCfg.Addr = fmt.Sprintf("127.0.0.1:%d", port2)
		err = server.UpdateConfig(newCfg)
		assert.NoError(t, err)

		// Serve the server with a timeout context
		rootCtx := contextapi.NewRootContext()
		ctx, cancel := context.WithTimeout(rootCtx, 5*time.Second)
		defer cancel()

		// Serve the server and wait for it to be ready
		statusCh, startErr := server.Start(ctx)
		require.NoError(t, startErr)
		require.NotNil(t, statusCh)

		// Give it a moment to fully initialize
		time.Sleep(500 * time.Millisecond)

		// Try changing address while running - should fail
		port3, err := findFreePort()
		require.NoError(t, err)

		newRunningCfg := &config.ServerConfig{
			Addr: fmt.Sprintf("127.0.0.1:%d", port3), // New address
			Timeouts: config.TimeoutConfig{
				ReadTimeout: 20 * time.Second,
			},
		}

		// This should error because we can't change the address while server is running
		err = server.UpdateConfig(newRunningCfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot change server address while running")

		// Cleanup - stop the server
		stopErr := server.Stop(ctx)
		require.NoError(t, stopErr)
	})
}

func TestServerService_RouterOperations(t *testing.T) {
	port, err := findFreePort()
	require.NoError(t, err)

	cfg := &config.ServerConfig{
		Addr: fmt.Sprintf(":%d", port),
	}

	id := registry.NewID("test", "server1")
	middleware := NewMiddlewareRegistry(zap.NewNop())
	server, err := NewServerService(id, cfg, middleware)
	require.NoError(t, err)

	t.Run("add and delete router", func(t *testing.T) {
		routerID := registry.NewID("test", "router1")
		routerCfg := &config.RouterConfig{
			Prefix: "/api/v1",
		}

		err := server.UpsertRouter(routerID, routerCfg)
		require.NoError(t, err)

		err = server.DeleteRouter(routerID)
		require.NoError(t, err)

		// Try deleting non-existent router
		err = server.DeleteRouter(routerID)
		assert.Error(t, err)
	})

	t.Run("add and remove endpoint", func(t *testing.T) {
		// Use a different router ID to avoid conflicts
		routerID := registry.NewID("test", "router2")
		routerCfg := &config.RouterConfig{
			Prefix: "/api/v2",
		}

		err := server.UpsertRouter(routerID, routerCfg)
		require.NoError(t, err)

		endpointID := registry.NewID("test", "endpoint1")

		// Add endpoint
		err = server.UpsertEndpoint(routerID, endpointID, "/test", "GET", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		require.NoError(t, err)

		// Remove endpoint
		err = server.RemoveEndpoint(routerID, endpointID)
		require.NoError(t, err)

		// Try removing non-existent endpoint
		err = server.RemoveEndpoint(routerID, endpointID)
		assert.Error(t, err)

		// Clean up
		err = server.DeleteRouter(routerID)
		require.NoError(t, err)
	})

	t.Run("mount and unmount", func(t *testing.T) {
		mountID := registry.NewID("test", "static1")

		// Create a temporary directory with files
		tempDir, cleanup := createServerTempDir(t, map[string]string{
			"index.html": "<html><body>Hello World</body></html>",
			"style.css":  "body { color: red; }",
		})
		defer cleanup()

		// Mount handler
		err := server.Mount(mountID, "/static", http.FileServer(http.Dir(tempDir)))
		require.NoError(t, err)

		// Verify the mount path is stored
		assert.Equal(t, "/static", server.mountPaths[mountID])
		assert.NotNil(t, server.mountHandlers[mountID])

		// Re-mounting the same entry at the same path is an update, not a conflict.
		err = server.Mount(mountID, "/static", http.FileServer(http.Dir(tempDir)))
		require.NoError(t, err)

		// Moving the same entry to a new path releases the old mount.
		err = server.Mount(mountID, "/assets", http.FileServer(http.Dir(tempDir)))
		require.NoError(t, err)
		assert.Equal(t, "/assets", server.mountPaths[mountID])

		// Different entries still cannot claim the same path.
		otherID := registry.NewID("test", "static2")
		err = server.Mount(otherID, "/assets", http.FileServer(http.Dir(tempDir)))
		require.Error(t, err)
		assert.Equal(t, "/assets", server.mountPaths[mountID])

		// Failed move to an occupied path rolls back the original entry mount.
		err = server.Mount(otherID, "/occupied", http.FileServer(http.Dir(tempDir)))
		require.NoError(t, err)
		err = server.Mount(mountID, "/occupied", http.FileServer(http.Dir(tempDir)))
		require.Error(t, err)
		assert.Equal(t, "/assets", server.mountPaths[mountID])
		assert.Equal(t, "/occupied", server.mountPaths[otherID])

		// Now unmount
		err = server.Remove(mountID)
		require.NoError(t, err)
		err = server.Remove(otherID)
		require.NoError(t, err)

		// Verify the mapping is removed
		_, exists := server.mountPaths[mountID]
		assert.False(t, exists)
		_, exists = server.mountHandlers[mountID]
		assert.False(t, exists)

		// Try unmounting non-existent handler
		err = server.Remove(mountID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("rebuild", func(t *testing.T) {
		// Add a router and endpoint before rebuild
		routerID := registry.NewID("test", "router3")
		routerCfg := &config.RouterConfig{
			Prefix: "/api/v3",
		}

		err := server.UpsertRouter(routerID, routerCfg)
		require.NoError(t, err)

		endpointID := registry.NewID("test", "endpoint3")

		err = server.UpsertEndpoint(routerID, endpointID, "/test", "GET", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		require.NoError(t, err)

		// Rebuild the router
		err = server.Rebuild(context.Background())
		require.NoError(t, err)

		// Cleanup
		err = server.RemoveEndpoint(routerID, endpointID)
		require.NoError(t, err)
		err = server.DeleteRouter(routerID)
		require.NoError(t, err)
	})
}

func TestServerService_StartStop(t *testing.T) {
	port, err := findFreePort()
	require.NoError(t, err)

	cfg := &config.ServerConfig{
		Addr: fmt.Sprintf("127.0.0.1:%d", port), // Use local address, not just port, for reliability
		Timeouts: config.TimeoutConfig{
			ReadTimeout:  2 * time.Second,
			WriteTimeout: 2 * time.Second,
			IdleTimeout:  5 * time.Second,
		},
	}

	id := registry.NewID("test", "server1")
	middleware := NewMiddlewareRegistry(zap.NewNop())
	server, err := NewServerService(id, cfg, middleware)
	require.NoError(t, err)

	// Add router and endpoint
	routerID := registry.NewID("test", "router4")
	routerCfg := &config.RouterConfig{
		Prefix: "/api",
	}

	err = server.UpsertRouter(routerID, routerCfg)
	require.NoError(t, err)

	endpointID := registry.NewID("test", "endpoint4")

	err = server.UpsertEndpoint(routerID, endpointID, "/test", "GET", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("test"))
	}))
	require.NoError(t, err)

	err = server.Rebuild(context.Background())
	require.NoError(t, err)

	// Serve the server
	ctx, cancel := context.WithTimeout(contextapi.NewRootContext(), 10*time.Second)
	defer cancel()

	// Use a done channel to synchronize server start
	done := make(chan struct{})
	var statusCh <-chan any
	var startErr error

	go func() {
		statusCh, startErr = server.Start(ctx)
		close(done)
	}()

	// Wait for server to start with timeout
	select {
	case <-done:
		// Continue with tests
	case <-time.After(5 * time.Second):
		t.Fatal("Server start timed out")
	}

	require.NoError(t, startErr)
	require.NotNil(t, statusCh)

	// Test endpoint
	client := &http.Client{Timeout: 2 * time.Second}

	// Retry a few times in case the server is still initializing
	var resp *http.Response
	var lastErr error

	for i := 0; i < 3; i++ {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, fmt.Sprintf("http://%s/api/test", cfg.Addr), nil)
		resp, lastErr = client.Do(req)
		if lastErr == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	require.NoError(t, lastErr, "Failed to connect to server after retries")

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "test", string(body))
	assert.NoError(t, resp.Body.Close())

	// Stop the server
	err = server.Stop(ctx)
	require.NoError(t, err)

	// Verify server is stopped
	stopReq, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, fmt.Sprintf("http://%s/api/test", cfg.Addr), nil)
	//nolint:bodyclose // server is expected to be stopped
	_, err = http.DefaultClient.Do(stopReq)
	assert.Error(t, err)

	// Cleanup
	err = server.RemoveEndpoint(routerID, endpointID)
	require.NoError(t, err)
	err = server.DeleteRouter(routerID)
	require.NoError(t, err)
}

func TestServerService_Middleware(t *testing.T) {
	port, err := findFreePort()
	require.NoError(t, err)

	cfg := &config.ServerConfig{
		Addr: fmt.Sprintf("127.0.0.1:%d", port),
	}

	id := registry.NewID("test", "server1")

	// Create middleware factory for the test
	middlewareFactory := NewMiddlewareRegistry(zap.NewNop())
	_ = middlewareFactory.Register("request_id", func(_ map[string]string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Pass through any existing request ID
				reqID := r.Header.Get("X-Request-Id")
				if reqID == "" {
					reqID = "generated-id"
				}
				// Set it in the request
				r.Header.Set("X-Request-Id", reqID)
				next.ServeHTTP(w, r)
			})
		}
	})

	_ = middlewareFactory.Register("real_ip", func(_ map[string]string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Simple pass-through middleware for testing
				next.ServeHTTP(w, r)
			})
		}
	})

	server, err := NewServerService(id, cfg, middlewareFactory)
	require.NoError(t, err)

	// Add router with middleware
	routerID := registry.NewID("test", "router5")
	routerCfg := &config.RouterConfig{
		Prefix:     "/api",
		Middleware: []string{"request_id", "real_ip"},
		Options:    map[string]string{},
	}

	err = server.UpsertRouter(routerID, routerCfg)
	require.NoError(t, err)

	// Add test endpoint that checks request ID middleware
	endpointID := registry.NewID("test", "endpoint5")

	err = server.UpsertEndpoint(routerID, endpointID, "/test", "GET", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if request ID was set by middleware
		reqID := r.Header.Get("X-Request-Id")
		// Important: Copy the header from request to response
		w.Header().Set("X-Got-Request-Id", reqID)
		w.WriteHeader(http.StatusOK)
	}))
	require.NoError(t, err)

	err = server.Rebuild(context.Background())
	require.NoError(t, err)

	// Serve the server
	ctx, cancel := context.WithTimeout(contextapi.NewRootContext(), 10*time.Second)
	defer cancel()

	// Use a done channel for synchronization
	done := make(chan struct{})
	var statusCh <-chan any
	var startErr error

	go func() {
		statusCh, startErr = server.Start(ctx)
		close(done)
	}()

	// Wait for server with timeout
	select {
	case <-done:
		// Continue with test
	case <-time.After(5 * time.Second):
		t.Fatal("Server start timed out")
	}

	require.NoError(t, startErr)
	require.NotNil(t, statusCh)

	// Test endpoint with retries
	client := &http.Client{Timeout: 2 * time.Second}

	var resp *http.Response
	var lastErr error

	// Set a custom request ID in the client request to ensure middleware processes it
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("http://%s/api/test", cfg.Addr), nil)
	require.NoError(t, err)
	req.Header.Set("X-Request-Id", "test-request-id")

	for i := 0; i < 3; i++ {
		resp, lastErr = client.Do(req)
		if lastErr == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	require.NoError(t, lastErr, "Failed to connect to server after retries")

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Check if request ID header was set
	reqID := resp.Header.Get("X-Got-Request-Id")
	assert.Equal(t, "test-request-id", reqID, "Request ID middleware should pass through the ID")
	assert.NoError(t, resp.Body.Close())

	// Stop the server
	err = server.Stop(ctx)
	require.NoError(t, err)

	// Cleanup
	err = server.RemoveEndpoint(routerID, endpointID)
	require.NoError(t, err)
	err = server.DeleteRouter(routerID)
	require.NoError(t, err)
}

func TestServerService_RebuildPreservesFrameContext(t *testing.T) {
	port, err := findFreePort()
	require.NoError(t, err)

	cfg := &config.ServerConfig{
		Addr: fmt.Sprintf("127.0.0.1:%d", port),
	}

	middleware := NewMiddlewareRegistry(zap.NewNop())
	frameKey := &contextapi.Key{Name: "test.frame"}
	require.NoError(t, middleware.Register("frame_check", func(_ map[string]string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fc := contextapi.FrameFromContext(r.Context())
				if fc == nil {
					w.Header().Set("X-Frame", "missing")
				} else if err := fc.Set(frameKey, "ok"); err != nil {
					w.Header().Set("X-Frame", "sealed")
				} else {
					w.Header().Set("X-Frame", "ok")
				}
				next.ServeHTTP(w, r)
			})
		}
	}))

	server, err := NewServerService(registry.NewID("test", "server_frame"), cfg, middleware)
	require.NoError(t, err)

	routerID := registry.NewID("test", "router_frame")
	routerCfg := &config.RouterConfig{
		Prefix:     "/api",
		Middleware: []string{"frame_check"},
		Options:    map[string]string{},
	}
	require.NoError(t, server.UpsertRouter(routerID, routerCfg))

	endpointID := registry.NewID("test", "endpoint_frame")
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	require.NoError(t, server.UpsertEndpoint(routerID, endpointID, "/hello", "GET", handler))
	require.NoError(t, server.Rebuild(context.Background()))

	ctx, cancel := context.WithTimeout(contextapi.NewRootContext(), 10*time.Second)
	defer cancel()

	done := make(chan struct{})
	var statusCh <-chan any
	var startErr error
	go func() {
		statusCh, startErr = server.Start(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Server start timed out")
	}

	require.NoError(t, startErr)
	require.NotNil(t, statusCh)

	client := &http.Client{Timeout: 2 * time.Second}
	doRequest := func(path string) *http.Response {
		var resp *http.Response
		var lastErr error
		for i := 0; i < 3; i++ {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, fmt.Sprintf("http://%s%s", cfg.Addr, path), nil)
			if err != nil {
				lastErr = err
				time.Sleep(200 * time.Millisecond)
				continue
			}
			resp, lastErr = client.Do(req)
			if lastErr == nil {
				return resp
			}
			time.Sleep(200 * time.Millisecond)
		}
		require.NoError(t, lastErr)
		return resp
	}

	resp := doRequest("/api/hello")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "ok", resp.Header.Get("X-Frame"))
	require.NoError(t, resp.Body.Close())

	require.NoError(t, server.UpsertEndpoint(routerID, endpointID, "/hello2", "GET", handler))
	require.NoError(t, server.Rebuild(context.Background()))

	resp = doRequest("/api/hello2")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "ok", resp.Header.Get("X-Frame"))
	require.NoError(t, resp.Body.Close())

	require.NoError(t, server.Stop(ctx))
}

func TestEnsureRunning(t *testing.T) {
	// Create a server with a test port
	port, err := findFreePort()
	require.NoError(t, err)

	cfg := &config.ServerConfig{
		Addr: fmt.Sprintf("127.0.0.1:%d", port),
	}

	id := registry.NewID("test", "server1")
	middleware := NewMiddlewareRegistry(zap.NewNop())
	server, err := NewServerService(id, cfg, middleware)
	require.NoError(t, err)

	// Serve a separate HTTP server on that port to simulate our server already running
	httpServer := &http.Server{
		Addr: cfg.Addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		ReadHeaderTimeout: time.Second,
	}

	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- httpServer.ListenAndServe()
	}()

	// Give the server a moment to start
	time.Sleep(100 * time.Millisecond)

	// Clearnet probe mirrors what buildListener produces for non-overlay services.
	probe := func(ctx context.Context, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", addr)
	}

	// The ensureRunning check should pass because something is listening on the port
	ctx, cancel := context.WithTimeout(contextapi.NewRootContext(), 2*time.Second)
	defer cancel()

	err = server.ensureRunning(ctx, probe)
	assert.NoError(t, err)

	// Now stop the server and the check should fail
	err = httpServer.Close()
	require.NoError(t, err)

	// Wait for server to actually close
	select {
	case <-serverErrCh:
		// Server closed
	case <-time.After(time.Second):
		t.Fatal("HTTP server didn't close")
	}

	ctx2, cancel2 := context.WithTimeout(contextapi.NewRootContext(), 500*time.Millisecond) // Short timeout
	defer cancel2()

	err = server.ensureRunning(ctx2, probe)
	assert.Error(t, err)
}

func TestContextListener(t *testing.T) {
	port, err := findFreePort()
	require.NoError(t, err)

	cfg := &config.ServerConfig{
		Addr: fmt.Sprintf("127.0.0.1:%d", port),
	}

	id := registry.NewID("test", "server1")
	middleware := NewMiddlewareRegistry(zap.NewNop())
	server, err := NewServerService(id, cfg, middleware)
	require.NoError(t, err)

	// Add a test endpoint that verifies the listener context is set
	routerID := registry.NewID("test", "router6")
	routerCfg := &config.RouterConfig{
		Prefix: "/api",
	}

	err = server.UpsertRouter(routerID, routerCfg)
	require.NoError(t, err)

	endpointID := registry.NewID("test", "endpoint6")

	err = server.UpsertEndpoint(routerID, endpointID, "/test", "GET", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// ContextListener is no longer set - HTTP metadata now in FrameContext
		// Just return success
		w.WriteHeader(http.StatusOK)
	}))
	require.NoError(t, err)

	err = server.Rebuild(context.Background())
	require.NoError(t, err)

	// Serve the server
	ctx, cancel := context.WithTimeout(contextapi.NewRootContext(), 5*time.Second)
	defer cancel()

	// Use a done channel for synchronization
	done := make(chan struct{})
	var statusCh <-chan any
	var startErr error

	go func() {
		statusCh, startErr = server.Start(ctx)
		close(done)
	}()

	// Wait for server with timeout
	select {
	case <-done:
		// Continue with test
	case <-time.After(5 * time.Second):
		t.Fatal("Server start timed out")
	}

	require.NoError(t, startErr)
	require.NotNil(t, statusCh)

	// Test the endpoint
	client := &http.Client{Timeout: 2 * time.Second}

	var resp *http.Response
	var lastErr error

	for i := 0; i < 3; i++ {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, fmt.Sprintf("http://%s/api/test", cfg.Addr), nil)
		resp, lastErr = client.Do(req)
		if lastErr == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	require.NoError(t, lastErr)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NoError(t, resp.Body.Close())

	// Stop the server
	err = server.Stop(ctx)
	require.NoError(t, err)

	// Cleanup
	err = server.RemoveEndpoint(routerID, endpointID)
	require.NoError(t, err)
	err = server.DeleteRouter(routerID)
	require.NoError(t, err)
}
