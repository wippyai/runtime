package queue

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

func setupTest() (*Manager, event.Bus) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	mgr := NewManager(bus, logger)
	return mgr, bus
}

func TestManager_StartStop(t *testing.T) {
	ctx := context.Background()
	mgr, _ := setupTest()

	err := mgr.Start(ctx)
	require.NoError(t, err)
	assert.NotNil(t, mgr.subscriber)

	err = mgr.Stop()
	require.NoError(t, err)
}

func TestManager_DriverRegister(t *testing.T) {
	ctx := context.Background()
	mgr, bus := setupTest()
	require.NoError(t, mgr.Start(ctx))
	defer func() { _ = mgr.Stop() }()

	driverID := registry.ID{NS: "test", Name: "mock-driver"}
	driver := &mockDriver{}

	bus.Send(ctx, event.Event{
		System: queueapi.System,
		Kind:   queueapi.DriverRegister,
		Path:   driverID.String(),
		Data:   driver,
	})

	// Give some time for async event processing
	// In real code this would be synchronous or use proper sync primitives
	// For now, just verify the driver got registered
	assert.Eventually(t, func() bool {
		_, ok := mgr.GetDriver(driverID)
		return ok
	}, 1000000000, 10000000, "driver should be registered")

	retrievedDriver, ok := mgr.GetDriver(driverID)
	assert.True(t, ok)
	assert.Equal(t, driver, retrievedDriver)
}

func TestManager_DriverRegister_InvalidType(t *testing.T) {
	ctx := context.Background()
	mgr, bus := setupTest()
	require.NoError(t, mgr.Start(ctx))
	defer func() { _ = mgr.Stop() }()

	driverID := registry.ID{NS: "test", Name: "bad-driver"}

	bus.Send(ctx, event.Event{
		System: queueapi.System,
		Kind:   queueapi.DriverRegister,
		Path:   driverID.String(),
		Data:   "not a driver",
	})

	// Invalid driver should not be registered
	assert.Never(t, func() bool {
		_, ok := mgr.GetDriver(driverID)
		return ok
	}, 100000000, 10000000, "invalid driver should not be registered")
}

func TestManager_QueueDeclare(t *testing.T) {
	ctx := context.Background()
	mgr, bus := setupTest()
	require.NoError(t, mgr.Start(ctx))
	defer func() { _ = mgr.Stop() }()

	driverID := registry.ID{NS: "test", Name: "mock-driver"}
	driver := &mockDriver{}

	mgr.drivers.Store(driverID, driver)

	queueID := registry.ID{NS: "test", Name: "my-queue"}
	queueEntry := &queueapi.Queue{
		ID:       queueID,
		DriverID: driverID,
		Name:     "my-queue",
		Options:  attrs.NewBag(),
	}

	bus.Send(ctx, event.Event{
		System: queueapi.System,
		Kind:   queueapi.QueueDeclare,
		Path:   queueID.String(),
		Data:   queueEntry,
	})

	assert.Eventually(t, func() bool {
		_, ok := mgr.GetQueue(queueID)
		return ok
	}, 1000000000, 10000000, "queue should be declared")

	assert.True(t, driver.declareQueueCalled, "DeclareQueue should have been called on driver")

	retrievedQueue, ok := mgr.GetQueue(queueID)
	assert.True(t, ok)
	assert.Equal(t, queueEntry, retrievedQueue)
}

func TestManager_QueueDeclare_DriverNotFound(t *testing.T) {
	ctx := context.Background()
	mgr, bus := setupTest()
	require.NoError(t, mgr.Start(ctx))
	defer func() { _ = mgr.Stop() }()

	queueID := registry.ID{NS: "test", Name: "my-queue"}
	driverID := registry.ID{NS: "test", Name: "nonexistent-driver"}
	queueEntry := &queueapi.Queue{
		ID:       queueID,
		DriverID: driverID,
		Name:     "my-queue",
		Options:  attrs.NewBag(),
	}

	bus.Send(ctx, event.Event{
		System: queueapi.System,
		Kind:   queueapi.QueueDeclare,
		Path:   queueID.String(),
		Data:   queueEntry,
	})

	// Queue should not be declared when driver doesn't exist
	assert.Never(t, func() bool {
		_, ok := mgr.GetQueue(queueID)
		return ok
	}, 100000000, 10000000, "queue should not be declared without driver")
}

func TestManager_Publish(t *testing.T) {
	ctx := context.Background()
	mgr, _ := setupTest()
	require.NoError(t, mgr.Start(ctx))
	defer func() { _ = mgr.Stop() }()

	driverID := registry.ID{NS: "test", Name: "mock-driver"}
	driver := &mockDriver{}
	mgr.drivers.Store(driverID, driver)

	queueID := registry.ID{NS: "test", Name: "my-queue"}
	queueEntry := &queueapi.Queue{
		ID:       queueID,
		DriverID: driverID,
		Name:     "my-queue",
		Options:  attrs.NewBag(),
	}
	mgr.queues.Store(queueID, queueEntry)

	msg := queueapi.NewMessage(payload.New("test message"))
	err := mgr.PublishDirect(ctx, queueID, msg)

	require.NoError(t, err)
	assert.True(t, driver.publishCalled, "Publish should have been called on driver")
}

func TestManager_Publish_QueueNotFound(t *testing.T) {
	ctx := context.Background()
	mgr, _ := setupTest()
	require.NoError(t, mgr.Start(ctx))
	defer func() { _ = mgr.Stop() }()

	queueID := registry.ID{NS: "test", Name: "nonexistent"}
	msg := queueapi.NewMessage(payload.New("test"))

	err := mgr.PublishDirect(ctx, queueID, msg)
	assert.ErrorIs(t, err, queueapi.ErrNoQueue)
}

func TestManager_Publish_DriverNotFound(t *testing.T) {
	ctx := context.Background()
	mgr, _ := setupTest()
	require.NoError(t, mgr.Start(ctx))
	defer func() { _ = mgr.Stop() }()

	queueID := registry.ID{NS: "test", Name: "my-queue"}
	driverID := registry.ID{NS: "test", Name: "nonexistent-driver"}
	queueEntry := &queueapi.Queue{
		ID:       queueID,
		DriverID: driverID,
		Name:     "my-queue",
		Options:  attrs.NewBag(),
	}
	mgr.queues.Store(queueID, queueEntry)

	msg := queueapi.NewMessage(payload.New("test"))
	err := mgr.PublishDirect(ctx, queueID, msg)
	assert.ErrorIs(t, err, queueapi.ErrNoDriver)
}

type mockDriver struct {
	publishCalled      bool
	attachCalled       bool
	declareQueueCalled bool
	getQueueInfoCalled bool
	started            bool
	stopped            bool
}

func (m *mockDriver) Publish(_ context.Context, _ registry.ID, _ ...*queueapi.Message) error {
	m.publishCalled = true
	return nil
}

func (m *mockDriver) Attach(_ context.Context, _ registry.ID, _ chan<- *queueapi.Delivery) (context.CancelFunc, error) {
	m.attachCalled = true
	return func() {}, nil
}

func (m *mockDriver) DeclareQueue(_ context.Context, _ registry.ID, _ attrs.Attributes) error {
	m.declareQueueCalled = true
	return nil
}

func (m *mockDriver) GetQueueInfo(_ context.Context, _ registry.ID) (attrs.Attributes, error) {
	m.getQueueInfoCalled = true
	return attrs.NewBag(), nil
}

func (m *mockDriver) Start(_ context.Context) (<-chan any, error) {
	m.started = true
	ch := make(chan any)
	close(ch)
	return ch, nil
}

func (m *mockDriver) Stop(_ context.Context) error {
	m.stopped = true
	return nil
}
