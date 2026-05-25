// SPDX-License-Identifier: MPL-2.0

package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	apiregistry "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	config "github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// SimpleTranscoder for payload transcoding during tests
type SimpleTranscoder struct{}

func NewSimpleTranscoder() *SimpleTranscoder {
	return &SimpleTranscoder{}
}

func (t *SimpleTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

func (t *SimpleTranscoder) Unmarshal(p payload.Payload, v any) error {
	// Type switches based on expected output type and source payload
	switch dest := v.(type) {
	case *config.ServerConfig:
		if src, ok := p.Data().(*config.ServerConfig); ok {
			*dest = *src
			return nil
		}
	case *config.RouterConfig:
		if src, ok := p.Data().(*config.RouterConfig); ok {
			*dest = *src
			return nil
		}
	case *config.EndpointConfig:
		if src, ok := p.Data().(*config.EndpointConfig); ok {
			*dest = *src
			return nil
		}
	case *config.StaticConfig:
		if src, ok := p.Data().(*config.StaticConfig); ok {
			*dest = *src
			return nil
		}
	}

	return nil // Let's assume success for simplicity
}

type failingServer struct {
	rebuildErr error
}

func (s *failingServer) Start(_ context.Context) (<-chan any, error) {
	ch := make(chan any)
	close(ch)
	return ch, nil
}

func (s *failingServer) Stop(_ context.Context) error {
	return nil
}

func (s *failingServer) Send(_ *relay.Package) error {
	return nil
}

func (s *failingServer) UpdateConfig(_ *config.ServerConfig) error {
	return nil
}

func (s *failingServer) UpsertRouter(_ apiregistry.ID, _ *config.RouterConfig) error {
	return nil
}

func (s *failingServer) DeleteRouter(_ apiregistry.ID) error {
	return nil
}

func (s *failingServer) UpsertEndpoint(_ apiregistry.ID, _ apiregistry.ID, _ string, _ string, _ http.Handler) error {
	return nil
}

func (s *failingServer) RemoveEndpoint(_ apiregistry.ID, _ apiregistry.ID) error {
	return nil
}

func (s *failingServer) Mount(_ apiregistry.ID, _ string, _ http.Handler) error {
	return nil
}

func (s *failingServer) Remove(_ apiregistry.ID) error {
	return nil
}

func (s *failingServer) Rebuild(_ context.Context) error {
	return s.rebuildErr
}

// createManagerTempDir for filesystem testing
func createManagerTempDir(t *testing.T, files map[string]string) (string, func()) {
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

func setupManager(t *testing.T) (*Manager, context.Context) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	transcoder := NewSimpleTranscoder()

	// Create function registry
	functionRegistry := NewSimpleFunctionRegistry()

	// Create FS registry
	fsRegistry := NewSimpleFSRegistry()

	// Create middleware factory
	middlewareFactory := NewMiddlewareRegistry(zap.NewNop())

	// Create temporary files directory
	tempDir, cleanup := createManagerTempDir(t, map[string]string{
		"index.html": "<html><body>Hello World</body></html>",
		"style.css":  "body { color: red; }",
	})
	t.Cleanup(cleanup)

	// Use our mockFS implementation from factory_test.go
	mockFS := NewMockFS(tempDir)
	fsRegistry.Register("test:files", mockFS)

	// Create factories
	serverFactory := NewServerFactory(middlewareFactory)
	endpointFactory, err := NewEndpointFactory(functionRegistry)
	require.NoError(t, err)
	staticFactory, err := NewStaticFactory(fsRegistry, middlewareFactory)
	require.NoError(t, err)

	// Create manager
	manager, err := NewManager(
		transcoder,
		bus,
		serverFactory,
		endpointFactory,
		staticFactory,
		logger,
	)
	require.NoError(t, err)

	ctx := ctxapi.NewRootContext()

	return manager, ctx
}

func TestManager_NewManager(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	bus := eventbus.NewBus()
	transcoder := NewSimpleTranscoder()
	functionRegistry := NewSimpleFunctionRegistry()
	fsRegistry := NewSimpleFSRegistry()
	middlewareFactory := NewMiddlewareRegistry(zap.NewNop())

	serverFactory := NewServerFactory(middlewareFactory)
	endpointFactory, _ := NewEndpointFactory(functionRegistry)
	staticFactory, _ := NewStaticFactory(fsRegistry, middlewareFactory)

	t.Run("valid creation", func(t *testing.T) {
		manager, err := NewManager(
			transcoder,
			bus,
			serverFactory,
			endpointFactory,
			staticFactory,
			logger,
		)
		assert.NoError(t, err)
		assert.NotNil(t, manager)
	})

	t.Run("missing dependencies", func(t *testing.T) {
		_, err := NewManager(
			nil, // Missing transcoder
			bus,
			serverFactory,
			endpointFactory,
			staticFactory,
			logger,
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "transcoder is required")

		_, err = NewManager(
			transcoder,
			nil, // Missing bus
			serverFactory,
			endpointFactory,
			staticFactory,
			logger,
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "event bus is required")

		_, err = NewManager(
			transcoder,
			bus,
			nil, // Missing server factory
			endpointFactory,
			staticFactory,
			logger,
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "server factory is required")

		_, err = NewManager(
			transcoder,
			bus,
			serverFactory,
			nil, // Missing endpoint factory
			staticFactory,
			logger,
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "endpoint factory is required")

		_, err = NewManager(
			transcoder,
			bus,
			serverFactory,
			endpointFactory,
			nil, // Missing static factory
			logger,
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "static factory is required")
	})
}

func TestManager_ServerOperations(t *testing.T) {
	manager, ctx := setupManager(t)

	// Create server config
	serverID := apiregistry.NewID("test", "server1")
	serverCfg := &config.ServerConfig{
		Addr: ":0", // Dynamic port
		Meta: map[string]any{},
	}

	// Add server
	entry := apiregistry.Entry{
		ID:   serverID,
		Kind: config.Server,
		Data: payload.New(serverCfg),
	}

	err := manager.Add(ctx, entry)
	require.NoError(t, err)

	// Verify server was added
	assert.Len(t, manager.servers, 1)
	assert.Contains(t, manager.servers, serverID)

	// Update server
	updatedCfg := &config.ServerConfig{
		Addr: ":0", // Dynamic port
		Meta: map[string]any{},
		Timeouts: config.TimeoutConfig{
			ReadTimeout: 30 * time.Second,
		},
	}

	updatedEntry := apiregistry.Entry{
		ID:   serverID,
		Kind: config.Server,
		Data: payload.New(updatedCfg),
	}

	err = manager.Update(ctx, updatedEntry)
	require.NoError(t, err)

	// Delete server
	err = manager.Delete(ctx, entry)
	require.NoError(t, err)

	// Verify server was removed
	assert.Empty(t, manager.servers)
}

func TestManager_RouterOperations(t *testing.T) {
	// Use a completely unique naming approach to avoid conflicts
	testID := fmt.Sprintf("test_router_%d", time.Now().UnixNano())

	// Create a fresh manager for this test
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	transcoder := NewSimpleTranscoder()

	functionRegistry := NewSimpleFunctionRegistry()
	fsRegistry := NewSimpleFSRegistry()
	middlewareFactory := NewMiddlewareRegistry(zap.NewNop())

	tempDir, cleanup := createManagerTempDir(t, map[string]string{
		"index.html": "<html><body>Test</body></html>",
	})
	t.Cleanup(cleanup)

	mockFS := NewMockFS(tempDir)
	fsRegistry.Register("test:files", mockFS)

	serverFactory := NewServerFactory(middlewareFactory)
	endpointFactory, err := NewEndpointFactory(functionRegistry)
	require.NoError(t, err)
	staticFactory, err := NewStaticFactory(fsRegistry, middlewareFactory)
	require.NoError(t, err)

	manager, err := NewManager(
		transcoder,
		bus,
		serverFactory,
		endpointFactory,
		staticFactory,
		logger,
	)
	require.NoError(t, err)

	ctx := ctxapi.NewRootContext()

	// First add a server with a unique Source
	serverID := apiregistry.NewID("test", testID+"_server")
	serverCfg := &config.ServerConfig{
		Addr: ":0", // Dynamic port
		Meta: map[string]any{},
	}

	serverEntry := apiregistry.Entry{
		ID:   serverID,
		Kind: config.Server,
		Data: payload.New(serverCfg),
	}

	err = manager.Add(ctx, serverEntry)
	require.NoError(t, err)

	// Add router with a unique Source
	routerID := apiregistry.NewID("test", testID+"_router")
	routerCfg := &config.RouterConfig{
		Prefix: "/api",
		Meta: map[string]any{
			config.ServerID: serverID.String(),
		},
	}

	routerEntry := apiregistry.Entry{
		ID:   routerID,
		Kind: config.Router,
		Data: payload.New(routerCfg),
	}

	err = manager.Add(ctx, routerEntry)
	require.NoError(t, err)

	// Verify router was mapped to server
	assert.Contains(t, manager.routerServers, routerID)
	assert.Equal(t, serverID, manager.routerServers[routerID])

	// Update router
	updatedRouterCfg := &config.RouterConfig{
		Prefix: "/api/v2",
		Meta: map[string]any{
			config.ServerID: serverID.String(),
		},
	}

	updatedRouterEntry := apiregistry.Entry{
		ID:   routerID,
		Kind: config.Router,
		Data: payload.New(updatedRouterCfg),
	}

	err = manager.Update(ctx, updatedRouterEntry)
	require.NoError(t, err)

	// Delete router
	err = manager.Delete(ctx, routerEntry)
	require.NoError(t, err)

	// Verify router mapping was removed
	assert.NotContains(t, manager.routerServers, routerID)

	// Cleanup server
	err = manager.Delete(ctx, serverEntry)
	require.NoError(t, err)
}

func TestManager_EndpointOperations(t *testing.T) {
	manager, ctx := setupManager(t)

	// Add a server
	serverID := apiregistry.NewID("test", "server1")
	serverCfg := &config.ServerConfig{
		Addr: ":0", // Dynamic port
		Meta: map[string]any{},
	}

	serverEntry := apiregistry.Entry{
		ID:   serverID,
		Kind: config.Server,
		Data: payload.New(serverCfg),
	}

	err := manager.Add(ctx, serverEntry)
	require.NoError(t, err)

	// Add a router
	routerID := apiregistry.NewID("test", "router1")
	routerCfg := &config.RouterConfig{
		Prefix: "/api",
		Meta: map[string]any{
			config.ServerID: serverID.String(),
		},
	}

	routerEntry := apiregistry.Entry{
		ID:   routerID,
		Kind: config.Router,
		Data: payload.New(routerCfg),
	}

	err = manager.Add(ctx, routerEntry)
	require.NoError(t, err)

	// Add an endpoint
	endpointID := apiregistry.NewID("test", "endpoint1")
	endpointCfg := &config.EndpointConfig{
		Path:   "/test",
		Method: "GET",
		Func:   apiregistry.NewID("test", "func1"),
		Meta: map[string]any{
			config.RouterID: routerID.String(),
		},
	}

	endpointEntry := apiregistry.Entry{
		ID:   endpointID,
		Kind: config.Endpoint,
		Data: payload.New(endpointCfg),
	}

	err = manager.Add(ctx, endpointEntry)
	require.NoError(t, err)

	// Verify pending server rebuild
	assert.True(t, manager.pending[serverID])

	// Delete endpoint
	err = manager.Delete(ctx, endpointEntry)
	require.NoError(t, err)
}

func TestManager_StaticOperations(t *testing.T) {
	manager, ctx := setupManager(t)

	// Add a server
	serverID := apiregistry.NewID("test", "server1")
	serverCfg := &config.ServerConfig{
		Addr: ":0", // Dynamic port
		Meta: map[string]any{},
	}

	serverEntry := apiregistry.Entry{
		ID:   serverID,
		Kind: config.Server,
		Data: payload.New(serverCfg),
	}

	err := manager.Add(ctx, serverEntry)
	require.NoError(t, err)

	// Add a static handler
	staticID := apiregistry.NewID("test", "static1")
	staticCfg := &config.StaticConfig{
		Path: "/static",
		FS:   apiregistry.NewID("test", "files"),
		Meta: map[string]any{
			config.ServerID: serverID.String(),
		},
	}

	staticEntry := apiregistry.Entry{
		ID:   staticID,
		Kind: config.Static,
		Data: payload.New(staticCfg),
	}

	err = manager.Add(ctx, staticEntry)
	require.NoError(t, err)

	// Verify pending server rebuild
	assert.True(t, manager.pending[serverID])

	// Updating the same static entry must replace the entry-owned mount in place.
	err = manager.Update(ctx, staticEntry)
	require.NoError(t, err)

	// Updating the same static entry can move the mount path.
	movedStaticCfg := *staticCfg
	movedStaticCfg.Path = "/assets"
	staticEntry.Data = payload.New(&movedStaticCfg)
	err = manager.Update(ctx, staticEntry)
	require.NoError(t, err)

	server := manager.servers[serverID].(*ServerService)
	assert.Equal(t, "/assets", server.mountPaths[staticID])

	// A different static entry cannot claim the same mount path.
	otherStaticEntry := apiregistry.Entry{
		ID:   apiregistry.NewID("test", "static2"),
		Kind: config.Static,
		Data: payload.New(&movedStaticCfg),
	}
	err = manager.Add(ctx, otherStaticEntry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mount path already exists")

	// Failed path move leaves the previous static mount intact.
	otherStaticCfg := *staticCfg
	otherStaticCfg.Path = "/occupied"
	otherStaticEntry.Data = payload.New(&otherStaticCfg)
	err = manager.Add(ctx, otherStaticEntry)
	require.NoError(t, err)
	conflictingStaticCfg := movedStaticCfg
	conflictingStaticCfg.Path = "/occupied"
	staticEntry.Data = payload.New(&conflictingStaticCfg)
	err = manager.Update(ctx, staticEntry)
	require.Error(t, err)
	assert.Equal(t, "/assets", server.mountPaths[staticID])
	assert.Equal(t, "/occupied", server.mountPaths[otherStaticEntry.ID])

	// Delete static handler
	err = manager.Delete(ctx, staticEntry)
	require.NoError(t, err)
	err = manager.Delete(ctx, otherStaticEntry)
	require.NoError(t, err)
}

func TestManager_TransactionOperations(t *testing.T) {
	manager, ctx := setupManager(t)

	// Add a server and mark it pending
	serverID := apiregistry.NewID("test", "server1")
	serverCfg := &config.ServerConfig{
		Addr: ":0", // Dynamic port
		Meta: map[string]any{},
	}

	serverEntry := apiregistry.Entry{
		ID:   serverID,
		Kind: config.Server,
		Data: payload.New(serverCfg),
	}

	err := manager.Add(ctx, serverEntry)
	require.NoError(t, err)

	// Add a router to create a pending rebuild
	routerID := apiregistry.NewID("test", "router1")
	routerCfg := &config.RouterConfig{
		Prefix: "/api",
		Meta: map[string]any{
			config.ServerID: serverID.String(),
		},
	}

	routerEntry := apiregistry.Entry{
		ID:   routerID,
		Kind: config.Router,
		Data: payload.New(routerCfg),
	}

	err = manager.Add(ctx, routerEntry)
	require.NoError(t, err)

	// Verify pending state
	assert.True(t, manager.pending[serverID])

	// Test Begin - should clear pending
	manager.Begin(ctx)
	assert.Empty(t, manager.pending)

	// Add a pending change
	manager.pending[serverID] = true

	// Test Discard - should clear pending without rebuild
	manager.Discard(ctx)
	assert.Empty(t, manager.pending)

	// Add a pending change
	manager.pending[serverID] = true

	// Test Commit - should trigger rebuild and clear pending
	manager.Commit(ctx)
	assert.Empty(t, manager.pending)
}

func TestManager_CommitKeepsPendingOnRebuildError(t *testing.T) {
	manager, ctx := setupManager(t)

	serverID := apiregistry.NewID("test", "server-rebuild-fail")
	manager.servers[serverID] = &failingServer{rebuildErr: errors.New("rebuild failed")}
	manager.pending[serverID] = true

	manager.Commit(ctx)

	if _, stillPending := manager.pending[serverID]; !stillPending {
		t.Fatal("expected pending server to remain after rebuild error")
	}
}

func TestManager_UnsupportedKinds(t *testing.T) {
	manager, ctx := setupManager(t)

	unsupportedEntry := apiregistry.Entry{
		ID:   apiregistry.NewID("test", "unsupported"),
		Kind: "unsupported.kind",
		Data: payload.New("test"),
	}

	// Test Add with unsupported kind
	err := manager.Add(ctx, unsupportedEntry)
	require.Error(t, err)
	var apiErr apierror.Error
	ok := errors.As(err, &apiErr)
	require.True(t, ok)
	assert.Contains(t, apiErr.Error(), "unsupported entry kind")
	assert.Equal(t, "unsupported.kind", apiErr.Details().GetString("kind", ""))

	// Test Update with unsupported kind
	err = manager.Update(ctx, unsupportedEntry)
	require.Error(t, err)
	ok = errors.As(err, &apiErr)
	require.True(t, ok)
	assert.Contains(t, apiErr.Error(), "unsupported entry kind")
	assert.Equal(t, "unsupported.kind", apiErr.Details().GetString("kind", ""))

	// Test Delete with unsupported kind
	err = manager.Delete(ctx, unsupportedEntry)
	require.Error(t, err)
	ok = errors.As(err, &apiErr)
	require.True(t, ok)
	assert.Contains(t, apiErr.Error(), "unsupported entry kind")
	assert.Equal(t, "unsupported.kind", apiErr.Details().GetString("kind", ""))
}

func TestManager_ErrorHandling(t *testing.T) {
	manager, ctx := setupManager(t)

	// Test server not found for router
	routerID := apiregistry.NewID("test", "router1")
	routerCfg := &config.RouterConfig{
		Prefix: "/api",
		Meta: map[string]any{
			config.ServerID: "test:nonexistent",
		},
	}

	routerEntry := apiregistry.Entry{
		ID:   routerID,
		Kind: config.Router,
		Data: payload.New(routerCfg),
	}

	err := manager.Add(ctx, routerEntry)
	require.Error(t, err)
	var apiErr apierror.Error
	ok := errors.As(err, &apiErr)
	require.True(t, ok)
	assert.Contains(t, apiErr.Error(), "server not found")
	assert.Equal(t, "test:nonexistent", apiErr.Details().GetString("id", ""))

	// Test router not found for endpoint
	endpointID := apiregistry.NewID("test", "endpoint1")
	endpointCfg := &config.EndpointConfig{
		Path:   "/test",
		Method: "GET",
		Func:   apiregistry.NewID("test", "func1"),
		Meta: map[string]any{
			config.RouterID: "test:nonexistent",
		},
	}

	endpointEntry := apiregistry.Entry{
		ID:   endpointID,
		Kind: config.Endpoint,
		Data: payload.New(endpointCfg),
	}

	err = manager.Add(ctx, endpointEntry)
	require.Error(t, err)
	ok = errors.As(err, &apiErr)
	require.True(t, ok)
	assert.Contains(t, apiErr.Error(), "router not found")
	assert.Equal(t, "test:nonexistent", apiErr.Details().GetString("router_id", ""))
}
