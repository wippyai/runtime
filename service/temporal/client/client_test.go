package client

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	api "github.com/wippyai/runtime/api/service/temporal"
	"github.com/wippyai/runtime/api/supervisor"
	"go.uber.org/zap"
)

func TestClient_Start(t *testing.T) {
	t.Run("start client with health check disabled", func(t *testing.T) {
		logger := zap.NewNop()
		temporalClient := &mockTemporalClient{}
		config := &api.ClientConfig{
			HealthCheck: api.HealthCheckConfig{
				Enabled: false,
			},
		}

		client := NewClient(
			registry.NewID("test", "client1"),
			logger,
			temporalClient,
			config,
		)

		ctx := context.Background()
		statusCh, err := client.Start(ctx)
		require.NoError(t, err)
		require.NotNil(t, statusCh)

		select {
		case status := <-statusCh:
			assert.Equal(t, supervisor.Running, status)
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for status")
		}
	})

	t.Run("start client with health check enabled", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping slow health check test in short mode")
		}

		logger := zap.NewNop()
		temporalClient := &mockTemporalClient{}
		config := &api.ClientConfig{
			HealthCheck: api.HealthCheckConfig{
				Enabled:  true,
				Interval: 100 * time.Millisecond,
			},
		}

		client := NewClient(
			registry.NewID("test", "client1"),
			logger,
			temporalClient,
			config,
		)

		ctx := context.Background()
		statusCh, err := client.Start(ctx)
		require.NoError(t, err)
		require.NotNil(t, statusCh)

		select {
		case status := <-statusCh:
			assert.Equal(t, supervisor.Running, status)
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for initial status")
		}

		time.Sleep(150 * time.Millisecond)

		select {
		case status := <-statusCh:
			assert.Equal(t, supervisor.Running, status)
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for health check status")
		}

		err = client.Stop(context.Background())
		require.NoError(t, err)
	})

	t.Run("cannot start closed client", func(t *testing.T) {
		logger := zap.NewNop()
		temporalClient := &mockTemporalClient{}
		config := &api.ClientConfig{
			HealthCheck: api.HealthCheckConfig{
				Enabled: false,
			},
		}

		client := NewClient(
			registry.NewID("test", "client1"),
			logger,
			temporalClient,
			config,
		)

		err := client.Stop(context.Background())
		require.NoError(t, err)

		_, err = client.Start(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "client is closed")
	})
}

func TestClient_Stop(t *testing.T) {
	t.Run("stop client cleanly", func(t *testing.T) {
		logger := zap.NewNop()
		temporalClient := &mockTemporalClient{}
		config := &api.ClientConfig{
			HealthCheck: api.HealthCheckConfig{
				Enabled: false,
			},
		}

		client := NewClient(
			registry.NewID("test", "client1"),
			logger,
			temporalClient,
			config,
		)

		ctx := context.Background()
		_, err := client.Start(ctx)
		require.NoError(t, err)

		err = client.Stop(context.Background())
		require.NoError(t, err)
		assert.True(t, client.closed.Load())
	})

	t.Run("stop client with health check", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping slow health check test in short mode")
		}

		logger := zap.NewNop()
		temporalClient := &mockTemporalClient{}
		config := &api.ClientConfig{
			HealthCheck: api.HealthCheckConfig{
				Enabled:  true,
				Interval: 50 * time.Millisecond,
			},
		}

		client := NewClient(
			registry.NewID("test", "client1"),
			logger,
			temporalClient,
			config,
		)

		ctx := context.Background()
		_, err := client.Start(ctx)
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		err = client.Stop(context.Background())
		require.NoError(t, err)
		assert.True(t, client.closed.Load())
	})

	t.Run("stop already stopped client is idempotent", func(t *testing.T) {
		logger := zap.NewNop()
		temporalClient := &mockTemporalClient{}
		config := &api.ClientConfig{
			HealthCheck: api.HealthCheckConfig{
				Enabled: false,
			},
		}

		client := NewClient(
			registry.NewID("test", "client1"),
			logger,
			temporalClient,
			config,
		)

		err := client.Stop(context.Background())
		require.NoError(t, err)

		err = client.Stop(context.Background())
		require.NoError(t, err)
	})
}

func TestClient_Acquire(t *testing.T) {
	t.Run("acquire resource from running client", func(t *testing.T) {
		logger := zap.NewNop()
		temporalClient := &mockTemporalClient{}
		config := &api.ClientConfig{
			TQPrefix: "dev:",
			HealthCheck: api.HealthCheckConfig{
				Enabled: false,
			},
		}

		client := NewClient(
			registry.NewID("test", "client1"),
			logger,
			temporalClient,
			config,
		)

		ctx := context.Background()
		res, err := client.Acquire(ctx, registry.NewID("test", "client1"), resource.ModeNormal)
		require.NoError(t, err)
		require.NotNil(t, res)

		clientRes, ok := res.(*clientResourceImpl)
		require.True(t, ok)
		assert.Equal(t, "dev:", clientRes.prefix)
	})

	t.Run("cannot acquire from closed client", func(t *testing.T) {
		logger := zap.NewNop()
		temporalClient := &mockTemporalClient{}
		config := &api.ClientConfig{
			HealthCheck: api.HealthCheckConfig{
				Enabled: false,
			},
		}

		client := NewClient(
			registry.NewID("test", "client1"),
			logger,
			temporalClient,
			config,
		)

		err := client.Stop(context.Background())
		require.NoError(t, err)

		ctx := context.Background()
		res, err := client.Acquire(ctx, registry.NewID("test", "client1"), resource.ModeNormal)
		assert.Error(t, err)
		assert.Nil(t, res)
		assert.Contains(t, err.Error(), "client is closed")
	})
}

func TestClient_GetTaskQueueName(t *testing.T) {
	t.Run("get task queue name with prefix", func(t *testing.T) {
		logger := zap.NewNop()
		temporalClient := &mockTemporalClient{}
		config := &api.ClientConfig{
			TQPrefix: "dev:",
		}

		client := NewClient(
			registry.NewID("test", "client1"),
			logger,
			temporalClient,
			config,
		)

		queueName := client.GetTaskQueueName("my-queue")
		assert.Equal(t, "dev:my-queue", queueName)
	})

	t.Run("get task queue name without prefix", func(t *testing.T) {
		logger := zap.NewNop()
		temporalClient := &mockTemporalClient{}
		config := &api.ClientConfig{
			TQPrefix: "",
		}

		client := NewClient(
			registry.NewID("test", "client1"),
			logger,
			temporalClient,
			config,
		)

		queueName := client.GetTaskQueueName("my-queue")
		assert.Equal(t, "my-queue", queueName)
	})
}

func TestClientResource_Get(t *testing.T) {
	t.Run("get temporal client from resource", func(t *testing.T) {
		temporalClient := &mockTemporalClient{}
		var wg sync.WaitGroup
		wg.Add(1)

		res := &clientResourceImpl{
			client: temporalClient,
			prefix: "dev:",
			wg:     &wg,
		}

		result, err := res.Get()
		require.NoError(t, err)
		clientRes, ok := result.(api.ClientResource)
		require.True(t, ok)
		assert.Equal(t, temporalClient, clientRes.Client)
		assert.Equal(t, "dev:", clientRes.TQPrefix)
	})

	t.Run("get from released resource fails", func(t *testing.T) {
		temporalClient := &mockTemporalClient{}
		var wg sync.WaitGroup
		wg.Add(1)

		res := &clientResourceImpl{
			client: temporalClient,
			prefix: "dev:",
			wg:     &wg,
		}

		res.Release()

		result, err := res.Get()
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "resource has been released")
	})
}

func TestClientResource_GetTaskQueueName(t *testing.T) {
	t.Run("apply prefix to task queue name", func(t *testing.T) {
		res := api.ClientResource{
			TQPrefix: "prod:",
		}

		queueName := res.GetTaskQueueName("my-queue")
		assert.Equal(t, "prod:my-queue", queueName)
	})

	t.Run("no prefix returns original name", func(t *testing.T) {
		res := api.ClientResource{
			TQPrefix: "",
		}

		queueName := res.GetTaskQueueName("my-queue")
		assert.Equal(t, "my-queue", queueName)
	})
}

func TestClientResource_Release(t *testing.T) {
	t.Run("release resource once", func(t *testing.T) {
		var wg sync.WaitGroup
		wg.Add(1)

		res := &clientResourceImpl{
			wg: &wg,
		}

		res.Release()
		assert.True(t, res.released.Load())
	})

	t.Run("release resource multiple times is idempotent", func(t *testing.T) {
		var wg sync.WaitGroup
		wg.Add(1)

		res := &clientResourceImpl{
			wg: &wg,
		}

		res.Release()
		res.Release()
		res.Release()

		assert.True(t, res.released.Load())
	})
}
