package queue_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	queuesvc "github.com/wippyai/runtime/service/queue"
)

// MockDriver is a mock implementation of the Driver interface
type MockDriver struct {
	mock.Mock
}

func (m *MockDriver) Publish(ctx context.Context, q registry.ID, msgs ...*queue.Message) error {
	args := m.Called(ctx, q, msgs)
	return args.Error(0)
}

func (m *MockDriver) Attach(ctx context.Context, q registry.ID, deliveries chan<- *queue.Delivery) (context.CancelFunc, error) {
	args := m.Called(ctx, q, deliveries)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(context.CancelFunc), args.Error(1)
}

func (m *MockDriver) DeclareQueue(ctx context.Context, q registry.ID, opts attrs.Attributes) error {
	args := m.Called(ctx, q, opts)
	return args.Error(0)
}

func (m *MockDriver) GetQueueInfo(ctx context.Context, q registry.ID) (attrs.Attributes, error) {
	args := m.Called(ctx, q)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(attrs.Attributes), args.Error(1)
}

// MockManager is a mock implementation of the Manager interface
type MockManager struct {
	mock.Mock
}

func (m *MockManager) Add(ctx context.Context, entry registry.Entry) error {
	args := m.Called(ctx, entry)
	return args.Error(0)
}

func (m *MockManager) Update(ctx context.Context, entry registry.Entry) error {
	args := m.Called(ctx, entry)
	return args.Error(0)
}

func (m *MockManager) Delete(ctx context.Context, entry registry.Entry) error {
	args := m.Called(ctx, entry)
	return args.Error(0)
}

func (m *MockManager) Publish(ctx context.Context, q registry.ID, msgs ...*queue.Message) error {
	args := m.Called(ctx, q, msgs)
	return args.Error(0)
}

func (m *MockManager) GetDriver(id registry.ID) (queue.Driver, bool) {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil, args.Bool(1)
	}
	return args.Get(0).(queue.Driver), args.Bool(1)
}

func (m *MockManager) GetQueue(id registry.ID) (*queue.Queue, bool) {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil, args.Bool(1)
	}
	return args.Get(0).(*queue.Queue), args.Bool(1)
}

func (m *MockManager) RegisterInterceptor(name string, interceptor queue.PublishInterceptor, priority int) {
	m.Called(name, interceptor, priority)
}

func (m *MockManager) UnregisterInterceptor(name string) {
	m.Called(name)
}

func TestQueueTypes(t *testing.T) {
	t.Run("Queue struct", func(t *testing.T) {
		queueID := registry.NewID("test", "my-queue")
		driverID := registry.NewID("test", "redis-driver")
		opts := attrs.NewBag()
		opts.Set(queue.OptionQueueName, "custom-queue-name")
		opts.Set(queue.OptionDurable, true)
		opts.Set(queue.OptionMaxLength, 1000)

		q := &queue.Queue{
			ID:       queueID,
			DriverID: driverID,
			Name:     "custom-queue-name",
			Options:  opts,
		}

		assert.Equal(t, queueID, q.ID)
		assert.Equal(t, driverID, q.DriverID)
		assert.Equal(t, "custom-queue-name", q.Name)
		assert.Equal(t, true, q.Options.GetBool(queue.OptionDurable, false))
		assert.Equal(t, 1000, q.Options.GetInt(queue.OptionMaxLength, 0))
	})

	t.Run("Delivery struct", func(t *testing.T) {
		msg := queue.NewMessage(payload.New("test"))
		ackCalled := false
		nackCalled := false

		delivery := &queue.Delivery{
			Message: msg,
			Ack: func(_ context.Context) error {
				ackCalled = true
				return nil
			},
			Nack: func(_ context.Context) error {
				nackCalled = true
				return nil
			},
		}

		assert.Equal(t, msg, delivery.Message)

		// Test Ack
		err := delivery.Ack(context.Background())
		assert.NoError(t, err)
		assert.True(t, ackCalled)
		assert.False(t, nackCalled)

		// Reset and test Nack
		ackCalled = false
		err = delivery.Nack(context.Background())
		assert.NoError(t, err)
		assert.False(t, ackCalled)
		assert.True(t, nackCalled)
	})
}

func TestEventConstants(t *testing.T) {
	assert.Equal(t, "queue", queue.System)

	assert.Equal(t, "queue.driver.register", queue.DriverRegister)
	assert.Equal(t, "queue.driver.delete", queue.DriverDelete)
	assert.Equal(t, "queue.queue.declare", queue.Declare)
	assert.Equal(t, "queue.queue.delete", queue.Delete)
}

func TestErrors(t *testing.T) {
	assert.EqualError(t, queue.ErrDriverNotFound, "queue driver not found")
	assert.EqualError(t, queue.ErrQueueNotFound, "queue not found")
	assert.EqualError(t, queuesvc.ErrDriverNotStarted, "queue driver not started")
	assert.EqualError(t, queuesvc.ErrQueueFull, "queue is full")
	assert.EqualError(t, queue.ErrMessageExpired, "message expired")
	assert.EqualError(t, queuesvc.ErrConsumerClosed, "consumer closed")
	assert.EqualError(t, queuesvc.ErrQueueClosed, "queue is closed")
	assert.EqualError(t, queuesvc.ErrNoPublishFunc, "no publish function configured")
	assert.EqualError(t, queue.ErrDriverIDRequired, "driver ID is required")
	assert.EqualError(t, queue.ErrQueueIDRequired, "queue ID is required")
	assert.EqualError(t, queue.ErrFunctionIDRequired, "function ID is required")
}

func TestErrorInterface(t *testing.T) {
	t.Run("ErrDriverNotFound", func(t *testing.T) {
		err := queue.ErrDriverNotFound
		assert.Equal(t, "queue driver not found", err.Error())
		assert.Equal(t, apierror.NotFound, err.Kind())
		assert.Equal(t, apierror.False, err.Retryable())
		assert.Nil(t, err.Details())
	})

	t.Run("ErrQueueNotFound", func(t *testing.T) {
		err := queue.ErrQueueNotFound
		assert.Equal(t, apierror.NotFound, err.Kind())
		assert.Equal(t, apierror.False, err.Retryable())
	})

	t.Run("ErrDriverNotStarted", func(t *testing.T) {
		err := queuesvc.ErrDriverNotStarted
		assert.Equal(t, apierror.Unavailable, err.Kind())
		assert.Equal(t, apierror.True, err.Retryable())
	})

	t.Run("ErrQueueFull", func(t *testing.T) {
		err := queuesvc.ErrQueueFull
		assert.Equal(t, apierror.Unavailable, err.Kind())
		assert.Equal(t, apierror.True, err.Retryable())
	})

	t.Run("ErrQueueClosed", func(t *testing.T) {
		err := queuesvc.ErrQueueClosed
		assert.Equal(t, apierror.Unavailable, err.Kind())
		assert.Equal(t, apierror.False, err.Retryable())
	})

	t.Run("ErrMessageExpired", func(t *testing.T) {
		err := queue.ErrMessageExpired
		assert.Equal(t, apierror.Invalid, err.Kind())
		assert.Equal(t, apierror.False, err.Retryable())
	})

	t.Run("ErrConsumerClosed", func(t *testing.T) {
		err := queuesvc.ErrConsumerClosed
		assert.Equal(t, apierror.Unavailable, err.Kind())
		assert.Equal(t, apierror.False, err.Retryable())
	})

	t.Run("ErrNoPublishFunc", func(t *testing.T) {
		err := queuesvc.ErrNoPublishFunc
		assert.Equal(t, apierror.Unavailable, err.Kind())
		assert.Equal(t, apierror.False, err.Retryable())
	})

	t.Run("ErrDriverIDRequired", func(t *testing.T) {
		err := queue.ErrDriverIDRequired
		assert.Equal(t, apierror.Invalid, err.Kind())
		assert.Equal(t, apierror.False, err.Retryable())
	})

	t.Run("ErrQueueIDRequired", func(t *testing.T) {
		err := queue.ErrQueueIDRequired
		assert.Equal(t, apierror.Invalid, err.Kind())
		assert.Equal(t, apierror.False, err.Retryable())
	})

	t.Run("ErrFunctionIDRequired", func(t *testing.T) {
		err := queue.ErrFunctionIDRequired
		assert.Equal(t, apierror.Invalid, err.Kind())
		assert.Equal(t, apierror.False, err.Retryable())
	})
}

func TestErrorMethods(t *testing.T) {
	t.Run("SetCause", func(t *testing.T) {
		causeErr := assert.AnError
		err := apierror.SetCause(queue.ErrDriverNotFound, causeErr)
		assert.Equal(t, "queue driver not found", err.Error())
		assert.True(t, errors.Is(err, causeErr))
	})

	t.Run("SetMessage", func(t *testing.T) {
		err := apierror.SetMessage(queue.ErrDriverNotFound, "custom driver error")
		assert.Equal(t, "custom driver error", err.Error())
		assert.Equal(t, apierror.NotFound, err.Kind())
	})

	t.Run("SetDetails", func(t *testing.T) {
		details := attrs.NewBag()
		details.Set("key", "value")
		err := apierror.SetDetails(queue.ErrDriverNotFound, details)
		assert.Equal(t, "queue driver not found", err.Error())
		assert.Equal(t, details, err.Details())
	})
}

func TestDriverInterface(t *testing.T) {
	driver := new(MockDriver)
	ctx := context.Background()
	queueID := registry.NewID("test", "my-queue")
	opts := attrs.NewBag()
	msgs := []*queue.Message{
		queue.NewMessage(payload.New("msg1")),
		queue.NewMessage(payload.New("msg2")),
	}

	t.Run("Publish", func(t *testing.T) {
		driver.On("Publish", ctx, queueID, msgs).Return(nil).Once()
		err := driver.Publish(ctx, queueID, msgs...)
		assert.NoError(t, err)
		driver.AssertExpectations(t)
	})

	t.Run("DeclareQueue", func(t *testing.T) {
		driver.On("DeclareQueue", ctx, queueID, opts).Return(nil).Once()
		err := driver.DeclareQueue(ctx, queueID, opts)
		assert.NoError(t, err)
		driver.AssertExpectations(t)
	})

	t.Run("Attach", func(t *testing.T) {
		deliveries := make(chan *queue.Delivery)
		cancel := context.CancelFunc(func() {})
		driver.On("Attach", ctx, queueID, mock.AnythingOfType("chan<- *queue.Delivery")).Return(cancel, nil).Once()

		cancelFunc, err := driver.Attach(ctx, queueID, deliveries)
		assert.NoError(t, err)
		assert.NotNil(t, cancelFunc)
		driver.AssertExpectations(t)
	})

	t.Run("GetQueueInfo", func(t *testing.T) {
		info := attrs.NewBag()
		info.Set(queue.StatsMessageCount, 100)
		info.Set(queue.StatsConsumerCount, 3)

		driver.On("GetQueueInfo", ctx, queueID).Return(info, nil).Once()

		result, err := driver.GetQueueInfo(ctx, queueID)
		assert.NoError(t, err)
		assert.Equal(t, 100, result.GetInt(queue.StatsMessageCount, 0))
		assert.Equal(t, 3, result.GetInt(queue.StatsConsumerCount, 0))
		driver.AssertExpectations(t)
	})
}

func TestManagerInterface(t *testing.T) {
	manager := new(MockManager)
	ctx := context.Background()
	queueID := registry.NewID("test", "my-queue")
	driverID := registry.NewID("test", "redis-driver")
	opts := attrs.NewBag()
	msgs := []*queue.Message{
		queue.NewMessage(payload.New("msg1")),
		queue.NewMessage(payload.New("msg2")),
	}

	t.Run("Publish", func(t *testing.T) {
		manager.On("Publish", ctx, queueID, msgs).Return(nil).Once()
		err := manager.Publish(ctx, queueID, msgs...)
		assert.NoError(t, err)
		manager.AssertExpectations(t)
	})

	t.Run("GetDriver", func(t *testing.T) {
		driver := new(MockDriver)
		manager.On("GetDriver", driverID).Return(driver, true).Once()

		result, ok := manager.GetDriver(driverID)
		assert.True(t, ok)
		assert.Equal(t, driver, result)
		manager.AssertExpectations(t)
	})

	t.Run("GetQueue", func(t *testing.T) {
		q := &queue.Queue{
			ID:       queueID,
			DriverID: driverID,
			Name:     "my-queue",
			Options:  opts,
		}
		manager.On("GetQueue", queueID).Return(q, true).Once()

		result, ok := manager.GetQueue(queueID)
		assert.True(t, ok)
		assert.Equal(t, q, result)
		manager.AssertExpectations(t)
	})
}

func TestInterceptorInterface(t *testing.T) {
	ctx := context.Background()
	queueID := registry.NewID("test", "my-queue")
	msgs := []*queue.Message{
		queue.NewMessage(payload.New("msg1")),
	}

	t.Run("PublishInterceptor", func(t *testing.T) {
		interceptorCalled := false
		interceptor := &testPublishInterceptor{
			handleFunc: func(ctx context.Context, q registry.ID, msgs []*queue.Message,
				next func(context.Context, registry.ID, []*queue.Message) error) error {
				interceptorCalled = true
				assert.Equal(t, queueID, q)
				assert.Len(t, msgs, 1)
				return next(ctx, q, msgs)
			},
		}

		// Test the interceptor chain
		err := interceptor.Handle(ctx, queueID, msgs, func(_ context.Context, q registry.ID, msgs []*queue.Message) error {
			assert.Equal(t, queueID, q)
			assert.Len(t, msgs, 1)
			return nil
		})

		assert.NoError(t, err)
		assert.True(t, interceptorCalled)
	})
}

// testPublishInterceptor is a test implementation of PublishInterceptor
type testPublishInterceptor struct {
	handleFunc func(context.Context, registry.ID, []*queue.Message, func(context.Context, registry.ID, []*queue.Message) error) error
}

func (i *testPublishInterceptor) Handle(ctx context.Context, q registry.ID, msgs []*queue.Message,
	next func(context.Context, registry.ID, []*queue.Message) error) error {
	return i.handleFunc(ctx, q, msgs, next)
}

func TestQueueDeclarationFlow(t *testing.T) {
	// Simulate a typical queue declaration flow
	ctx := context.Background()
	manager := new(MockManager)

	// Step 1: Register a driver (kind would be defined in service layer, e.g., "queue.redis")
	driverID := registry.NewID("app", "redis-driver")
	driverEntry := registry.Entry{
		ID:   driverID,
		Kind: "queue.redis", // This kind would be defined in service layer
		Data: payload.New(map[string]any{
			"dsn": "redis://localhost:6379",
		}),
	}

	manager.On("Add", ctx, driverEntry).Return(nil).Once()
	err := manager.Add(ctx, driverEntry)
	require.NoError(t, err)

	// Step 2: Declare a queue
	queueID := registry.NewID("app", "my-queue")
	queueOpts := attrs.NewBag()
	queueOpts.Set(queue.OptionQueueName, "custom-name")
	queueOpts.Set(queue.OptionDurable, true)
	queueOpts.Set(queue.OptionMaxLength, 10000)

	queueEntry := registry.Entry{
		ID:   queueID,
		Kind: "queue.queue", // This kind would be defined in service layer
		Data: payload.New(map[string]any{
			"driver":  driverID.String(),
			"options": queueOpts,
		}),
	}

	manager.On("Add", ctx, queueEntry).Return(nil).Once()
	err = manager.Add(ctx, queueEntry)
	require.NoError(t, err)

	// Step 3: Get the queue to verify it was created
	expectedQueue := &queue.Queue{
		ID:       queueID,
		DriverID: driverID,
		Name:     "custom-name",
		Options:  queueOpts,
	}

	manager.On("GetQueue", queueID).Return(expectedQueue, true).Once()
	q, ok := manager.GetQueue(queueID)
	assert.True(t, ok)
	assert.Equal(t, expectedQueue, q)

	manager.AssertExpectations(t)
}

func TestContextFunctions(t *testing.T) {
	t.Run("WithManager_NoAppContext", func(t *testing.T) {
		ctx := context.Background()
		result := queue.WithManager(ctx, nil)
		assert.Equal(t, ctx, result)
	})

	t.Run("GetManager_NoAppContext", func(t *testing.T) {
		ctx := context.Background()
		mgr := queue.GetManager(ctx)
		assert.Nil(t, mgr)
	})

	t.Run("WithDelivery_NoFrameContext", func(t *testing.T) {
		ctx := context.Background()
		delivery := &queue.Delivery{Message: queue.NewMessage(payload.New("test"))}
		err := queue.WithDelivery(ctx, delivery)
		assert.Error(t, err)
	})

	t.Run("GetDelivery_NoFrameContext", func(t *testing.T) {
		ctx := context.Background()
		delivery, ok := queue.GetDelivery(ctx)
		assert.Nil(t, delivery)
		assert.False(t, ok)
	})

	t.Run("DeliveryPair", func(t *testing.T) {
		delivery := &queue.Delivery{Message: queue.NewMessage(payload.New("test"))}
		pair := queue.DeliveryPair(delivery)
		assert.NotNil(t, pair.Key)
		assert.Equal(t, delivery, pair.Value)
	})

	t.Run("Manager_WithAppContext", func(t *testing.T) {
		appCtx := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), appCtx)

		mockMgr := new(MockManager)
		result := queue.WithManager(ctx, mockMgr)
		assert.Equal(t, ctx, result)

		retrieved := queue.GetManager(ctx)
		assert.Equal(t, mockMgr, retrieved)

		// Test idempotent - second set doesn't override
		mockMgr2 := new(MockManager)
		queue.WithManager(ctx, mockMgr2)
		assert.Equal(t, mockMgr, queue.GetManager(ctx))
	})

	t.Run("GetManager_WrongType", func(t *testing.T) {
		appCtx := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), appCtx)

		appCtx.With(&ctxapi.Key{Name: "queue.manager"}, "not a manager")

		mgr := queue.GetManager(ctx)
		assert.Nil(t, mgr)
	})

	t.Run("Delivery_WithFrameContext", func(t *testing.T) {
		appCtx := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), appCtx)

		ctx, frameCtx := ctxapi.OpenFrameContext(ctx)
		defer ctxapi.ReleaseFrameContext(frameCtx)

		delivery := &queue.Delivery{Message: queue.NewMessage(payload.New("test"))}
		err := queue.WithDelivery(ctx, delivery)
		assert.NoError(t, err)

		retrieved, ok := queue.GetDelivery(ctx)
		assert.True(t, ok)
		assert.Equal(t, delivery, retrieved)
	})

	t.Run("GetDelivery_WrongType", func(t *testing.T) {
		appCtx := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), appCtx)

		ctx, frameCtx := ctxapi.OpenFrameContext(ctx)
		defer ctxapi.ReleaseFrameContext(frameCtx)

		_ = frameCtx.Set(&ctxapi.Key{Name: "queue.delivery", Inherit: true}, "not a delivery")

		delivery, ok := queue.GetDelivery(ctx)
		assert.Nil(t, delivery)
		assert.False(t, ok)
	})
}
