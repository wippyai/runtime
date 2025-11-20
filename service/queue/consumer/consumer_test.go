package consumer

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	consumerapi "github.com/wippyai/runtime/api/service/queue/consumer"
	"go.uber.org/zap"
)

func TestConsumer_StartStop(t *testing.T) {
	ctx := context.Background()
	driver := &mockDriver{}
	funcReg := &mockFuncRegistry{}

	config := &consumerapi.Config{
		Queue:       registry.ID{NS: "test", Name: "queue"},
		Func:        registry.ID{NS: "test", Name: "func"},
		Concurrency: 2,
		Prefetch:    10,
	}

	consumer := NewConsumer(
		registry.ID{NS: "test", Name: "consumer"},
		config,
		driver,
		funcReg,
		zap.NewNop(),
	)

	statusChan, err := consumer.Start(ctx)
	require.NoError(t, err)
	assert.NotNil(t, statusChan)
	assert.True(t, driver.attachCalled.Load())

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = consumer.Stop(stopCtx)
	require.NoError(t, err)
	assert.True(t, driver.cancelCalled.Load())
}

func TestConsumer_ProcessMessage(t *testing.T) {
	ctx := context.Background()
	driver := &mockDriver{}
	funcReg := &mockFuncRegistry{}

	config := &consumerapi.Config{
		Queue:       registry.ID{NS: "test", Name: "queue"},
		Func:        registry.ID{NS: "test", Name: "func"},
		Concurrency: 1,
		Prefetch:    10,
	}

	consumer := NewConsumer(
		registry.ID{NS: "test", Name: "consumer"},
		config,
		driver,
		funcReg,
		zap.NewNop(),
	)

	_, err := consumer.Start(ctx)
	require.NoError(t, err)

	// Send a message
	acked := atomic.Bool{}
	nacked := atomic.Bool{}

	delivery := &queueapi.Delivery{
		Message: &queueapi.Message{
			ID:      "msg1",
			Body:    payload.New("test message"),
			Headers: attrs.NewBag(),
		},
		Ack: func(_ context.Context) error {
			acked.Store(true)
			return nil
		},
		Nack: func(_ context.Context) error {
			nacked.Store(true)
			return nil
		},
	}

	driver.deliveries <- delivery

	// Wait for processing
	assert.Eventually(t, acked.Load, 2*time.Second, 10*time.Millisecond, "message should be acked")

	assert.False(t, nacked.Load(), "message should not be nacked")
	assert.True(t, funcReg.callCalled.Load(), "function should be called")

	// Stop consumer
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = consumer.Stop(stopCtx)
	require.NoError(t, err)
}

func TestConsumer_ProcessMessage_Error(t *testing.T) {
	ctx := context.Background()
	driver := &mockDriver{}
	funcReg := &mockFuncRegistry{
		callErr: errors.New("function failed"),
	}

	config := &consumerapi.Config{
		Queue:       registry.ID{NS: "test", Name: "queue"},
		Func:        registry.ID{NS: "test", Name: "func"},
		Concurrency: 1,
		Prefetch:    10,
	}

	consumer := NewConsumer(
		registry.ID{NS: "test", Name: "consumer"},
		config,
		driver,
		funcReg,
		zap.NewNop(),
	)

	_, err := consumer.Start(ctx)
	require.NoError(t, err)

	// Send a message
	acked := atomic.Bool{}
	nacked := atomic.Bool{}

	delivery := &queueapi.Delivery{
		Message: &queueapi.Message{
			ID:      "msg1",
			Body:    payload.New("test message"),
			Headers: attrs.NewBag(),
		},
		Ack: func(_ context.Context) error {
			acked.Store(true)
			return nil
		},
		Nack: func(_ context.Context) error {
			nacked.Store(true)
			return nil
		},
	}

	driver.deliveries <- delivery

	// Wait for processing
	assert.Eventually(t, nacked.Load, 2*time.Second, 10*time.Millisecond, "message should be nacked")

	assert.False(t, acked.Load(), "message should not be acked")
	assert.True(t, funcReg.callCalled.Load(), "function should be called")

	// Stop consumer
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = consumer.Stop(stopCtx)
	require.NoError(t, err)
}

func TestConsumer_StopTimeout(t *testing.T) {
	ctx := context.Background()
	driver := &mockDriver{}

	blockProcessing := make(chan struct{})
	startedProcessing := make(chan struct{})

	funcReg := &mockFuncRegistry{
		onCall: func() {
			close(startedProcessing)
			<-blockProcessing
		},
	}

	config := &consumerapi.Config{
		Queue:       registry.ID{NS: "test", Name: "queue"},
		Func:        registry.ID{NS: "test", Name: "func"},
		Concurrency: 1,
		Prefetch:    10,
	}

	consumer := NewConsumer(
		registry.ID{NS: "test", Name: "consumer"},
		config,
		driver,
		funcReg,
		zap.NewNop(),
	)

	statusChan, err := consumer.Start(ctx)
	require.NoError(t, err)
	assert.NotNil(t, statusChan)

	delivery := &queueapi.Delivery{
		Message: &queueapi.Message{
			ID:      "msg1",
			Body:    payload.New("test message"),
			Headers: attrs.NewBag(),
		},
		Ack:  func(_ context.Context) error { return nil },
		Nack: func(_ context.Context) error { return nil },
	}

	driver.deliveries <- delivery

	<-startedProcessing

	stopCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	go func() {
		time.Sleep(200 * time.Millisecond)
		close(blockProcessing)
	}()

	err = consumer.Stop(stopCtx)
	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)

	select {
	case _, ok := <-statusChan:
		assert.False(t, ok, "statusChan should be closed")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("statusChan should be closed immediately on timeout")
	}
}

func TestConsumer_StopWithNoMessages(t *testing.T) {
	ctx := context.Background()
	driver := &mockDriver{}
	funcReg := &mockFuncRegistry{}

	config := &consumerapi.Config{
		Queue:       registry.ID{NS: "test", Name: "queue"},
		Func:        registry.ID{NS: "test", Name: "func"},
		Concurrency: 3,
		Prefetch:    10,
	}

	consumer := NewConsumer(
		registry.ID{NS: "test", Name: "consumer"},
		config,
		driver,
		funcReg,
		zap.NewNop(),
	)

	statusChan, err := consumer.Start(ctx)
	require.NoError(t, err)
	assert.NotNil(t, statusChan)

	stopCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err = consumer.Stop(stopCtx)
	assert.NoError(t, err)

	select {
	case _, ok := <-statusChan:
		assert.False(t, ok, "statusChan should be closed")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("statusChan should be closed immediately")
	}
}

func TestConsumer_MultipleStopCalls(t *testing.T) {
	ctx := context.Background()
	driver := &mockDriver{}
	funcReg := &mockFuncRegistry{}

	config := &consumerapi.Config{
		Queue:       registry.ID{NS: "test", Name: "queue"},
		Func:        registry.ID{NS: "test", Name: "func"},
		Concurrency: 2,
		Prefetch:    10,
	}

	consumer := NewConsumer(
		registry.ID{NS: "test", Name: "consumer"},
		config,
		driver,
		funcReg,
		zap.NewNop(),
	)

	_, err := consumer.Start(ctx)
	require.NoError(t, err)

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = consumer.Stop(stopCtx)
	require.NoError(t, err)

	err2 := consumer.Stop(stopCtx)
	assert.NoError(t, err2, "second Stop should be idempotent and return nil")
}

func TestConsumer_ConcurrentMessageProcessing(t *testing.T) {
	ctx := context.Background()
	driver := &mockDriver{}

	processedCount := atomic.Int32{}
	funcReg := &mockFuncRegistry{
		callDelay: 50 * time.Millisecond,
		onCall: func() {
			processedCount.Add(1)
		},
	}

	config := &consumerapi.Config{
		Queue:       registry.ID{NS: "test", Name: "queue"},
		Func:        registry.ID{NS: "test", Name: "func"},
		Concurrency: 3,
		Prefetch:    10,
	}

	consumer := NewConsumer(
		registry.ID{NS: "test", Name: "consumer"},
		config,
		driver,
		funcReg,
		zap.NewNop(),
	)

	_, err := consumer.Start(ctx)
	require.NoError(t, err)

	messageCount := 9
	for i := 0; i < messageCount; i++ {
		delivery := &queueapi.Delivery{
			Message: &queueapi.Message{
				ID:      fmt.Sprintf("msg%d", i),
				Body:    payload.New("test message"),
				Headers: attrs.NewBag(),
			},
			Ack:  func(_ context.Context) error { return nil },
			Nack: func(_ context.Context) error { return nil },
		}
		driver.deliveries <- delivery
	}

	assert.Eventually(t, func() bool {
		return processedCount.Load() == int32(messageCount)
	}, 3*time.Second, 10*time.Millisecond, "all messages should be processed")

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = consumer.Stop(stopCtx)
	require.NoError(t, err)
}

func TestConsumer_StopDuringProcessing(t *testing.T) {
	ctx := context.Background()
	driver := &mockDriver{}

	startedProcessing := make(chan struct{})
	blockProcessing := make(chan struct{})

	funcReg := &mockFuncRegistry{
		onCall: func() {
			close(startedProcessing)
			<-blockProcessing
		},
	}

	config := &consumerapi.Config{
		Queue:       registry.ID{NS: "test", Name: "queue"},
		Func:        registry.ID{NS: "test", Name: "func"},
		Concurrency: 1,
		Prefetch:    10,
	}

	consumer := NewConsumer(
		registry.ID{NS: "test", Name: "consumer"},
		config,
		driver,
		funcReg,
		zap.NewNop(),
	)

	_, err := consumer.Start(ctx)
	require.NoError(t, err)

	delivery := &queueapi.Delivery{
		Message: &queueapi.Message{
			ID:      "msg1",
			Body:    payload.New("test message"),
			Headers: attrs.NewBag(),
		},
		Ack:  func(_ context.Context) error { return nil },
		Nack: func(_ context.Context) error { return nil },
	}
	driver.deliveries <- delivery

	<-startedProcessing

	stopCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	stopDone := make(chan error, 1)
	go func() {
		stopDone <- consumer.Stop(stopCtx)
	}()

	time.Sleep(50 * time.Millisecond)
	close(blockProcessing)

	err = <-stopDone
	assert.NoError(t, err, "should stop gracefully after processing completes")
}

func TestConsumer_ContextCancellationStopsWorkers(t *testing.T) {
	ctx := context.Background()
	driver := &mockDriver{}
	funcReg := &mockFuncRegistry{}

	config := &consumerapi.Config{
		Queue:       registry.ID{NS: "test", Name: "queue"},
		Func:        registry.ID{NS: "test", Name: "func"},
		Concurrency: 3,
		Prefetch:    10,
	}

	consumer := NewConsumer(
		registry.ID{NS: "test", Name: "consumer"},
		config,
		driver,
		funcReg,
		zap.NewNop(),
	)

	statusChan, err := consumer.Start(ctx)
	require.NoError(t, err)
	assert.NotNil(t, statusChan)

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	startStop := time.Now()
	err = consumer.Stop(stopCtx)
	stopDuration := time.Since(startStop)

	assert.NoError(t, err)
	assert.Less(t, stopDuration, 500*time.Millisecond, "stop should be fast with no messages")
}

func TestConsumer_AckNackAfterShutdown(t *testing.T) {
	ctx := context.Background()
	driver := &mockDriver{}

	funcReg := &mockFuncRegistry{
		onCall: func() {},
	}

	config := &consumerapi.Config{
		Queue:       registry.ID{NS: "test", Name: "queue"},
		Func:        registry.ID{NS: "test", Name: "func"},
		Concurrency: 1,
		Prefetch:    10,
	}

	consumer := NewConsumer(
		registry.ID{NS: "test", Name: "consumer"},
		config,
		driver,
		funcReg,
		zap.NewNop(),
	)

	_, err := consumer.Start(ctx)
	require.NoError(t, err)

	ackCalled := false
	var savedNack func(context.Context) error

	delivery := &queueapi.Delivery{
		Message: &queueapi.Message{
			ID:      "msg1",
			Body:    payload.New("test message"),
			Headers: attrs.NewBag(),
		},
		Ack: func(_ context.Context) error {
			ackCalled = true
			return nil
		},
		Nack: func(_ context.Context) error {
			return fmt.Errorf("queue is closed")
		},
	}

	savedNack = delivery.Nack

	driver.deliveries <- delivery

	time.Sleep(50 * time.Millisecond)

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = consumer.Stop(stopCtx)
	require.NoError(t, err)

	assert.True(t, ackCalled, "message should have been acked during processing")

	nackErr := savedNack(context.Background())
	assert.Error(t, nackErr, "nack after shutdown should return error (queue closed)")
}

func TestConsumer_SlowWorkers(t *testing.T) {
	ctx := context.Background()
	driver := &mockDriver{}

	processedCount := atomic.Int32{}
	funcReg := &mockFuncRegistry{
		callDelay: 200 * time.Millisecond,
		onCall: func() {
			processedCount.Add(1)
		},
	}

	config := &consumerapi.Config{
		Queue:       registry.ID{NS: "test", Name: "queue"},
		Func:        registry.ID{NS: "test", Name: "func"},
		Concurrency: 2,
		Prefetch:    10,
	}

	consumer := NewConsumer(
		registry.ID{NS: "test", Name: "consumer"},
		config,
		driver,
		funcReg,
		zap.NewNop(),
	)

	_, err := consumer.Start(ctx)
	require.NoError(t, err)

	for i := 0; i < 4; i++ {
		delivery := &queueapi.Delivery{
			Message: &queueapi.Message{
				ID:      fmt.Sprintf("msg%d", i),
				Body:    payload.New("test message"),
				Headers: attrs.NewBag(),
			},
			Ack:  func(_ context.Context) error { return nil },
			Nack: func(_ context.Context) error { return nil },
		}
		driver.deliveries <- delivery
	}

	time.Sleep(100 * time.Millisecond)

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	startStop := time.Now()
	err = consumer.Stop(stopCtx)
	stopDuration := time.Since(startStop)

	require.NoError(t, err)
	assert.GreaterOrEqual(t, processedCount.Load(), int32(2), "at least 2 messages should be processed before stop")
	assert.Less(t, stopDuration, 1500*time.Millisecond, "should stop relatively quickly even with slow workers")
}

func TestConsumer_DeadWorkerTimeout(t *testing.T) {
	ctx := context.Background()
	driver := &mockDriver{}

	startedProcessing := make(chan struct{})
	blockForever := make(chan struct{})

	funcReg := &mockFuncRegistry{
		onCall: func() {
			select {
			case startedProcessing <- struct{}{}:
			default:
			}
			<-blockForever
		},
	}

	config := &consumerapi.Config{
		Queue:       registry.ID{NS: "test", Name: "queue"},
		Func:        registry.ID{NS: "test", Name: "func"},
		Concurrency: 1,
		Prefetch:    10,
	}

	consumer := NewConsumer(
		registry.ID{NS: "test", Name: "consumer"},
		config,
		driver,
		funcReg,
		zap.NewNop(),
	)

	_, err := consumer.Start(ctx)
	require.NoError(t, err)

	delivery := &queueapi.Delivery{
		Message: &queueapi.Message{
			ID:      "msg1",
			Body:    payload.New("test message"),
			Headers: attrs.NewBag(),
		},
		Ack:  func(_ context.Context) error { return nil },
		Nack: func(_ context.Context) error { return nil },
	}

	driver.deliveries <- delivery

	<-startedProcessing

	stopCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err = consumer.Stop(stopCtx)
	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err, "should timeout when worker is blocked")
}

func TestConsumer_MultipleWorkersOneBlocked(t *testing.T) {
	ctx := context.Background()
	driver := &mockDriver{}

	firstWorkerStarted := make(chan struct{})
	blockFirstWorker := make(chan struct{})
	otherWorkersProcessed := atomic.Int32{}

	callCount := atomic.Int32{}
	funcReg := &mockFuncRegistry{
		onCall: func() {
			count := callCount.Add(1)
			if count == 1 {
				close(firstWorkerStarted)
				<-blockFirstWorker
			} else {
				otherWorkersProcessed.Add(1)
			}
		},
	}

	config := &consumerapi.Config{
		Queue:       registry.ID{NS: "test", Name: "queue"},
		Func:        registry.ID{NS: "test", Name: "func"},
		Concurrency: 3,
		Prefetch:    10,
	}

	consumer := NewConsumer(
		registry.ID{NS: "test", Name: "consumer"},
		config,
		driver,
		funcReg,
		zap.NewNop(),
	)

	_, err := consumer.Start(ctx)
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		delivery := &queueapi.Delivery{
			Message: &queueapi.Message{
				ID:      fmt.Sprintf("msg%d", i),
				Body:    payload.New("test message"),
				Headers: attrs.NewBag(),
			},
			Ack:  func(_ context.Context) error { return nil },
			Nack: func(_ context.Context) error { return nil },
		}
		driver.deliveries <- delivery
	}

	<-firstWorkerStarted

	assert.Eventually(t, func() bool {
		return otherWorkersProcessed.Load() >= 2
	}, 1*time.Second, 10*time.Millisecond, "other workers should continue processing while one is blocked")

	close(blockFirstWorker)

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = consumer.Stop(stopCtx)
	require.NoError(t, err)
}

func TestConsumer_StopWithAllWorkersBlocked(t *testing.T) {
	ctx := context.Background()
	driver := &mockDriver{}

	allWorkersStarted := make(chan struct{})
	blockAllWorkers := make(chan struct{})
	workersStarted := atomic.Int32{}

	funcReg := &mockFuncRegistry{
		onCall: func() {
			count := workersStarted.Add(1)
			if count == 3 {
				close(allWorkersStarted)
			}
			<-blockAllWorkers
		},
	}

	config := &consumerapi.Config{
		Queue:       registry.ID{NS: "test", Name: "queue"},
		Func:        registry.ID{NS: "test", Name: "func"},
		Concurrency: 3,
		Prefetch:    10,
	}

	consumer := NewConsumer(
		registry.ID{NS: "test", Name: "consumer"},
		config,
		driver,
		funcReg,
		zap.NewNop(),
	)

	_, err := consumer.Start(ctx)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		delivery := &queueapi.Delivery{
			Message: &queueapi.Message{
				ID:      fmt.Sprintf("msg%d", i),
				Body:    payload.New("test message"),
				Headers: attrs.NewBag(),
			},
			Ack:  func(_ context.Context) error { return nil },
			Nack: func(_ context.Context) error { return nil },
		}
		driver.deliveries <- delivery
	}

	<-allWorkersStarted

	stopCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err = consumer.Stop(stopCtx)
	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err, "should timeout when all workers are blocked")
}

type mockDriver struct {
	attachCalled atomic.Bool
	cancelCalled atomic.Bool
	deliveries   chan *queueapi.Delivery
	cancelCtx    context.Context
	cancel       context.CancelFunc
}

func (m *mockDriver) Attach(ctx context.Context, _ registry.ID, deliveries chan<- *queueapi.Delivery) (context.CancelFunc, error) {
	m.attachCalled.Store(true)
	m.deliveries = make(chan *queueapi.Delivery, 10)
	m.cancelCtx, m.cancel = context.WithCancel(ctx)

	// Forward deliveries
	go func() {
		defer close(deliveries)
		for {
			select {
			case delivery, ok := <-m.deliveries:
				if !ok {
					return
				}
				select {
				case deliveries <- delivery:
				case <-m.cancelCtx.Done():
					return
				}
			case <-m.cancelCtx.Done():
				return
			}
		}
	}()

	return func() {
		m.cancelCalled.Store(true)
		if m.cancel != nil {
			m.cancel()
		}
		close(m.deliveries)
	}, nil
}

func (m *mockDriver) Publish(ctx context.Context, q registry.ID, msgs ...*queueapi.Message) error {
	return nil
}

func (m *mockDriver) DeclareQueue(ctx context.Context, q registry.ID, opts attrs.Attributes) error {
	return nil
}

func (m *mockDriver) GetQueueInfo(ctx context.Context, q registry.ID) (attrs.Attributes, error) {
	return attrs.NewBag(), nil
}

type mockFuncRegistry struct {
	callCalled atomic.Bool
	callErr    error
	callDelay  time.Duration
	onCall     func()
}

func (m *mockFuncRegistry) Call(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
	m.callCalled.Store(true)
	if m.onCall != nil {
		m.onCall()
	}
	if m.callDelay > 0 {
		select {
		case <-time.After(m.callDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if m.callErr != nil {
		return nil, m.callErr
	}
	return &runtime.Result{}, nil
}
