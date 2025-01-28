package http

import (
	"context"
	httpbase "net/http"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/service/http"

	"github.com/ponyruntime/pony/pkg/eventbus"
	transcoder "github.com/ponyruntime/pony/pkg/payload"
	"github.com/ponyruntime/pony/pkg/payload/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupTest(*testing.T) *ServerManager {
	logger := zap.NewNop()
	bus := eventbus.NewBus()

	tr := transcoder.NewTranscoder()
	json.Register(tr)

	manager := NewManager(bus, tr, func(writer httpbase.ResponseWriter, _ *httpbase.Request) {
		_, _ = writer.Write([]byte("Hello, World!"))
	}, logger)

	return manager
}

func TestServerManager_ServerOperations(t *testing.T) {
	ctx := context.Background()

	t.Run("create server", func(t *testing.T) {
		manager := setupTest(t)

		entry := registry.Entry{
			ID:   "test-server",
			Kind: http.KindServer,
			Data: payload.New(map[string]interface{}{
				"addr": ":8080",
				"timeouts": map[string]interface{}{
					"read":  "5s",
					"write": "5s",
					"idle":  "60s",
				},
			}),
		}

		err := manager.Add(ctx, entry)
		require.NoError(t, err)

		// Verify server was created
		_, exists := manager.servers[entry.ID]
		assert.True(t, exists)
	})

	t.Run("update server", func(t *testing.T) {
		manager := setupTest(t)

		// First create the server
		entry := registry.Entry{
			ID:   "test-server",
			Kind: http.KindServer,
			Data: payload.New(map[string]interface{}{
				"addr": ":8080",
			}),
		}
		require.NoError(t, manager.Add(ctx, entry))

		// Then update it
		entry.Data = payload.New(map[string]interface{}{
			"addr": ":8081",
		})
		err := manager.Update(ctx, entry)
		require.NoError(t, err)
	})

	t.Run("delete server", func(t *testing.T) {
		manager := setupTest(t)

		entry := registry.Entry{
			ID:   "test-server",
			Kind: http.KindServer,
			Data: payload.New(map[string]interface{}{
				"addr": ":8080",
			}),
		}
		require.NoError(t, manager.Add(ctx, entry))

		err := manager.Delete(ctx, entry)
		require.NoError(t, err)

		// Verify server was deleted
		_, exists := manager.servers[entry.ID]
		assert.False(t, exists)
	})
}

func TestServerManager_RouterOperations(t *testing.T) {
	ctx := context.Background()

	t.Run("router lifecycle", func(t *testing.T) {
		manager := setupTest(t)

		// Create server first
		serverEntry := registry.Entry{
			ID:   "test-server",
			Kind: http.KindServer,
			Data: payload.New(map[string]interface{}{
				"addr": ":8080",
			}),
		}
		require.NoError(t, manager.Add(ctx, serverEntry))

		// Create router
		routerEntry := registry.Entry{
			ID:   "test-router",
			Kind: http.KindRouter,
			Data: payload.New(map[string]interface{}{
				"prefix": "/api",
				"meta": map[string]interface{}{
					"server": "test-server",
				},
			}),
		}
		require.NoError(t, manager.Add(ctx, routerEntry))

		// Update router
		routerEntry.Data = payload.New(map[string]interface{}{
			"prefix": "/api/v2",
			"meta": map[string]interface{}{
				"server": "test-server",
			},
		})
		require.NoError(t, manager.Update(ctx, routerEntry))

		// Delete router
		require.NoError(t, manager.Delete(ctx, routerEntry))
	})

	t.Run("router server migration", func(t *testing.T) {
		manager := setupTest(t)

		// Create two servers
		for i, id := range []string{"server1", "server2"} {
			require.NoError(t, manager.Add(ctx, registry.Entry{
				ID:   registry.ID(id),
				Kind: http.KindServer,
				Data: payload.New(map[string]interface{}{
					"addr": ":808" + string(rune('0'+i)),
				}),
			}))
		}

		// Create router on server1
		routerEntry := registry.Entry{
			ID:   "test-router",
			Kind: http.KindRouter,
			Data: payload.New(map[string]interface{}{
				"prefix": "/api",
				"meta": map[string]interface{}{
					"server": "server1",
				},
			}),
		}
		require.NoError(t, manager.Add(ctx, routerEntry))
		assert.Equal(t, registry.ID("server1"), manager.routerServers[routerEntry.ID])

		// Migrate to server2
		routerEntry.Data = payload.New(map[string]interface{}{
			"prefix": "/api",
			"meta": map[string]interface{}{
				"server": "server2",
			},
		})
		require.NoError(t, manager.Update(ctx, routerEntry))
		assert.Equal(t, registry.ID("server2"), manager.routerServers[routerEntry.ID])
	})
}

func TestServerManager_EndpointOperations(t *testing.T) {
	ctx := context.Background()

	t.Run("endpoint lifecycle", func(t *testing.T) {
		manager := setupTest(t)

		// Create server first
		serverEntry := registry.Entry{
			ID:   "test-server",
			Kind: http.KindServer,
			Data: payload.New(map[string]interface{}{
				"addr": ":8080",
			}),
		}
		require.NoError(t, manager.Add(ctx, serverEntry))

		// Create endpoint
		endpointEntry := registry.Entry{
			ID:   "test-endpoint",
			Kind: http.KindEndpoint,
			Data: payload.New(map[string]interface{}{
				"path":   "/test",
				"method": "GET",
				"meta": map[string]interface{}{
					"server": "test-server",
				},
			}),
		}
		require.NoError(t, manager.Add(ctx, endpointEntry))

		// Update endpoint
		endpointEntry.Data = payload.New(map[string]interface{}{
			"path":   "/test/v2",
			"method": "POST",
			"meta": map[string]interface{}{
				"server": "test-server",
			},
		})
		require.NoError(t, manager.Update(ctx, endpointEntry))

		// Delete endpoint
		require.NoError(t, manager.Delete(ctx, endpointEntry))
	})

	t.Run("endpoint server migration", func(t *testing.T) {
		manager := setupTest(t)

		// Create two servers
		for i, id := range []string{"server1", "server2"} {
			require.NoError(t, manager.Add(ctx, registry.Entry{
				ID:   registry.ID(id),
				Kind: http.KindServer,
				Data: payload.New(map[string]interface{}{
					"addr": ":808" + string(rune('0'+i)),
				}),
			}))
		}

		// Create endpoint on server1
		endpointEntry := registry.Entry{
			ID:   "test-endpoint",
			Kind: http.KindEndpoint,
			Data: payload.New(map[string]interface{}{
				"path":   "/test",
				"method": "GET",
				"meta": map[string]interface{}{
					"server": "server1",
				},
			}),
		}
		require.NoError(t, manager.Add(ctx, endpointEntry))
		assert.Equal(t, registry.ID("server1"), manager.endpointServers[endpointEntry.ID])

		// Migrate to server2
		endpointEntry.Data = payload.New(map[string]interface{}{
			"path":   "/test",
			"method": "GET",
			"meta": map[string]interface{}{
				"server": "server2",
			},
		})
		require.NoError(t, manager.Update(ctx, endpointEntry))
		assert.Equal(t, registry.ID("server2"), manager.endpointServers[endpointEntry.ID])
	})
}

func TestServerManager_ErrorCases(t *testing.T) {
	ctx := context.Background()

	t.Run("invalid config", func(t *testing.T) {
		manager := setupTest(t)

		entry := registry.Entry{
			ID:   "test-server",
			Kind: http.KindServer,
			Data: payload.New(map[string]interface{}{
				"invalid": "config",
			}),
		}

		err := manager.Add(ctx, entry)
		assert.Error(t, err)
	})

	t.Run("missing config", func(t *testing.T) {
		manager := setupTest(t)

		entry := registry.Entry{
			ID:   "test-server",
			Kind: http.KindServer,
		}

		err := manager.Add(ctx, entry)
		assert.Error(t, err)
	})

	t.Run("server not found", func(t *testing.T) {
		manager := setupTest(t)

		entry := registry.Entry{
			ID:   "test-router",
			Kind: http.KindRouter,
			Data: payload.New(map[string]interface{}{
				"prefix": "/api",
				"meta": map[string]interface{}{
					"server": "nonexistent-server",
				},
			}),
		}

		err := manager.Add(ctx, entry)
		assert.Error(t, err)
	})
}
