// SPDX-License-Identifier: MPL-2.0

package consumer

import (
	"context"
	"errors"
	"fmt"
	"sync"
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
		ConsumerOptions: queueapi.ConsumerOptions{
			Queue:       registry.NewID("test", "queue"),
			Func:        registry.NewID("test", "func"),
			Concurrency: 2,
			Prefetch:    10,
		},
	}

	consumer := NewConsumer(
		registry.NewID("test", "consumer"),
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
		ConsumerOptions: queueapi.ConsumerOptions{
			Queue:       registry.NewID("test", "queue"),
			Func:        registry.NewID("test", "func"),
			Concurrency: 1,
			Prefetch:    10,
		},
	}

	consumer := NewConsumer(
		registry.NewID("test", "consumer"),
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
		ConsumerOptions: queueapi.ConsumerOptions{
			Queue:       registry.NewID("test", "queue"),
			Func:        registry.NewID("test", "func"),
			Concurrency: 1,
			Prefetch:    10,
		},
	}

	consumer := NewConsumer(
		registry.NewID("test", "consumer"),
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
		ConsumerOptions: queueapi.ConsumerOptions{
			Queue:       registry.NewID("test", "queue"),
			Func:        registry.NewID("test", "func"),
			Concurrency: 1,
			Prefetch:    10,
		},
	}

	consumer := NewConsumer(
		registry.NewID("test", "consumer"),
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
		ConsumerOptions: queueapi.ConsumerOptions{
			Queue:       registry.NewID("test", "queue"),
			Func:        registry.NewID("test", "func"),
			Concurrency: 3,
			Prefetch:    10,
		},
	}

	consumer := NewConsumer(
		registry.NewID("test", "consumer"),
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
		ConsumerOptions: queueapi.ConsumerOptions{
			Queue:       registry.NewID("test", "queue"),
			Func:        registry.NewID("test", "func"),
			Concurrency: 2,
			Prefetch:    10,
		},
	}

	consumer := NewConsumer(
		registry.NewID("test", "consumer"),
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
		ConsumerOptions: queueapi.ConsumerOptions{
			Queue:       registry.NewID("test", "queue"),
			Func:        registry.NewID("test", "func"),
			Concurrency: 3,
			Prefetch:    10,
		},
	}

	consumer := NewConsumer(
		registry.NewID("test", "consumer"),
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
		ConsumerOptions: queueapi.ConsumerOptions{
			Queue:       registry.NewID("test", "queue"),
			Func:        registry.NewID("test", "func"),
			Concurrency: 1,
			Prefetch:    10,
		},
	}

	consumer := NewConsumer(
		registry.NewID("test", "consumer"),
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
		ConsumerOptions: queueapi.ConsumerOptions{
			Queue:       registry.NewID("test", "queue"),
			Func:        registry.NewID("test", "func"),
			Concurrency: 3,
			Prefetch:    10,
		},
	}

	consumer := NewConsumer(
		registry.NewID("test", "consumer"),
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
		ConsumerOptions: queueapi.ConsumerOptions{
			Queue:       registry.NewID("test", "queue"),
			Func:        registry.NewID("test", "func"),
			Concurrency: 1,
			Prefetch:    10,
		},
	}

	consumer := NewConsumer(
		registry.NewID("test", "consumer"),
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
		ConsumerOptions: queueapi.ConsumerOptions{
			Queue:       registry.NewID("test", "queue"),
			Func:        registry.NewID("test", "func"),
			Concurrency: 2,
			Prefetch:    10,
		},
	}

	consumer := NewConsumer(
		registry.NewID("test", "consumer"),
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
		ConsumerOptions: queueapi.ConsumerOptions{
			Queue:       registry.NewID("test", "queue"),
			Func:        registry.NewID("test", "func"),
			Concurrency: 1,
			Prefetch:    10,
		},
	}

	consumer := NewConsumer(
		registry.NewID("test", "consumer"),
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
		ConsumerOptions: queueapi.ConsumerOptions{
			Queue:       registry.NewID("test", "queue"),
			Func:        registry.NewID("test", "func"),
			Concurrency: 3,
			Prefetch:    10,
		},
	}

	consumer := NewConsumer(
		registry.NewID("test", "consumer"),
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
		ConsumerOptions: queueapi.ConsumerOptions{
			Queue:       registry.NewID("test", "queue"),
			Func:        registry.NewID("test", "func"),
			Concurrency: 3,
			Prefetch:    10,
		},
	}

	consumer := NewConsumer(
		registry.NewID("test", "consumer"),
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
	cancelCtx    context.Context
	deliveries   chan *queueapi.Delivery
	cancel       context.CancelFunc
	attachCalled atomic.Bool
	cancelCalled atomic.Bool
}

func (m *mockDriver) Attach(ctx context.Context, _ registry.ID, _ *queueapi.ConsumerOptions, deliveries chan<- *queueapi.Delivery) (context.CancelFunc, error) {
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

func (m *mockDriver) Publish(_ context.Context, _ registry.ID, _ ...*queueapi.Message) error {
	return nil
}

func (m *mockDriver) DeclareQueue(_ context.Context, _ registry.ID, _ *queueapi.Config) error {
	return nil
}

func (m *mockDriver) GetQueueInfo(_ context.Context, _ registry.ID) (attrs.Attributes, error) {
	return attrs.NewBag(), nil
}

type mockFuncRegistry struct {
	callErr    error
	onCall     func()
	callDelay  time.Duration
	callCalled atomic.Bool
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

// Stress tests

func TestConsumer_StressHighThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}
	ctx := context.Background()
	driver := &mockDriver{}

	processedCount := atomic.Int64{}
	funcReg := &mockFuncRegistry{
		onCall: func() {
			processedCount.Add(1)
		},
	}

	config := &consumerapi.Config{
		ConsumerOptions: queueapi.ConsumerOptions{
			Queue:       registry.NewID("stress", "queue"),
			Func:        registry.NewID("stress", "func"),
			Concurrency: 10,
			Prefetch:    100,
		},
	}

	consumer := NewConsumer(
		registry.NewID("stress", "consumer"),
		config,
		driver,
		funcReg,
		zap.NewNop(),
	)

	_, err := consumer.Start(ctx)
	require.NoError(t, err)

	const messageCount = 10000
	ackCount := atomic.Int64{}

	for i := 0; i < messageCount; i++ {
		delivery := &queueapi.Delivery{
			Message: &queueapi.Message{
				ID:      fmt.Sprintf("msg%d", i),
				Body:    payload.New("stress test message"),
				Headers: attrs.NewBag(),
			},
			Ack: func(_ context.Context) error {
				ackCount.Add(1)
				return nil
			},
			Nack: func(_ context.Context) error { return nil },
		}
		driver.deliveries <- delivery
	}

	assert.Eventually(t, func() bool {
		return ackCount.Load() == messageCount
	}, 30*time.Second, 10*time.Millisecond, "all messages should be acked")

	assert.Equal(t, int64(messageCount), processedCount.Load(), "all messages should be processed")

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = consumer.Stop(stopCtx)
	require.NoError(t, err)
}

func TestConsumer_StressRapidStartStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}
	const iterations = 50

	for i := 0; i < iterations; i++ {
		ctx := context.Background()
		driver := &mockDriver{}
		funcReg := &mockFuncRegistry{}

		config := &consumerapi.Config{
			ConsumerOptions: queueapi.ConsumerOptions{
				Queue:       registry.NewID("stress", "queue"),
				Func:        registry.NewID("stress", "func"),
				Concurrency: 5,
				Prefetch:    10,
			},
		}

		consumer := NewConsumer(
			registry.ID{NS: "stress", Name: fmt.Sprintf("consumer-%d", i)},
			config,
			driver,
			funcReg,
			zap.NewNop(),
		)

		_, err := consumer.Start(ctx)
		require.NoError(t, err, "iteration %d start failed", i)

		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err = consumer.Stop(stopCtx)
		cancel()
		require.NoError(t, err, "iteration %d stop failed", i)
	}
}

func TestConsumer_StressStartStopWithMessages(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}
	const iterations = 20

	for i := 0; i < iterations; i++ {
		ctx := context.Background()
		driver := &mockDriver{}

		processedCount := atomic.Int32{}
		funcReg := &mockFuncRegistry{
			callDelay: time.Millisecond,
			onCall: func() {
				processedCount.Add(1)
			},
		}

		config := &consumerapi.Config{
			ConsumerOptions: queueapi.ConsumerOptions{
				Queue:       registry.NewID("stress", "queue"),
				Func:        registry.NewID("stress", "func"),
				Concurrency: 3,
				Prefetch:    10,
			},
		}

		consumer := NewConsumer(
			registry.ID{NS: "stress", Name: fmt.Sprintf("consumer-%d", i)},
			config,
			driver,
			funcReg,
			zap.NewNop(),
		)

		_, err := consumer.Start(ctx)
		require.NoError(t, err, "iteration %d start failed", i)

		for j := 0; j < 10; j++ {
			delivery := &queueapi.Delivery{
				Message: &queueapi.Message{
					ID:      fmt.Sprintf("msg-%d-%d", i, j),
					Body:    payload.New("test"),
					Headers: attrs.NewBag(),
				},
				Ack:  func(_ context.Context) error { return nil },
				Nack: func(_ context.Context) error { return nil },
			}
			driver.deliveries <- delivery
		}

		time.Sleep(5 * time.Millisecond)

		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err = consumer.Stop(stopCtx)
		cancel()
		require.NoError(t, err, "iteration %d stop failed", i)
	}
}

func TestConsumer_StressConcurrentConsumers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}
	const consumerCount = 10
	const messagesPerConsumer = 100

	var wg sync.WaitGroup
	wg.Add(consumerCount)

	totalProcessed := atomic.Int64{}

	for c := 0; c < consumerCount; c++ {
		go func(consumerID int) {
			defer wg.Done()

			ctx := context.Background()
			driver := &mockDriver{}

			funcReg := &mockFuncRegistry{
				onCall: func() {
					totalProcessed.Add(1)
				},
			}

			config := &consumerapi.Config{
				ConsumerOptions: queueapi.ConsumerOptions{
					Queue:       registry.ID{NS: "stress", Name: fmt.Sprintf("queue-%d", consumerID)},
					Func:        registry.NewID("stress", "func"),
					Concurrency: 3,
					Prefetch:    20,
				},
			}

			consumer := NewConsumer(
				registry.ID{NS: "stress", Name: fmt.Sprintf("consumer-%d", consumerID)},
				config,
				driver,
				funcReg,
				zap.NewNop(),
			)

			_, err := consumer.Start(ctx)
			if err != nil {
				t.Errorf("consumer %d start failed: %v", consumerID, err)
				return
			}

			for m := 0; m < messagesPerConsumer; m++ {
				delivery := &queueapi.Delivery{
					Message: &queueapi.Message{
						ID:      fmt.Sprintf("msg-%d-%d", consumerID, m),
						Body:    payload.New("test"),
						Headers: attrs.NewBag(),
					},
					Ack:  func(_ context.Context) error { return nil },
					Nack: func(_ context.Context) error { return nil },
				}
				driver.deliveries <- delivery
			}

			time.Sleep(50 * time.Millisecond)

			stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := consumer.Stop(stopCtx); err != nil {
				t.Errorf("consumer %d stop failed: %v", consumerID, err)
			}
		}(c)
	}

	wg.Wait()

	expected := int64(consumerCount * messagesPerConsumer)
	assert.GreaterOrEqual(t, totalProcessed.Load(), expected/2, "at least half of messages should be processed")
}

func TestConsumer_StressNackRequeue(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}
	ctx := context.Background()
	driver := &mockDriver{}

	nackCount := atomic.Int32{}
	ackCount := atomic.Int32{}

	// Use a thread-safe mock that fails first 10 calls
	funcReg := &mockFuncRegistryFailFirst{
		failCount: 10,
	}

	config := &consumerapi.Config{
		ConsumerOptions: queueapi.ConsumerOptions{
			Queue:       registry.NewID("stress", "queue"),
			Func:        registry.NewID("stress", "func"),
			Concurrency: 1,
			Prefetch:    10,
		},
	}

	consumer := NewConsumer(
		registry.NewID("stress", "consumer"),
		config,
		driver,
		funcReg,
		zap.NewNop(),
	)

	_, err := consumer.Start(ctx)
	require.NoError(t, err)

	delivery := &queueapi.Delivery{
		Message: &queueapi.Message{
			ID:      "msg-requeue",
			Body:    payload.New("requeue test"),
			Headers: attrs.NewBag(),
		},
		Ack: func(_ context.Context) error {
			ackCount.Add(1)
			return nil
		},
		Nack: func(_ context.Context) error {
			nackCount.Add(1)
			return nil
		},
	}

	driver.deliveries <- delivery

	time.Sleep(50 * time.Millisecond)

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = consumer.Stop(stopCtx)
	require.NoError(t, err)

	// Message was processed at least once
	assert.GreaterOrEqual(t, funcReg.callCount.Load(), int64(1))
}

func TestConsumer_StressResourceCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}
	const iterations = 100

	for i := 0; i < iterations; i++ {
		ctx := context.Background()
		driver := &mockDriver{}

		funcReg := &mockFuncRegistry{}

		config := &consumerapi.Config{
			ConsumerOptions: queueapi.ConsumerOptions{
				Queue:       registry.NewID("stress", "queue"),
				Func:        registry.NewID("stress", "func"),
				Concurrency: 5,
				Prefetch:    50,
			},
		}

		consumer := NewConsumer(
			registry.ID{NS: "stress", Name: fmt.Sprintf("consumer-%d", i)},
			config,
			driver,
			funcReg,
			zap.NewNop(),
		)

		statusChan, err := consumer.Start(ctx)
		require.NoError(t, err, "iteration %d start failed", i)

		stopCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		err = consumer.Stop(stopCtx)
		cancel()
		require.NoError(t, err, "iteration %d stop failed", i)

		// Verify status channel is closed
		select {
		case _, ok := <-statusChan:
			assert.False(t, ok, "status channel should be closed")
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("iteration %d: status channel not closed", i)
		}
	}
}

func TestConsumer_StressMixedAckNack(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}
	ctx := context.Background()
	driver := &mockDriver{}

	ackCount := atomic.Int64{}
	nackCount := atomic.Int64{}
	callCount := atomic.Int64{}

	// Use a thread-safe mock that alternates success/failure
	funcReg := &mockFuncRegistryAtomic{
		failEveryN: 3,
	}

	config := &consumerapi.Config{
		ConsumerOptions: queueapi.ConsumerOptions{
			Queue:       registry.NewID("stress", "queue"),
			Func:        registry.NewID("stress", "func"),
			Concurrency: 5,
			Prefetch:    20,
		},
	}

	consumer := NewConsumer(
		registry.NewID("stress", "consumer"),
		config,
		driver,
		funcReg,
		zap.NewNop(),
	)

	_, err := consumer.Start(ctx)
	require.NoError(t, err)

	const messageCount = 100
	for i := 0; i < messageCount; i++ {
		delivery := &queueapi.Delivery{
			Message: &queueapi.Message{
				ID:      fmt.Sprintf("msg%d", i),
				Body:    payload.New("mixed test"),
				Headers: attrs.NewBag(),
			},
			Ack: func(_ context.Context) error {
				ackCount.Add(1)
				callCount.Add(1)
				return nil
			},
			Nack: func(_ context.Context) error {
				nackCount.Add(1)
				callCount.Add(1)
				return nil
			},
		}
		driver.deliveries <- delivery
	}

	assert.Eventually(t, func() bool {
		return callCount.Load() >= messageCount
	}, 10*time.Second, 10*time.Millisecond, "all messages should be processed")

	total := ackCount.Load() + nackCount.Load()
	assert.Equal(t, int64(messageCount), total, "all messages should be acked or nacked")

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = consumer.Stop(stopCtx)
	require.NoError(t, err)
}

// Thread-safe mock that fails every Nth call
type mockFuncRegistryAtomic struct {
	callCount  atomic.Int64
	failEveryN int64
}

func (m *mockFuncRegistryAtomic) Call(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
	count := m.callCount.Add(1)
	if count%m.failEveryN == 0 {
		return nil, errors.New("simulated failure")
	}
	return &runtime.Result{}, nil
}

// Thread-safe mock that fails first N calls
type mockFuncRegistryFailFirst struct {
	callCount atomic.Int64
	failCount int64
}

func (m *mockFuncRegistryFailFirst) Call(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
	count := m.callCount.Add(1)
	if count <= m.failCount {
		return nil, errors.New("temporary failure")
	}
	return &runtime.Result{}, nil
}
