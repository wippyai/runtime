package http

import (
	"context"
	httpbase "net/http"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/service/http"

	"github.com/ponyruntime/pony/system/eventbus"
	transcoder "github.com/ponyruntime/pony/system/payload"
	"github.com/ponyruntime/pony/system/payload/json"
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
			ID: registry.ID{
				NS:   "test",
				Name: "test-server",
			},
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
		serverID := registry.ID{NS: "test", Name: "test-server"}
		entry := registry.Entry{
			ID:   serverID,
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

		serverID := registry.ID{NS: "test", Name: "test-server"}
		entry := registry.Entry{
			ID:   serverID,
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
		serverID := registry.ID{NS: "test", Name: "test-server"}
		serverEntry := registry.Entry{
			ID:   serverID,
			Kind: http.KindServer,
			Data: payload.New(map[string]interface{}{
				"addr": ":8080",
			}),
		}
		require.NoError(t, manager.Add(ctx, serverEntry))

		// Create router
		routerID := registry.ID{NS: "test", Name: "test-router"}
		routerEntry := registry.Entry{
			ID:   routerID,
			Kind: http.KindRouter,
			Data: payload.New(map[string]interface{}{
				"prefix": "/api",
				"meta": map[string]interface{}{
					"server": "test:test-server",
				},
			}),
		}
		require.NoError(t, manager.Add(ctx, routerEntry))

		// Update router
		routerEntry.Data = payload.New(map[string]interface{}{
			"prefix": "/api/v2",
			"meta": map[string]interface{}{
				"server": "test:test-server",
			},
		})
		require.NoError(t, manager.Update(ctx, routerEntry))

		// Delete router
		require.NoError(t, manager.Delete(ctx, routerEntry))
	})

	t.Run("router server migration", func(t *testing.T) {
		manager := setupTest(t)

		// Create two servers
		for _, server := range []struct {
			id   registry.ID
			addr string
		}{
			{registry.ID{NS: "test", Name: "server1"}, ":8080"},
			{registry.ID{NS: "test", Name: "server2"}, ":8081"},
		} {
			require.NoError(t, manager.Add(ctx, registry.Entry{
				ID:   server.id,
				Kind: http.KindServer,
				Data: payload.New(map[string]interface{}{
					"addr": server.addr,
				}),
			}))
		}

		// Create router on server1
		routerID := registry.ID{NS: "test", Name: "test-router"}
		routerEntry := registry.Entry{
			ID:   routerID,
			Kind: http.KindRouter,
			Data: payload.New(map[string]interface{}{
				"prefix": "/api",
				"meta": map[string]interface{}{
					"server": "test:server1",
				},
			}),
		}
		require.NoError(t, manager.Add(ctx, routerEntry))
		server1ID := registry.ID{NS: "test", Name: "server1"}
		assert.Equal(t, server1ID, manager.routerServers[routerEntry.ID])

		// Migrate to server2
		routerEntry.Data = payload.New(map[string]interface{}{
			"prefix": "/api",
			"meta": map[string]interface{}{
				"server": "test:server2",
			},
		})
		require.NoError(t, manager.Update(ctx, routerEntry))
		server2ID := registry.ID{NS: "test", Name: "server2"}
		assert.Equal(t, server2ID, manager.routerServers[routerEntry.ID])
	})
}

func TestServerManager_EndpointOperations(t *testing.T) {
	ctx := context.Background()

	t.Run("endpoint lifecycle", func(t *testing.T) {
		manager := setupTest(t)

		// Create server first
		serverID := registry.ID{NS: "test", Name: "test-server"}
		serverEntry := registry.Entry{
			ID:   serverID,
			Kind: http.KindServer,
			Data: payload.New(map[string]interface{}{
				"addr": ":8080",
			}),
		}
		require.NoError(t, manager.Add(ctx, serverEntry))

		// Create endpoint
		endpointID := registry.ID{NS: "test", Name: "test-endpoint"}
		endpointEntry := registry.Entry{
			ID:   endpointID,
			Kind: http.KindEndpoint,
			Data: payload.New(map[string]interface{}{
				"path":   "/test",
				"method": "GET",
				"meta": map[string]interface{}{
					"server": "test:test-server",
				},
			}),
		}
		require.NoError(t, manager.Add(ctx, endpointEntry))

		// Update endpoint
		endpointEntry.Data = payload.New(map[string]interface{}{
			"path":   "/test/v2",
			"method": "POST",
			"meta": map[string]interface{}{
				"server": "test:test-server",
			},
		})
		require.NoError(t, manager.Update(ctx, endpointEntry))

		// Delete endpoint
		require.NoError(t, manager.Delete(ctx, endpointEntry))
	})

	t.Run("endpoint server migration", func(t *testing.T) {
		manager := setupTest(t)

		// Create two servers
		for _, server := range []struct {
			id   registry.ID
			addr string
		}{
			{registry.ID{NS: "test", Name: "server1"}, ":8080"},
			{registry.ID{NS: "test", Name: "server2"}, ":8081"},
		} {
			require.NoError(t, manager.Add(ctx, registry.Entry{
				ID:   server.id,
				Kind: http.KindServer,
				Data: payload.New(map[string]interface{}{
					"addr": server.addr,
				}),
			}))
		}

		// Create endpoint on server1
		endpointID := registry.ID{NS: "test", Name: "test-endpoint"}
		endpointEntry := registry.Entry{
			ID:   endpointID,
			Kind: http.KindEndpoint,
			Data: payload.New(map[string]interface{}{
				"path":   "/test",
				"method": "GET",
				"meta": map[string]interface{}{
					"server": "test:server1",
				},
			}),
		}
		require.NoError(t, manager.Add(ctx, endpointEntry))
		server1ID := registry.ID{NS: "test", Name: "server1"}
		assert.Equal(t, server1ID, manager.endpointServers[endpointEntry.ID])

		// Migrate to server2
		endpointEntry.Data = payload.New(map[string]interface{}{
			"path":   "/test",
			"method": "GET",
			"meta": map[string]interface{}{
				"server": "test:server2",
			},
		})
		require.NoError(t, manager.Update(ctx, endpointEntry))
		server2ID := registry.ID{NS: "test", Name: "server2"}
		assert.Equal(t, server2ID, manager.endpointServers[endpointEntry.ID])
	})
}

func TestServerManager_ErrorCases(t *testing.T) {
	ctx := context.Background()

	t.Run("invalid config", func(t *testing.T) {
		manager := setupTest(t)

		entry := registry.Entry{
			ID:   registry.ID{NS: "test", Name: "test-server"},
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
			ID:   registry.ID{NS: "test", Name: "test-server"},
			Kind: http.KindServer,
		}

		err := manager.Add(ctx, entry)
		assert.Error(t, err)
	})

	t.Run("server not found", func(t *testing.T) {
		manager := setupTest(t)

		entry := registry.Entry{
			ID:   registry.ID{NS: "test", Name: "test-router"},
			Kind: http.KindRouter,
			Data: payload.New(map[string]interface{}{
				"prefix": "/api",
				"meta": map[string]interface{}{
					"server": "test:nonexistent-server",
				},
			}),
		}

		err := manager.Add(ctx, entry)
		assert.Error(t, err)
	})
}
