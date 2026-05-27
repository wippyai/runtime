// SPDX-License-Identifier: MPL-2.0

package consumer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	consumerapi "github.com/wippyai/runtime/api/service/queue/consumer"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

func TestManager_Add(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	queueMgr := &mockQueueManager{}
	funcReg := &mockFuncRegistry{}
	dtt := &mockDTT{}

	manager := NewManager(bus, queueMgr, funcReg, dtt, zap.NewNop())

	config := &consumerapi.Config{
		ConsumerOptions: queueapi.ConsumerOptions{
			Queue:       registry.NewID("test", "queue"),
			Func:        registry.NewID("test", "func"),
			Concurrency: 5,
			Prefetch:    10,
		},
		Lifecycle: supervisor.LifecycleConfig{
			AutoStart: true,
		},
	}

	entry := registry.Entry{
		ID:   registry.NewID("test", "consumer"),
		Kind: "queue.consumer",
		Data: payload.New(config),
	}

	err := manager.Add(ctx, entry)
	require.NoError(t, err)

	// Verify consumer was stored
	val, ok := manager.consumers.Load(entry.ID)
	assert.True(t, ok)
	assert.NotNil(t, val)
}

func TestManager_Add_QueueNotFound(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	queueMgr := &mockQueueManager{
		queueNotFound: true,
	}
	funcReg := &mockFuncRegistry{}
	dtt := &mockDTT{}

	manager := NewManager(bus, queueMgr, funcReg, dtt, zap.NewNop())

	config := &consumerapi.Config{
		ConsumerOptions: queueapi.ConsumerOptions{
			Queue:       registry.NewID("test", "queue"),
			Func:        registry.NewID("test", "func"),
			Concurrency: 5,
			Prefetch:    10,
		},
	}

	entry := registry.Entry{
		ID:   registry.NewID("test", "consumer"),
		Kind: "queue.consumer",
		Data: payload.New(config),
	}

	err := manager.Add(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "queue not found")
}

func TestManager_Add_DriverNotFound(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	queueMgr := &mockQueueManager{
		driverNotFound: true,
	}
	funcReg := &mockFuncRegistry{}
	dtt := &mockDTT{}

	manager := NewManager(bus, queueMgr, funcReg, dtt, zap.NewNop())

	config := &consumerapi.Config{
		ConsumerOptions: queueapi.ConsumerOptions{
			Queue:       registry.NewID("test", "queue"),
			Func:        registry.NewID("test", "func"),
			Concurrency: 5,
			Prefetch:    10,
		},
	}

	entry := registry.Entry{
		ID:   registry.NewID("test", "consumer"),
		Kind: "queue.consumer",
		Data: payload.New(config),
	}

	err := manager.Add(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "driver not found")
}

func TestManager_Update(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	queueMgr := &mockQueueManager{}
	funcReg := &mockFuncRegistry{}
	dtt := &mockDTT{}

	manager := NewManager(bus, queueMgr, funcReg, dtt, zap.NewNop())

	consumerID := registry.NewID("test", "consumer")

	oldConfig := &consumerapi.Config{
		ConsumerOptions: queueapi.ConsumerOptions{
			Queue:       registry.NewID("test", "queue"),
			Func:        registry.NewID("test", "func"),
			Concurrency: 5,
			Prefetch:    10,
		},
		Lifecycle: supervisor.LifecycleConfig{
			AutoStart: true,
		},
	}

	entry := registry.Entry{
		ID:   consumerID,
		Kind: "queue.consumer",
		Data: payload.New(oldConfig),
	}

	err := manager.Add(ctx, entry)
	require.NoError(t, err)

	newConfig := &consumerapi.Config{
		ConsumerOptions: queueapi.ConsumerOptions{
			Queue:       registry.NewID("test", "queue"),
			Func:        registry.NewID("test", "func"),
			Concurrency: 10,
			Prefetch:    20,
		},
		Lifecycle: supervisor.LifecycleConfig{
			AutoStart: false,
		},
	}

	updatedEntry := registry.Entry{
		ID:   consumerID,
		Kind: "queue.consumer",
		Data: payload.New(newConfig),
	}

	err = manager.Update(ctx, updatedEntry)
	require.NoError(t, err)

	val, ok := manager.consumers.Load(consumerID)
	assert.True(t, ok)
	consumer, ok := val.(*Consumer)
	assert.True(t, ok)
	assert.Equal(t, 10, consumer.config.Concurrency)
	assert.Equal(t, 20, consumer.config.Prefetch)
}

func TestManager_Delete(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	queueMgr := &mockQueueManager{}
	funcReg := &mockFuncRegistry{}
	dtt := &mockDTT{}

	manager := NewManager(bus, queueMgr, funcReg, dtt, zap.NewNop())

	consumerID := registry.NewID("test", "consumer")

	// Add consumer first
	manager.consumers.Store(consumerID, &Consumer{})

	entry := registry.Entry{
		ID:   consumerID,
		Kind: "queue.consumer",
	}

	err := manager.Delete(ctx, entry)
	require.NoError(t, err)

	// Verify consumer was removed
	_, ok := manager.consumers.Load(consumerID)
	assert.False(t, ok)
}

type mockQueueManager struct {
	queueNotFound  bool
	driverNotFound bool
}

func (m *mockQueueManager) Publish(_ context.Context, _ registry.ID, _ ...*queueapi.Message) error {
	return nil
}

func (m *mockQueueManager) GetDriver(_ registry.ID) (queueapi.Driver, bool) {
	if m.driverNotFound {
		return nil, false
	}
	return &mockDriver{}, true
}

func (m *mockQueueManager) GetQueue(id registry.ID) (*queueapi.Queue, bool) {
	if m.queueNotFound {
		return nil, false
	}
	return &queueapi.Queue{
		ID:       id,
		DriverID: registry.NewID("test", "driver"),
		Name:     "test-queue",
		Config:   &queueapi.Config{},
	}, true
}

func (m *mockQueueManager) RegisterInterceptor(_ string, _ queueapi.PublishInterceptor, _ int) {}

func (m *mockQueueManager) UnregisterInterceptor(_ string) {}

type mockDTT struct{}

func (m *mockDTT) Unmarshal(p payload.Payload, v any) error {
	if config, ok := v.(*consumerapi.Config); ok {
		if src, ok := p.Data().(*consumerapi.Config); ok {
			*config = *src
			return nil
		}
	}
	return nil
}

func (m *mockDTT) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}
