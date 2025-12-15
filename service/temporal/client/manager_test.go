package client

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/service/temporal"
	"github.com/wippyai/runtime/api/supervisor"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.uber.org/zap"
)

type mockTranscoder struct{}

func (m *mockTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

func (m *mockTranscoder) Unmarshal(p payload.Payload, v interface{}) error {
	data := p.Data()
	if bytes, ok := data.([]byte); ok {
		return json.Unmarshal(bytes, v)
	}
	if str, ok := data.(string); ok {
		return json.Unmarshal([]byte(str), v)
	}
	return fmt.Errorf("unsupported data type")
}

type mockEventBus struct {
	events []event.Event
}

func (m *mockEventBus) Send(_ context.Context, evt event.Event) {
	m.events = append(m.events, evt)
}

func (m *mockEventBus) Subscribe(_ context.Context, _ event.System, _ chan<- event.Event) (event.SubscriberID, error) {
	return event.SubscriberID("mock"), nil
}

func (m *mockEventBus) SubscribeP(_ context.Context, _ event.System, _ event.Kind, _ chan<- event.Event) (event.SubscriberID, error) {
	return event.SubscriberID("mock"), nil
}

func (m *mockEventBus) Unsubscribe(_ context.Context, _ event.SubscriberID) {
}

type mockEnvRegistry struct {
	values map[string]string
}

func (m *mockEnvRegistry) Get(_ context.Context, key string) (string, error) {
	if val, ok := m.values[key]; ok {
		return val, nil
	}
	return "", fmt.Errorf("not found")
}

func (m *mockEnvRegistry) Lookup(_ context.Context, key string) (string, bool, error) {
	val, ok := m.values[key]
	return val, ok, nil
}

func (m *mockEnvRegistry) Set(_ context.Context, key string, value string) error {
	m.values[key] = value
	return nil
}

func (m *mockEnvRegistry) All(_ context.Context) (map[string]string, error) {
	return m.values, nil
}

func (m *mockEnvRegistry) GetStorage(_ context.Context, _ registry.ID) (env.Storage, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockEnvRegistry) RegisterStorage(_ registry.ID, _ env.Storage) {
}

type mockClientFactory struct {
	createFunc func(ctx context.Context, logger *zap.Logger, id registry.ID, config *api.ClientConfig) (*Client, error)
}

func (m *mockClientFactory) CreateClient(ctx context.Context, logger *zap.Logger, id registry.ID, config *api.ClientConfig) (*Client, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, logger, id, config)
	}
	return &Client{
		id:     id,
		log:    logger,
		config: config,
	}, nil
}

type mockTemporalClient struct {
	client.Client
}

func (m *mockTemporalClient) Close() {}

func (m *mockTemporalClient) CheckHealth(_ context.Context, _ *client.CheckHealthRequest) (*client.CheckHealthResponse, error) {
	return &client.CheckHealthResponse{}, nil
}

func setupManager(t *testing.T) (*Manager, *mockEventBus) {
	logger := zap.NewNop()
	transcoder := &mockTranscoder{}
	bus := &mockEventBus{events: make([]event.Event, 0)}
	envReg := &mockEnvRegistry{values: make(map[string]string)}

	factory := &mockClientFactory{
		createFunc: func(_ context.Context, logger *zap.Logger, id registry.ID, config *api.ClientConfig) (*Client, error) {
			return &Client{
				id:     id,
				log:    logger,
				client: &mockTemporalClient{},
				config: config,
			}, nil
		},
	}

	manager, err := NewManagerWithFactory(
		logger,
		transcoder,
		bus,
		envReg,
		factory,
		converter.GetDefaultDataConverter(),
		nil,
	)
	require.NoError(t, err)

	return manager, bus
}

func TestNewManager(t *testing.T) {
	t.Run("create manager with valid dependencies", func(t *testing.T) {
		logger := zap.NewNop()
		transcoder := &mockTranscoder{}
		bus := &mockEventBus{}
		envReg := &mockEnvRegistry{values: make(map[string]string)}

		manager, err := NewManager(
			logger,
			transcoder,
			bus,
			envReg,
			converter.GetDefaultDataConverter(),
			nil,
		)

		require.NoError(t, err)
		assert.NotNil(t, manager)
		assert.NotNil(t, manager.factory)
	})

	t.Run("nil logger returns error", func(t *testing.T) {
		transcoder := &mockTranscoder{}
		bus := &mockEventBus{}
		envReg := &mockEnvRegistry{values: make(map[string]string)}

		manager, err := NewManager(
			nil,
			transcoder,
			bus,
			envReg,
			converter.GetDefaultDataConverter(),
			nil,
		)

		assert.Error(t, err)
		assert.Nil(t, manager)
		assert.Contains(t, err.Error(), "logger is required")
	})

	t.Run("nil transcoder returns error", func(t *testing.T) {
		logger := zap.NewNop()
		bus := &mockEventBus{}
		envReg := &mockEnvRegistry{values: make(map[string]string)}

		manager, err := NewManager(
			logger,
			nil,
			bus,
			envReg,
			converter.GetDefaultDataConverter(),
			nil,
		)

		assert.Error(t, err)
		assert.Nil(t, manager)
		assert.Contains(t, err.Error(), "transcoder is required")
	})

	t.Run("nil event bus returns error", func(t *testing.T) {
		logger := zap.NewNop()
		transcoder := &mockTranscoder{}
		envReg := &mockEnvRegistry{values: make(map[string]string)}

		manager, err := NewManager(
			logger,
			transcoder,
			nil,
			envReg,
			converter.GetDefaultDataConverter(),
			nil,
		)

		assert.Error(t, err)
		assert.Nil(t, manager)
		assert.Contains(t, err.Error(), "event bus is required")
	})

	t.Run("nil env registry returns error", func(t *testing.T) {
		logger := zap.NewNop()
		transcoder := &mockTranscoder{}
		bus := &mockEventBus{}

		manager, err := NewManager(
			logger,
			transcoder,
			bus,
			nil,
			converter.GetDefaultDataConverter(),
			nil,
		)

		assert.Error(t, err)
		assert.Nil(t, manager)
		assert.Contains(t, err.Error(), "env registry is required")
	})
}

func TestManager_AddClient(t *testing.T) {
	t.Run("add valid client config", func(t *testing.T) {
		manager, bus := setupManager(t)

		id := registry.NewID("test", "client1")
		cfg := &api.ClientConfig{
			Address:   "localhost:7233",
			Namespace: "default",
			Auth: api.AuthConfig{
				Type: api.AuthTypeNone,
			},
		}

		err := manager.AddClient(context.Background(), id, cfg)
		require.NoError(t, err)

		assert.True(t, manager.Has(id))
		assert.Len(t, bus.events, 2) // supervisor.ServiceRegister + resource.Register
	})

	t.Run("add client without address fails", func(t *testing.T) {
		manager, _ := setupManager(t)

		id := registry.NewID("test", "client1")
		cfg := &api.ClientConfig{
			Namespace: "default",
			Auth: api.AuthConfig{
				Type: api.AuthTypeNone,
			},
		}

		err := manager.AddClient(context.Background(), id, cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "address is required")
	})

	t.Run("add duplicate client fails", func(t *testing.T) {
		manager, _ := setupManager(t)

		id := registry.NewID("test", "client1")
		cfg := &api.ClientConfig{
			Address:   "localhost:7233",
			Namespace: "default",
			Auth: api.AuthConfig{
				Type: api.AuthTypeNone,
			},
		}

		err := manager.AddClient(context.Background(), id, cfg)
		require.NoError(t, err)

		err = manager.AddClient(context.Background(), id, cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already initialized")
	})

	t.Run("defaults are initialized", func(t *testing.T) {
		manager, _ := setupManager(t)

		id := registry.NewID("test", "client1")
		cfg := &api.ClientConfig{
			Address: "localhost:7233",
			Auth: api.AuthConfig{
				Type: api.AuthTypeNone,
			},
		}

		err := manager.AddClient(context.Background(), id, cfg)
		require.NoError(t, err)

		storedCfg, exists := manager.GetConfig(id)
		require.True(t, exists)
		assert.Equal(t, "default", storedCfg.Namespace)
		assert.Equal(t, 10*time.Second, storedCfg.ConnectionTimeout)
		assert.Equal(t, 30*time.Second, storedCfg.KeepAliveTime)
	})
}

func TestManager_UpdateClient(t *testing.T) {
	t.Run("update existing client", func(t *testing.T) {
		manager, bus := setupManager(t)

		id := registry.NewID("test", "client1")
		cfg := &api.ClientConfig{
			Address:   "localhost:7233",
			Namespace: "default",
			TQPrefix:  "old:",
			Auth: api.AuthConfig{
				Type: api.AuthTypeNone,
			},
		}

		err := manager.AddClient(context.Background(), id, cfg)
		require.NoError(t, err)

		bus.events = nil

		newCfg := &api.ClientConfig{
			Address:   "localhost:7233",
			Namespace: "default",
			TQPrefix:  "new:",
			Auth: api.AuthConfig{
				Type: api.AuthTypeNone,
			},
		}

		err = manager.UpdateClient(context.Background(), id, newCfg)
		require.NoError(t, err)

		storedCfg, exists := manager.GetConfig(id)
		require.True(t, exists)
		assert.Equal(t, "new:", storedCfg.TQPrefix)
		assert.Len(t, bus.events, 1) // supervisor.ServiceUpdate
		assert.Equal(t, supervisor.ServiceUpdate, bus.events[0].Kind)
	})

	t.Run("update non-existent client fails", func(t *testing.T) {
		manager, _ := setupManager(t)

		id := registry.NewID("test", "nonexistent")
		cfg := &api.ClientConfig{
			Address: "localhost:7233",
			Auth: api.AuthConfig{
				Type: api.AuthTypeNone,
			},
		}

		err := manager.UpdateClient(context.Background(), id, cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestManager_DeleteClient(t *testing.T) {
	t.Run("delete existing client", func(t *testing.T) {
		manager, bus := setupManager(t)

		id := registry.NewID("test", "client1")
		cfg := &api.ClientConfig{
			Address:   "localhost:7233",
			Namespace: "default",
			Auth: api.AuthConfig{
				Type: api.AuthTypeNone,
			},
		}

		err := manager.AddClient(context.Background(), id, cfg)
		require.NoError(t, err)

		bus.events = nil

		err = manager.DeleteClient(context.Background(), id)
		require.NoError(t, err)

		assert.False(t, manager.Has(id))
		assert.Len(t, bus.events, 2) // supervisor.ServiceRemove + resource.Delete
	})

	t.Run("delete non-existent client fails", func(t *testing.T) {
		manager, _ := setupManager(t)

		id := registry.NewID("test", "nonexistent")
		err := manager.DeleteClient(context.Background(), id)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestManager_GetClient(t *testing.T) {
	t.Run("get existing client", func(t *testing.T) {
		manager, _ := setupManager(t)

		id := registry.NewID("test", "client1")
		cfg := &api.ClientConfig{
			Address:   "localhost:7233",
			Namespace: "default",
			Auth: api.AuthConfig{
				Type: api.AuthTypeNone,
			},
		}

		err := manager.AddClient(context.Background(), id, cfg)
		require.NoError(t, err)

		client, err := manager.GetClient(id)
		require.NoError(t, err)
		assert.NotNil(t, client)
		assert.Equal(t, id, client.id)
	})

	t.Run("get non-existent client fails", func(t *testing.T) {
		manager, _ := setupManager(t)

		id := registry.NewID("test", "nonexistent")
		client, err := manager.GetClient(id)
		assert.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "not initialized")
	})
}

func TestManager_EntryListener(t *testing.T) {
	t.Run("add entry with correct kind", func(t *testing.T) {
		manager, _ := setupManager(t)

		cfg := &api.ClientConfig{
			Address:   "localhost:7233",
			Namespace: "default",
			Auth: api.AuthConfig{
				Type: api.AuthTypeNone,
			},
		}

		data, err := json.Marshal(cfg)
		require.NoError(t, err)

		entry := registry.Entry{
			ID:   registry.NewID("test", "client1"),
			Kind: api.Client,
			Data: payload.New(data),
		}

		err = manager.Add(context.Background(), entry)
		require.NoError(t, err)
		assert.True(t, manager.Has(entry.ID))
	})

	t.Run("add entry with wrong kind fails", func(t *testing.T) {
		manager, _ := setupManager(t)

		entry := registry.Entry{
			ID:   registry.NewID("test", "wrong"),
			Kind: "wrong.kind",
		}

		err := manager.Add(context.Background(), entry)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected entry kind")
	})

	t.Run("update entry", func(t *testing.T) {
		manager, _ := setupManager(t)

		cfg := &api.ClientConfig{
			Address:   "localhost:7233",
			Namespace: "default",
			TQPrefix:  "old:",
			Auth: api.AuthConfig{
				Type: api.AuthTypeNone,
			},
		}

		data, err := json.Marshal(cfg)
		require.NoError(t, err)

		entry := registry.Entry{
			ID:   registry.NewID("test", "client1"),
			Kind: api.Client,
			Data: payload.New(data),
		}

		err = manager.Add(context.Background(), entry)
		require.NoError(t, err)

		cfg.TQPrefix = "new:"
		data, err = json.Marshal(cfg)
		require.NoError(t, err)
		entry.Data = payload.New(data)

		err = manager.Update(context.Background(), entry)
		require.NoError(t, err)

		storedCfg, exists := manager.GetConfig(entry.ID)
		require.True(t, exists)
		assert.Equal(t, "new:", storedCfg.TQPrefix)
	})

	t.Run("delete entry", func(t *testing.T) {
		manager, _ := setupManager(t)

		cfg := &api.ClientConfig{
			Address:   "localhost:7233",
			Namespace: "default",
			Auth: api.AuthConfig{
				Type: api.AuthTypeNone,
			},
		}

		data, err := json.Marshal(cfg)
		require.NoError(t, err)

		entry := registry.Entry{
			ID:   registry.NewID("test", "client1"),
			Kind: api.Client,
			Data: payload.New(data),
		}

		err = manager.Add(context.Background(), entry)
		require.NoError(t, err)

		err = manager.Delete(context.Background(), entry)
		require.NoError(t, err)
		assert.False(t, manager.Has(entry.ID))
	})
}

func TestManager_GetTaskQueueName(t *testing.T) {
	t.Run("get task queue name with prefix", func(t *testing.T) {
		manager, _ := setupManager(t)

		id := registry.NewID("test", "client1")
		cfg := &api.ClientConfig{
			Address:   "localhost:7233",
			Namespace: "default",
			TQPrefix:  "dev:",
			Auth: api.AuthConfig{
				Type: api.AuthTypeNone,
			},
		}

		err := manager.AddClient(context.Background(), id, cfg)
		require.NoError(t, err)

		queueName, err := manager.GetTaskQueueName(id, "my-queue")
		require.NoError(t, err)
		assert.Equal(t, "dev:my-queue", queueName)
	})

	t.Run("get task queue name without prefix", func(t *testing.T) {
		manager, _ := setupManager(t)

		id := registry.NewID("test", "client1")
		cfg := &api.ClientConfig{
			Address:   "localhost:7233",
			Namespace: "default",
			Auth: api.AuthConfig{
				Type: api.AuthTypeNone,
			},
		}

		err := manager.AddClient(context.Background(), id, cfg)
		require.NoError(t, err)

		queueName, err := manager.GetTaskQueueName(id, "my-queue")
		require.NoError(t, err)
		assert.Equal(t, "my-queue", queueName)
	})

	t.Run("get task queue name for non-existent client fails", func(t *testing.T) {
		manager, _ := setupManager(t)

		id := registry.NewID("test", "nonexistent")
		queueName, err := manager.GetTaskQueueName(id, "my-queue")
		assert.Error(t, err)
		assert.Empty(t, queueName)
	})
}

func TestManager_ConcurrentAccess(t *testing.T) {
	t.Run("concurrent add and get operations", func(t *testing.T) {
		manager, _ := setupManager(t)

		done := make(chan bool, 2)

		go func() {
			for i := 0; i < 10; i++ {
				id := registry.NewID("test", "client"+string(rune('A'+i)))
				cfg := &api.ClientConfig{
					Address:   "localhost:7233",
					Namespace: "default",
					Auth: api.AuthConfig{
						Type: api.AuthTypeNone,
					},
				}
				_ = manager.AddClient(context.Background(), id, cfg)
			}
			done <- true
		}()

		go func() {
			for i := 0; i < 10; i++ {
				id := registry.NewID("test", "client"+string(rune('A'+i)))
				_, _ = manager.GetClient(id)
				_ = manager.Has(id)
			}
			done <- true
		}()

		<-done
		<-done
	})
}
