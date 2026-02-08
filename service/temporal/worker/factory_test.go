package worker

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/service/temporal"
	luapayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	systempayload "github.com/wippyai/runtime/system/payload"
	"go.temporal.io/sdk/interceptor"
	"go.uber.org/zap"
)

func newFactoryTestTranscoder() payload.Transcoder {
	transcoder := systempayload.NewTranscoder()
	luapayload.RegisterAllBasicFormats(transcoder)
	return transcoder
}

func TestNewDefaultWorkerFactory(t *testing.T) {
	t.Run("creates factory without interceptors", func(t *testing.T) {
		factory := NewDefaultWorkerFactory(nil, nil, newFactoryTestTranscoder())
		require.NotNil(t, factory)
		assert.Nil(t, factory.interceptors)
	})

	t.Run("creates factory with interceptors", func(t *testing.T) {
		interceptors := make([]interceptor.WorkerInterceptor, 0)
		factory := NewDefaultWorkerFactory(nil, interceptors, newFactoryTestTranscoder())
		require.NotNil(t, factory)
		assert.NotNil(t, factory.interceptors)
	})
}

func TestDefaultWorkerFactory_CreateWorker(t *testing.T) {
	logger := zap.NewNop()
	factory := NewDefaultWorkerFactory(nil, nil, newFactoryTestTranscoder())

	t.Run("creates worker with valid config", func(t *testing.T) {
		config := &api.WorkerConfig{
			TaskQueue: "test-queue",
		}
		id := registry.ParseID("test/worker")

		worker, err := factory.CreateWorker(context.Background(), logger, id, config, nil)
		require.NoError(t, err)
		require.NotNil(t, worker)
		assert.Equal(t, id, worker.id)
		assert.Equal(t, "test-queue", worker.config.TaskQueue)
	})

	t.Run("creates worker with empty task queue", func(t *testing.T) {
		config := &api.WorkerConfig{}
		id := registry.ParseID("test/empty-queue")

		worker, err := factory.CreateWorker(context.Background(), logger, id, config, nil)
		require.NoError(t, err)
		require.NotNil(t, worker)
		assert.Empty(t, worker.config.TaskQueue)
	})
}

func TestDefaultWorkerFactory_ImplementsFactory(t *testing.T) {
	var _ Factory = (*DefaultWorkerFactory)(nil)
}
