package queue

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	queuecfg "github.com/wippyai/runtime/api/service/queue/queue"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

func TestManager_Add(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	queueMgr := &mockQueueManager{}
	dtt := &mockDTT{}

	manager := NewManager(bus, queueMgr, dtt, zap.NewNop())

	config := &queuecfg.Config{
		Driver: registry.NewID("test", "driver"),
		Options: attrs.Bag{
			queueapi.OptionMaxLength: 1000,
		},
	}

	entry := registry.Entry{
		ID:   registry.NewID("app", "tasks"),
		Kind: queuecfg.Kind,
		Data: payload.New(config),
	}

	err := manager.Add(ctx, entry)
	require.NoError(t, err)

	val, ok := manager.queues.Load(entry.ID)
	assert.True(t, ok)
	assert.NotNil(t, val)
}

func TestManager_Add_DriverNotFound(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	queueMgr := &mockQueueManager{
		driverNotFound: true,
	}
	dtt := &mockDTT{}

	manager := NewManager(bus, queueMgr, dtt, zap.NewNop())

	config := &queuecfg.Config{
		Driver: registry.NewID("test", "driver"),
		Options: attrs.Bag{
			queueapi.OptionMaxLength: 1000,
		},
	}

	entry := registry.Entry{
		ID:   registry.NewID("app", "tasks"),
		Kind: queuecfg.Kind,
		Data: payload.New(config),
	}

	err := manager.Add(ctx, entry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "driver not found")
}

func TestManager_Delete(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	queueMgr := &mockQueueManager{}
	dtt := &mockDTT{}

	manager := NewManager(bus, queueMgr, dtt, zap.NewNop())

	queueID := registry.NewID("app", "tasks")

	queue := &queueapi.Queue{
		ID:       queueID,
		DriverID: registry.NewID("test", "driver"),
		Name:     "tasks",
		Options:  attrs.NewBag(),
	}
	manager.queues.Store(queueID, queue)

	entry := registry.Entry{
		ID:   queueID,
		Kind: queuecfg.Kind,
	}

	err := manager.Delete(ctx, entry)
	require.NoError(t, err)

	_, ok := manager.queues.Load(queueID)
	assert.False(t, ok)
}

func TestManager_Update(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	queueMgr := &mockQueueManager{}
	dtt := &mockDTT{}

	manager := NewManager(bus, queueMgr, dtt, zap.NewNop())

	queueID := registry.NewID("app", "tasks")

	oldQueue := &queueapi.Queue{
		ID:       queueID,
		DriverID: registry.NewID("test", "old-driver"),
		Name:     "tasks",
		Options:  attrs.NewBag(),
	}
	manager.queues.Store(queueID, oldQueue)

	newConfig := &queuecfg.Config{
		Driver: registry.NewID("test", "new-driver"),
		Options: attrs.Bag{
			queueapi.OptionMaxLength: 2000,
		},
	}

	entry := registry.Entry{
		ID:   queueID,
		Kind: queuecfg.Kind,
		Data: payload.New(newConfig),
	}

	err := manager.Update(ctx, entry)
	require.NoError(t, err)

	val, ok := manager.queues.Load(queueID)
	assert.True(t, ok)
	queue, ok := val.(*queueapi.Queue)
	assert.True(t, ok)
	assert.Equal(t, registry.NewID("test", "new-driver"), queue.DriverID)
}

type mockQueueManager struct {
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

func (m *mockQueueManager) GetQueue(_ registry.ID) (*queueapi.Queue, bool) {
	return nil, false
}

type mockDriver struct{}

func (m *mockDriver) Publish(_ context.Context, _ registry.ID, _ ...*queueapi.Message) error {
	return nil
}

func (m *mockDriver) Attach(_ context.Context, _ registry.ID, _ chan<- *queueapi.Delivery) (context.CancelFunc, error) {
	return func() {}, nil
}

func (m *mockDriver) DeclareQueue(_ context.Context, _ registry.ID, _ attrs.Attributes) error {
	return nil
}

func (m *mockDriver) GetQueueInfo(_ context.Context, _ registry.ID) (attrs.Attributes, error) {
	return attrs.NewBag(), nil
}

type mockDTT struct{}

func (m *mockDTT) Unmarshal(p payload.Payload, v interface{}) error {
	if config, ok := v.(*queuecfg.Config); ok {
		if src, ok := p.Data().(*queuecfg.Config); ok {
			*config = *src
			return nil
		}
	}
	return nil
}

func (m *mockDTT) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}
