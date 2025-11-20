package memory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap/zaptest"
)

func TestMemoryDriver_DeclareQueue(t *testing.T) {
	logger := zaptest.NewLogger(t)
	driver := NewDriver(registry.ParseID("test:driver"), logger)

	ctx := context.Background()
	queueID := registry.ParseID("test:queue1")
	opts := attrs.NewBag()
	opts.Set(queueapi.OptionMaxLength, 50)

	err := driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	driver.mu.RLock()
	q, exists := driver.queues[queueID]
	driver.mu.RUnlock()

	assert.True(t, exists)
	assert.Equal(t, queueID, q.id)
	assert.Equal(t, 50, cap(q.messages))
}

func TestMemoryDriver_PublishAndAttach(t *testing.T) {
	logger := zaptest.NewLogger(t)
	driver := NewDriver(registry.ParseID("test:driver"), logger)

	ctx := context.Background()
	queueID := registry.ParseID("test:queue1")

	err := driver.DeclareQueue(ctx, queueID, attrs.NewBag())
	require.NoError(t, err)

	body := payload.New("test message")
	msg := queueapi.AcquireMessage(body)
	msg.ID = "msg1"

	err = driver.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := driver.Attach(ctx, queueID, deliveries)
	require.NoError(t, err)
	defer cancel()

	select {
	case delivery := <-deliveries:
		assert.Equal(t, "msg1", delivery.Message.ID)
		bodyData, ok := delivery.Message.Body.Data().(string)
		require.True(t, ok, "expected string payload, got %T", delivery.Message.Body.Data())
		assert.Equal(t, "test message", bodyData)

		err = delivery.Ack(ctx)
		assert.NoError(t, err)

	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestMemoryDriver_MultipleMessages(t *testing.T) {
	logger := zaptest.NewLogger(t)
	driver := NewDriver(registry.ParseID("test:driver"), logger)

	ctx := context.Background()
	queueID := registry.ParseID("test:queue1")

	err := driver.DeclareQueue(ctx, queueID, attrs.NewBag())
	require.NoError(t, err)

	msgs := make([]*queueapi.Message, 3)
	for i := 0; i < 3; i++ {
		body := payload.New("msg" + string(rune('a'+i)))
		msg := queueapi.AcquireMessage(body)
		msg.ID = string(rune('a' + i))
		msgs[i] = msg
	}

	err = driver.Publish(ctx, queueID, msgs...)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := driver.Attach(ctx, queueID, deliveries)
	require.NoError(t, err)
	defer cancel()

	received := 0
	timeout := time.After(2 * time.Second)

	for received < 3 {
		select {
		case delivery := <-deliveries:
			assert.NotNil(t, delivery.Message)
			err = delivery.Ack(ctx)
			assert.NoError(t, err)
			received++
		case <-timeout:
			t.Fatalf("timeout, only received %d of 3 messages", received)
		}
	}
}

func TestMemoryDriver_Nack(t *testing.T) {
	logger := zaptest.NewLogger(t)
	driver := NewDriver(registry.ParseID("test:driver"), logger)

	ctx := context.Background()
	queueID := registry.ParseID("test:queue1")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionMaxLength, 10)
	err := driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	body := payload.New("test")
	msg := queueapi.AcquireMessage(body)
	msg.ID = "msg1"

	err = driver.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := driver.Attach(ctx, queueID, deliveries)
	require.NoError(t, err)
	defer cancel()

	delivery := <-deliveries
	assert.Equal(t, "msg1", delivery.Message.ID)

	err = delivery.Nack(ctx)
	assert.NoError(t, err)

	redelivered := <-deliveries
	assert.Equal(t, "msg1", redelivered.Message.ID)
}

func TestMemoryDriver_GetQueueInfo(t *testing.T) {
	logger := zaptest.NewLogger(t)
	driver := NewDriver(registry.ParseID("test:driver"), logger)

	ctx := context.Background()
	queueID := registry.ParseID("test:queue1")

	err := driver.DeclareQueue(ctx, queueID, attrs.NewBag())
	require.NoError(t, err)

	body1 := payload.New("msg1")
	msg1 := queueapi.AcquireMessage(body1)
	msg1.ID = "msg1"
	body2 := payload.New("msg2")
	msg2 := queueapi.AcquireMessage(body2)
	msg2.ID = "msg2"

	err = driver.Publish(ctx, queueID, msg1, msg2)
	require.NoError(t, err)

	info, err := driver.GetQueueInfo(ctx, queueID)
	require.NoError(t, err)

	count := info.GetInt(queueapi.StatsMessageCount, 0)
	ready := info.GetInt(queueapi.StatsReady, 0)

	assert.Equal(t, 2, count)
	assert.Equal(t, 2, ready)
}

func TestMemoryDriver_Stop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	driver := NewDriver(registry.ParseID("test:driver"), logger)

	ctx := context.Background()
	queueID := registry.ParseID("test:queue1")

	err := driver.DeclareQueue(ctx, queueID, attrs.NewBag())
	require.NoError(t, err)

	_, err = driver.Start(ctx)
	require.NoError(t, err)

	err = driver.Stop(ctx)
	require.NoError(t, err)

	driver.mu.RLock()
	queueCount := len(driver.queues)
	driver.mu.RUnlock()

	assert.Equal(t, 0, queueCount)
}
