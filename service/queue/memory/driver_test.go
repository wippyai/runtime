// SPDX-License-Identifier: MPL-2.0

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
	"github.com/wippyai/runtime/service/queue/drivertest"
	"go.uber.org/zap/zaptest"
)

func requireDelivery(t *testing.T, deliveries <-chan *queueapi.Delivery) *queueapi.Delivery {
	t.Helper()
	select {
	case delivery := <-deliveries:
		require.NotNil(t, delivery)
		require.NotNil(t, delivery.Message)
		return delivery
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for delivery")
		return nil
	}
}

func requireNoDelivery(t *testing.T, deliveries <-chan *queueapi.Delivery) {
	t.Helper()
	select {
	case delivery := <-deliveries:
		t.Fatalf("unexpected delivery: %#v", delivery)
	case <-time.After(50 * time.Millisecond):
	}
}

func newStartedMemoryDriver(t *testing.T) (context.Context, *Driver, registry.ID, chan *queueapi.Delivery) {
	t.Helper()
	logger := zaptest.NewLogger(t)
	driver := NewDriver(registry.ParseID("test:driver"), logger)
	ctx := context.Background()
	queueID := registry.ParseID("test:queue1")

	require.NoError(t, driver.DeclareQueue(ctx, queueID, &queueapi.Config{}))
	_, err := driver.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, driver.Stop(ctx)) })

	deliveries := make(chan *queueapi.Delivery, 8)
	cancel, err := driver.Attach(ctx, queueID, &queueapi.ConsumerOptions{}, deliveries)
	require.NoError(t, err)
	t.Cleanup(cancel)

	return ctx, driver, queueID, deliveries
}

// TestMemoryDriver_Conformance runs the shared driver conformance suite.
func TestMemoryDriver_Conformance(t *testing.T) {
	logger := zaptest.NewLogger(t)
	driver := NewDriver(registry.ParseID("test:driver"), logger)
	drivertest.New(t, driver).Run()
}

// --- Memory-specific tests below (internal state and lifecycle) ---

func TestMemoryDriver_DeclareQueueInternal(t *testing.T) {
	logger := zaptest.NewLogger(t)
	driver := NewDriver(registry.ParseID("test:driver"), logger)

	ctx := context.Background()
	queueID := registry.ParseID("test:queue1")
	memBag := attrs.NewBag()
	memBag.Set("max_length", 50)
	cfg := &queueapi.Config{DriverOptions: attrs.NewBag()}
	cfg.DriverOptions.Set("memory", memBag)

	err := driver.DeclareQueue(ctx, queueID, cfg)
	require.NoError(t, err)

	driver.mu.RLock()
	q, exists := driver.queues[queueID]
	driver.mu.RUnlock()

	assert.True(t, exists)
	assert.Equal(t, queueID, q.id)
	assert.Equal(t, 50, cap(q.messages))
}

func TestMemoryDriver_Stop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	driver := NewDriver(registry.ParseID("test:driver"), logger)

	ctx := context.Background()
	queueID := registry.ParseID("test:queue1")

	err := driver.DeclareQueue(ctx, queueID, &queueapi.Config{})
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

func TestMemoryDriver_PublishBeforeStart(t *testing.T) {
	logger := zaptest.NewLogger(t)
	driver := NewDriver(registry.ParseID("test:driver"), logger)

	ctx := context.Background()
	queueID := registry.ParseID("test:queue1")

	err := driver.DeclareQueue(ctx, queueID, &queueapi.Config{})
	require.NoError(t, err)

	msg := queueapi.AcquireMessage(payload.New("test"))
	msg.ID = "msg1"

	err = driver.Publish(ctx, queueID, msg)
	require.NoError(t, err, "publish should work before start")
}

func TestMemoryDriver_PublishAfterStop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	driver := NewDriver(registry.ParseID("test:driver"), logger)

	ctx := context.Background()
	queueID := registry.ParseID("test:queue1")

	err := driver.DeclareQueue(ctx, queueID, &queueapi.Config{})
	require.NoError(t, err)

	_, err = driver.Start(ctx)
	require.NoError(t, err)

	err = driver.Stop(ctx)
	require.NoError(t, err)

	msg := queueapi.AcquireMessage(payload.New("test"))
	msg.ID = "msg1"

	err = driver.Publish(ctx, queueID, msg)
	assert.ErrorIs(t, err, queueapi.ErrQueueNotFound, "publish should fail after stop")
}

func TestMemoryDriver_NackRedeliverySurvivesMessageRelease(t *testing.T) {
	ctx, driver, queueID, deliveries := newStartedMemoryDriver(t)

	msg := queueapi.AcquireMessageWithID("msg1", payload.New("payload-1"))
	msg.Headers.Set("job_id", "job-1")
	require.NoError(t, driver.Publish(ctx, queueID, msg))

	first := requireDelivery(t, deliveries)
	require.Equal(t, "msg1", first.Message.ID)
	require.NoError(t, first.Nack(ctx))

	queueapi.ReleaseMessage(first.Message)

	redelivered := requireDelivery(t, deliveries)
	require.Equal(t, "msg1", redelivered.Message.ID)
	require.NotNil(t, redelivered.Message.Body)
	require.Equal(t, "payload-1", redelivered.Message.Body.Data())
	require.Equal(t, "job-1", redelivered.Message.Headers.GetString("job_id", ""))
}

func TestMemoryDriver_NackRedeliveryPreservesMultipleHeaders(t *testing.T) {
	ctx, driver, queueID, deliveries := newStartedMemoryDriver(t)

	msg := queueapi.AcquireMessageWithID("msg-headers", payload.New("payload"))
	msg.Headers.Set("job_id", "job-headers")
	msg.Headers.Set(queueapi.HeaderCorrelationID, "corr-1")
	msg.Headers.Set("priority", 7)
	msg.Headers.Set("retryable", true)
	require.NoError(t, driver.Publish(ctx, queueID, msg))

	first := requireDelivery(t, deliveries)
	require.NoError(t, first.Nack(ctx))
	queueapi.ReleaseMessage(first.Message)

	redelivered := requireDelivery(t, deliveries)
	require.Equal(t, "job-headers", redelivered.Message.Headers.GetString("job_id", ""))
	require.Equal(t, "corr-1", redelivered.Message.Headers.GetString(queueapi.HeaderCorrelationID, ""))
	require.Equal(t, 7, redelivered.Message.Headers.GetInt("priority", 0))
	assert.Equal(t, true, redelivered.Message.Headers["retryable"])
}

func TestMemoryDriver_NackRedeliveryCloneIsIndependentFromReleasedOriginal(t *testing.T) {
	ctx, driver, queueID, deliveries := newStartedMemoryDriver(t)

	msg := queueapi.AcquireMessageWithID("msg-isolated", payload.New("body-a"))
	msg.Headers.Set("job_id", "job-original")
	require.NoError(t, driver.Publish(ctx, queueID, msg))

	first := requireDelivery(t, deliveries)
	require.NoError(t, first.Nack(ctx))
	first.Message.ID = "mutated-after-nack"
	first.Message.Headers.Set("job_id", "mutated-after-nack")
	first.Message.Headers.Set("extra", "should-not-leak")
	queueapi.ReleaseMessage(first.Message)

	redelivered := requireDelivery(t, deliveries)
	require.Equal(t, "msg-isolated", redelivered.Message.ID)
	require.Equal(t, "job-original", redelivered.Message.Headers.GetString("job_id", ""))
	require.Equal(t, "", redelivered.Message.Headers.GetString("extra", ""))
}

func TestMemoryDriver_NackRedeliveryCanBeNackedRepeatedly(t *testing.T) {
	ctx, driver, queueID, deliveries := newStartedMemoryDriver(t)

	msg := queueapi.AcquireMessageWithID("msg-repeat", payload.New("repeat-body"))
	msg.Headers.Set("job_id", "job-repeat")
	require.NoError(t, driver.Publish(ctx, queueID, msg))

	for i := 0; i < 3; i++ {
		delivery := requireDelivery(t, deliveries)
		require.Equal(t, "msg-repeat", delivery.Message.ID)
		require.NotNil(t, delivery.Message.Body)
		require.Equal(t, "repeat-body", delivery.Message.Body.Data())
		require.Equal(t, "job-repeat", delivery.Message.Headers.GetString("job_id", ""))
		require.NoError(t, delivery.Nack(ctx))
		queueapi.ReleaseMessage(delivery.Message)
	}

	final := requireDelivery(t, deliveries)
	require.Equal(t, "msg-repeat", final.Message.ID)
	require.Equal(t, "repeat-body", final.Message.Body.Data())
	require.Equal(t, "job-repeat", final.Message.Headers.GetString("job_id", ""))
}

func TestMemoryDriver_AckDoesNotRedeliverAfterRelease(t *testing.T) {
	ctx, driver, queueID, deliveries := newStartedMemoryDriver(t)

	msg := queueapi.AcquireMessageWithID("msg-ack", payload.New("ack-body"))
	require.NoError(t, driver.Publish(ctx, queueID, msg))

	first := requireDelivery(t, deliveries)
	require.NoError(t, first.Ack(ctx))
	queueapi.ReleaseMessage(first.Message)

	requireNoDelivery(t, deliveries)
}

func TestMemoryDriver_NackRedeliveryWithNilHeaderBag(t *testing.T) {
	ctx, driver, queueID, deliveries := newStartedMemoryDriver(t)

	msg := &queueapi.Message{ID: "manual", Body: payload.New("manual-body")}
	require.NoError(t, driver.Publish(ctx, queueID, msg))

	first := requireDelivery(t, deliveries)
	require.NoError(t, first.Nack(ctx))
	queueapi.ReleaseMessage(first.Message)

	redelivered := requireDelivery(t, deliveries)
	require.Equal(t, "manual", redelivered.Message.ID)
	require.NotNil(t, redelivered.Message.Headers)
	require.Equal(t, "manual-body", redelivered.Message.Body.Data())
}

func TestMemoryDriver_NackRedeliveryAfterPublishingSeveralMessages(t *testing.T) {
	ctx, driver, queueID, deliveries := newStartedMemoryDriver(t)

	for i := 1; i <= 3; i++ {
		msg := queueapi.AcquireMessageWithID("msg-batch-"+string(rune('0'+i)), payload.New("body-"+string(rune('0'+i))))
		msg.Headers.Set("job_id", "job-"+string(rune('0'+i)))
		require.NoError(t, driver.Publish(ctx, queueID, msg))
	}

	first := requireDelivery(t, deliveries)
	require.Equal(t, "msg-batch-1", first.Message.ID)
	require.NoError(t, first.Nack(ctx))
	queueapi.ReleaseMessage(first.Message)

	second := requireDelivery(t, deliveries)
	require.Equal(t, "msg-batch-2", second.Message.ID)
	require.NoError(t, second.Ack(ctx))
	queueapi.ReleaseMessage(second.Message)

	third := requireDelivery(t, deliveries)
	require.Equal(t, "msg-batch-3", third.Message.ID)
	require.NoError(t, third.Ack(ctx))
	queueapi.ReleaseMessage(third.Message)

	redelivered := requireDelivery(t, deliveries)
	require.Equal(t, "msg-batch-1", redelivered.Message.ID)
	require.Equal(t, "body-1", redelivered.Message.Body.Data())
	require.Equal(t, "job-1", redelivered.Message.Headers.GetString("job_id", ""))
}

func TestMemoryDriver_StopWithoutStart(t *testing.T) {
	logger := zaptest.NewLogger(t)
	driver := NewDriver(registry.ParseID("test:driver"), logger)

	ctx := context.Background()
	err := driver.Stop(ctx)
	require.NoError(t, err, "stop without start should not panic")
}
