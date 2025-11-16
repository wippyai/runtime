package http

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	apiregistry "github.com/wippyai/runtime/api/registry"
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

func (t *SimpleTranscoder) Unmarshal(p payload.Payload, v interface{}) error {
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
	staticFactory, err := NewStaticFactory(fsRegistry)
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
	staticFactory, _ := NewStaticFactory(fsRegistry)

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
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "transcoder is required")

		_, err = NewManager(
			transcoder,
			nil, // Missing bus
			serverFactory,
			endpointFactory,
			staticFactory,
			logger,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "event bus is required")

		_, err = NewManager(
			transcoder,
			bus,
			nil, // Missing server factory
			endpointFactory,
			staticFactory,
			logger,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "server factory is required")

		_, err = NewManager(
			transcoder,
			bus,
			serverFactory,
			nil, // Missing endpoint factory
			staticFactory,
			logger,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "endpoint factory is required")

		_, err = NewManager(
			transcoder,
			bus,
			serverFactory,
			endpointFactory,
			nil, // Missing static factory
			logger,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "static factory is required")
	})
}

func TestManager_ServerOperations(t *testing.T) {
	manager, ctx := setupManager(t)

	// Create server config
	serverID := apiregistry.ID{NS: "test", Name: "server1"}
	serverCfg := &config.ServerConfig{
		Addr: ":0", // Dynamic port
		Meta: map[string]interface{}{},
	}

	// Add server
	entry := apiregistry.Entry{
		ID:   serverID,
		Kind: config.KindServer,
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
		Meta: map[string]interface{}{},
		Timeouts: config.TimeoutConfig{
			ReadTimeout: 30 * time.Second,
		},
	}

	updatedEntry := apiregistry.Entry{
		ID:   serverID,
		Kind: config.KindServer,
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
	staticFactory, err := NewStaticFactory(fsRegistry)
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
	serverID := apiregistry.ID{NS: "test", Name: testID + "_server"}
	serverCfg := &config.ServerConfig{
		Addr: ":0", // Dynamic port
		Meta: map[string]interface{}{},
	}

	serverEntry := apiregistry.Entry{
		ID:   serverID,
		Kind: config.KindServer,
		Data: payload.New(serverCfg),
	}

	err = manager.Add(ctx, serverEntry)
	require.NoError(t, err)

	// Add router with a unique Source
	routerID := apiregistry.ID{NS: "test", Name: testID + "_router"}
	routerCfg := &config.RouterConfig{
		Prefix: "/api",
		Meta: map[string]interface{}{
			config.ServerID: serverID.String(),
		},
	}

	routerEntry := apiregistry.Entry{
		ID:   routerID,
		Kind: config.KindRouter,
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
		Meta: map[string]interface{}{
			config.ServerID: serverID.String(),
		},
	}

	updatedRouterEntry := apiregistry.Entry{
		ID:   routerID,
		Kind: config.KindRouter,
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
	serverID := apiregistry.ID{NS: "test", Name: "server1"}
	serverCfg := &config.ServerConfig{
		Addr: ":0", // Dynamic port
		Meta: map[string]interface{}{},
	}

	serverEntry := apiregistry.Entry{
		ID:   serverID,
		Kind: config.KindServer,
		Data: payload.New(serverCfg),
	}

	err := manager.Add(ctx, serverEntry)
	require.NoError(t, err)

	// Add a router
	routerID := apiregistry.ID{NS: "test", Name: "router1"}
	routerCfg := &config.RouterConfig{
		Prefix: "/api",
		Meta: map[string]interface{}{
			config.ServerID: serverID.String(),
		},
	}

	routerEntry := apiregistry.Entry{
		ID:   routerID,
		Kind: config.KindRouter,
		Data: payload.New(routerCfg),
	}

	err = manager.Add(ctx, routerEntry)
	require.NoError(t, err)

	// Add an endpoint
	endpointID := apiregistry.ID{NS: "test", Name: "endpoint1"}
	endpointCfg := &config.EndpointConfig{
		Path:   "/test",
		Method: "GET",
		Func:   apiregistry.ID{NS: "test", Name: "func1"},
		Meta: map[string]interface{}{
			config.RouterID: routerID.String(),
		},
	}

	endpointEntry := apiregistry.Entry{
		ID:   endpointID,
		Kind: config.KindEndpoint,
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
	serverID := apiregistry.ID{NS: "test", Name: "server1"}
	serverCfg := &config.ServerConfig{
		Addr: ":0", // Dynamic port
		Meta: map[string]interface{}{},
	}

	serverEntry := apiregistry.Entry{
		ID:   serverID,
		Kind: config.KindServer,
		Data: payload.New(serverCfg),
	}

	err := manager.Add(ctx, serverEntry)
	require.NoError(t, err)

	// Add a static handler
	staticID := apiregistry.ID{NS: "test", Name: "static1"}
	staticCfg := &config.StaticConfig{
		Path: "/static",
		FS:   apiregistry.ID{NS: "test", Name: "files"},
		Meta: map[string]interface{}{
			config.ServerID: serverID.String(),
		},
	}

	staticEntry := apiregistry.Entry{
		ID:   staticID,
		Kind: config.KindStatic,
		Data: payload.New(staticCfg),
	}

	err = manager.Add(ctx, staticEntry)
	require.NoError(t, err)

	// Verify pending server rebuild
	assert.True(t, manager.pending[serverID])

	// Delete static handler
	err = manager.Delete(ctx, staticEntry)
	require.NoError(t, err)
}

func TestManager_TransactionOperations(t *testing.T) {
	manager, ctx := setupManager(t)

	// Add a server and mark it pending
	serverID := apiregistry.ID{NS: "test", Name: "server1"}
	serverCfg := &config.ServerConfig{
		Addr: ":0", // Dynamic port
		Meta: map[string]interface{}{},
	}

	serverEntry := apiregistry.Entry{
		ID:   serverID,
		Kind: config.KindServer,
		Data: payload.New(serverCfg),
	}

	err := manager.Add(ctx, serverEntry)
	require.NoError(t, err)

	// Add a router to create a pending rebuild
	routerID := apiregistry.ID{NS: "test", Name: "router1"}
	routerCfg := &config.RouterConfig{
		Prefix: "/api",
		Meta: map[string]interface{}{
			config.ServerID: serverID.String(),
		},
	}

	routerEntry := apiregistry.Entry{
		ID:   routerID,
		Kind: config.KindRouter,
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

func TestManager_UnsupportedKinds(t *testing.T) {
	manager, ctx := setupManager(t)

	unsupportedEntry := apiregistry.Entry{
		ID:   apiregistry.ID{NS: "test", Name: "unsupported"},
		Kind: "unsupported.kind",
		Data: payload.New("test"),
	}

	// Test Add with unsupported kind
	err := manager.Add(ctx, unsupportedEntry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported entry kind")

	// Test Update with unsupported kind
	err = manager.Update(ctx, unsupportedEntry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported entry kind")

	// Test Delete with unsupported kind
	err = manager.Delete(ctx, unsupportedEntry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported entry kind")
}

func TestManager_ErrorHandling(t *testing.T) {
	manager, ctx := setupManager(t)

	// Test server not found for router
	routerID := apiregistry.ID{NS: "test", Name: "router1"}
	routerCfg := &config.RouterConfig{
		Prefix: "/api",
		Meta: map[string]interface{}{
			config.ServerID: "test:nonexistent",
		},
	}

	routerEntry := apiregistry.Entry{
		ID:   routerID,
		Kind: config.KindRouter,
		Data: payload.New(routerCfg),
	}

	err := manager.Add(ctx, routerEntry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "server test:nonexistent not found")

	// Test router not found for endpoint
	endpointID := apiregistry.ID{NS: "test", Name: "endpoint1"}
	endpointCfg := &config.EndpointConfig{
		Path:   "/test",
		Method: "GET",
		Func:   apiregistry.ID{NS: "test", Name: "func1"},
		Meta: map[string]interface{}{
			config.RouterID: "test:nonexistent",
		},
	}

	endpointEntry := apiregistry.Entry{
		ID:   endpointID,
		Kind: config.KindEndpoint,
		Data: payload.New(endpointCfg),
	}

	err = manager.Add(ctx, endpointEntry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "router test:nonexistent not found")
}
