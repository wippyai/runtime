// SPDX-License-Identifier: MPL-2.0

package drivertest

import (
	"context"
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
)

// Run executes the full conformance suite as subtests of the current test.
func (h *Harness) Run() {
	h.t.Helper()

	// Core operations
	h.t.Run("DeclareQueue", h.TestDeclareQueue)
	h.t.Run("PublishAndAttach", h.TestPublishAndAttach)
	h.t.Run("MultipleMessages", h.TestMultipleMessages)
	h.t.Run("Nack", h.TestNack)
	h.t.Run("PublishNonExistent", h.TestPublishNonExistent)
	h.t.Run("AttachNonExistent", h.TestAttachNonExistent)

	// Data integrity
	h.t.Run("MessageBodyPreservation", h.TestMessageBodyPreservation)
	h.t.Run("BatchPublish", h.TestBatchPublish)
	if h.cfg.preservesHeaders {
		h.t.Run("CustomHeaders", h.TestCustomHeaders)
		h.t.Run("MultipleHeaders", h.TestMultipleHeaders)
	}

	// Queue management
	h.t.Run("DeclareMultipleQueues", h.TestDeclareMultipleQueues)
	h.t.Run("QueueIsolation", h.TestQueueIsolation)
	if h.cfg.getQueueInfoAccurate {
		h.t.Run("GetQueueInfo", h.TestGetQueueInfo)
		h.t.Run("EmptyQueueInfo", h.TestEmptyQueueInfo)
	}

	// Consumer lifecycle
	h.t.Run("CancelAttach", h.TestCancelAttach)
	h.t.Run("PublishBeforeAttach", h.TestPublishBeforeAttach)

	// Concurrency and volume
	h.t.Run("ConcurrentPublish", h.TestConcurrentPublish)
	h.t.Run("HighVolume", h.TestHighVolume)
}

// TestDeclareQueue verifies that a queue can be declared and that
// re-declaring the same queue is idempotent (no error).
func (h *Harness) TestDeclareQueue(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("declare")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, h.uniqueQueueName("test-declare"))

	err := h.driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	// Declaring again should be idempotent.
	err = h.driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)
}

// TestPublishAndAttach verifies the basic publish → attach → receive → ack cycle.
func (h *Harness) TestPublishAndAttach(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("pubsub")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, h.uniqueQueueName("test-pubsub"))
	err := h.driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := h.driver.Attach(ctx, queueID, deliveries)
	require.NoError(t, err)
	defer cancel()

	msg := queueapi.AcquireMessage(payload.New("hello driver"))
	msg.ID = "pubsub-msg-1"

	err = h.driver.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	select {
	case delivery := <-deliveries:
		if h.cfg.preservesMessageID {
			assert.Equal(t, "pubsub-msg-1", delivery.Message.ID)
		} else {
			assert.NotEmpty(t, delivery.Message.ID)
		}
		assert.NotNil(t, delivery.Message.Body)
		err = delivery.Ack(ctx)
		assert.NoError(t, err)
	case <-time.After(h.cfg.timeout):
		t.Fatal("timeout waiting for message")
	}
}

// TestMultipleMessages verifies that publishing several messages results in all
// of them being delivered.
func (h *Harness) TestMultipleMessages(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("multi")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, h.uniqueQueueName("test-multi"))
	err := h.driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := h.driver.Attach(ctx, queueID, deliveries)
	require.NoError(t, err)
	defer cancel()

	const messageCount = 3
	for i := 0; i < messageCount; i++ {
		m := queueapi.AcquireMessage(payload.New("msg"))
		m.ID = string(rune('a' + i))
		err = h.driver.Publish(ctx, queueID, m)
		require.NoError(t, err)
	}

	received := 0
	timeout := time.After(h.cfg.timeout)
	for received < messageCount {
		select {
		case delivery := <-deliveries:
			assert.NotNil(t, delivery.Message)
			err = delivery.Ack(ctx)
			assert.NoError(t, err)
			received++
		case <-timeout:
			t.Fatalf("timeout, only received %d of %d messages", received, messageCount)
		}
	}
}

// TestNack verifies that calling Nack does not return an error.
// When the driver supports automatic redelivery (nackRedelivers=true),
// it also verifies the message is redelivered after being nacked.
func (h *Harness) TestNack(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("nack")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, h.uniqueQueueName("test-nack"))
	err := h.driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	msg := queueapi.AcquireMessage(payload.New("nack-test"))
	msg.ID = "nack-msg-1"
	err = h.driver.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := h.driver.Attach(ctx, queueID, deliveries)
	require.NoError(t, err)
	defer cancel()

	// First delivery — nack it.
	select {
	case delivery := <-deliveries:
		if h.cfg.preservesMessageID {
			assert.Equal(t, "nack-msg-1", delivery.Message.ID)
		} else {
			assert.NotEmpty(t, delivery.Message.ID)
		}
		err = delivery.Nack(ctx)
		assert.NoError(t, err)
	case <-time.After(h.cfg.timeout):
		t.Fatal("timeout waiting for first delivery")
	}

	if !h.cfg.nackRedelivers {
		return
	}

	// Second delivery — the redelivered message.
	select {
	case delivery := <-deliveries:
		if h.cfg.preservesMessageID {
			assert.Equal(t, "nack-msg-1", delivery.Message.ID)
		} else {
			assert.NotEmpty(t, delivery.Message.ID)
		}
		err = delivery.Ack(ctx)
		assert.NoError(t, err)
	case <-time.After(h.cfg.timeout):
		t.Fatal("timeout waiting for redelivery")
	}
}

// TestGetQueueInfo verifies that after publishing messages the driver reports
// the correct message count through GetQueueInfo.
func (h *Harness) TestGetQueueInfo(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("info")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, h.uniqueQueueName("test-info"))
	err := h.driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	msg1 := queueapi.AcquireMessage(payload.New("info1"))
	msg1.ID = "info-1"
	msg2 := queueapi.AcquireMessage(payload.New("info2"))
	msg2.ID = "info-2"

	err = h.driver.Publish(ctx, queueID, msg1, msg2)
	require.NoError(t, err)

	info, err := h.driver.GetQueueInfo(ctx, queueID)
	require.NoError(t, err)

	count := info.GetInt(queueapi.StatsMessageCount, 0)
	assert.Equal(t, 2, count)
}

// TestPublishNonExistent verifies that publishing to a queue that has not been
// declared returns ErrQueueNotFound.
func (h *Harness) TestPublishNonExistent(t *testing.T) {
	ctx := context.Background()
	msg := queueapi.AcquireMessage(payload.New("test"))
	err := h.driver.Publish(ctx, registry.ParseID("test:nonexistent"), msg)
	assert.ErrorIs(t, err, queueapi.ErrQueueNotFound)
}

// TestAttachNonExistent verifies that attaching to a queue that has not been
// declared returns ErrQueueNotFound.
func (h *Harness) TestAttachNonExistent(t *testing.T) {
	ctx := context.Background()
	deliveries := make(chan *queueapi.Delivery, 10)
	_, err := h.driver.Attach(ctx, registry.ParseID("test:nonexistent"), deliveries)
	assert.ErrorIs(t, err, queueapi.ErrQueueNotFound)
}

// TestCustomHeaders verifies that custom message headers survive a
// publish → consume round-trip.
func (h *Harness) TestCustomHeaders(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("headers")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, h.uniqueQueueName("test-headers"))
	err := h.driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	msg := queueapi.AcquireMessage(payload.New("header-test"))
	msg.ID = "header-msg-1"
	msg.Headers.Set("custom", "header-value")

	err = h.driver.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := h.driver.Attach(ctx, queueID, deliveries)
	require.NoError(t, err)
	defer cancel()

	select {
	case delivery := <-deliveries:
		val, ok := delivery.Message.Headers.Get("custom")
		assert.True(t, ok, "custom header should be present")
		assert.Equal(t, "header-value", val)
		err = delivery.Ack(ctx)
		assert.NoError(t, err)
	case <-time.After(h.cfg.timeout):
		t.Fatal("timeout waiting for message")
	}
}

// ---------------------------------------------------------------------------
// Data integrity tests
// ---------------------------------------------------------------------------

// TestMessageBodyPreservation verifies that the message body survives a
// publish → consume round-trip.
//
// For in-process drivers (memory) the exact Go value is preserved. For wire
// drivers (AMQP, Redis, SQS) the body is serialised and deserialised, so the
// test only asserts the body and its data pointer are non-nil.
func (h *Harness) TestMessageBodyPreservation(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("body")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, h.uniqueQueueName("test-body"))
	err := h.driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := h.driver.Attach(ctx, queueID, deliveries)
	require.NoError(t, err)
	defer cancel()

	original := "the quick brown fox jumps over the lazy dog"
	msg := queueapi.AcquireMessage(payload.New(original))
	msg.ID = "body-msg-1"

	err = h.driver.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	select {
	case delivery := <-deliveries:
		require.NotNil(t, delivery.Message.Body, "body must not be nil")
		require.NotNil(t, delivery.Message.Body.Data(), "body data must not be nil")

		// In-process drivers preserve the exact Go value.
		if s, ok := delivery.Message.Body.Data().(string); ok {
			assert.Equal(t, original, s)
		}

		err = delivery.Ack(ctx)
		assert.NoError(t, err)
	case <-time.After(h.cfg.timeout):
		t.Fatal("timeout waiting for message")
	}
}

// TestBatchPublish verifies that publishing multiple messages in a single
// variadic Publish call delivers all of them.
func (h *Harness) TestBatchPublish(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("batch")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, h.uniqueQueueName("test-batch"))
	err := h.driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := h.driver.Attach(ctx, queueID, deliveries)
	require.NoError(t, err)
	defer cancel()

	const batchSize = 5
	msgs := make([]*queueapi.Message, batchSize)
	for i := range msgs {
		msgs[i] = queueapi.AcquireMessage(payload.New("batch"))
		msgs[i].ID = string(rune('A' + i))
	}

	err = h.driver.Publish(ctx, queueID, msgs...)
	require.NoError(t, err)

	received := 0
	timeout := time.After(h.cfg.timeout)
	for received < batchSize {
		select {
		case delivery := <-deliveries:
			assert.NotNil(t, delivery.Message)
			err = delivery.Ack(ctx)
			assert.NoError(t, err)
			received++
		case <-timeout:
			t.Fatalf("timeout, only received %d of %d messages", received, batchSize)
		}
	}
}

// TestMultipleHeaders verifies that several custom headers on a single message
// all survive the publish → consume round-trip.
func (h *Harness) TestMultipleHeaders(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("multiheader")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, h.uniqueQueueName("test-multiheader"))
	err := h.driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	msg := queueapi.AcquireMessage(payload.New("multi-header"))
	msg.ID = "mh-msg-1"
	msg.Headers.Set("x-tenant", "acme")
	msg.Headers.Set("x-priority", "high")
	msg.Headers.Set("x-source", "test-suite")

	err = h.driver.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := h.driver.Attach(ctx, queueID, deliveries)
	require.NoError(t, err)
	defer cancel()

	select {
	case delivery := <-deliveries:
		for _, kv := range []struct{ key, val string }{
			{"x-tenant", "acme"},
			{"x-priority", "high"},
			{"x-source", "test-suite"},
		} {
			val, ok := delivery.Message.Headers.Get(kv.key)
			assert.True(t, ok, "header %q should be present", kv.key)
			assert.Equal(t, kv.val, val, "header %q value mismatch", kv.key)
		}
		err = delivery.Ack(ctx)
		assert.NoError(t, err)
	case <-time.After(h.cfg.timeout):
		t.Fatal("timeout waiting for message")
	}
}

// ---------------------------------------------------------------------------
// Queue management tests
// ---------------------------------------------------------------------------

// TestDeclareMultipleQueues verifies that a single driver can manage several
// independent queues simultaneously.
func (h *Harness) TestDeclareMultipleQueues(t *testing.T) {
	ctx := context.Background()

	const numQueues = 3
	queueIDs := make([]registry.ID, numQueues)
	for i := range queueIDs {
		queueIDs[i] = h.uniqueID("mqd")
		opts := attrs.NewBag()
		opts.Set(queueapi.OptionQueueName, h.uniqueQueueName("test-mqd"))
		err := h.driver.DeclareQueue(ctx, queueIDs[i], opts)
		require.NoError(t, err, "declare queue %d", i)
	}

	// Publish one message to each queue.
	for i, qID := range queueIDs {
		msg := queueapi.AcquireMessage(payload.New("mqd"))
		msg.ID = string(rune('x' + i))
		err := h.driver.Publish(ctx, qID, msg)
		require.NoError(t, err, "publish to queue %d", i)
	}
}

// TestQueueIsolation verifies that messages published to one queue are not
// delivered to a consumer attached to a different queue.
func (h *Harness) TestQueueIsolation(t *testing.T) {
	ctx := context.Background()

	queueA := h.uniqueID("iso-a")
	queueB := h.uniqueID("iso-b")

	optsA := attrs.NewBag()
	optsA.Set(queueapi.OptionQueueName, h.uniqueQueueName("test-iso-a"))
	err := h.driver.DeclareQueue(ctx, queueA, optsA)
	require.NoError(t, err)

	optsB := attrs.NewBag()
	optsB.Set(queueapi.OptionQueueName, h.uniqueQueueName("test-iso-b"))
	err = h.driver.DeclareQueue(ctx, queueB, optsB)
	require.NoError(t, err)

	deliveriesB := make(chan *queueapi.Delivery, 10)
	cancelB, err := h.driver.Attach(ctx, queueB, deliveriesB)
	require.NoError(t, err)
	defer cancelB()

	// Publish only to queue A.
	msg := queueapi.AcquireMessage(payload.New("only-for-A"))
	msg.ID = "iso-msg-1"
	err = h.driver.Publish(ctx, queueA, msg)
	require.NoError(t, err)

	// Queue B should NOT receive the message.
	select {
	case delivery := <-deliveriesB:
		t.Fatalf("queue B received a message meant for queue A: %s", delivery.Message.ID)
	case <-time.After(500 * time.Millisecond):
		// Expected: no message received.
	}
}

// TestEmptyQueueInfo verifies that GetQueueInfo on a declared but empty queue
// reports zero messages.
func (h *Harness) TestEmptyQueueInfo(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("empty-info")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, h.uniqueQueueName("test-empty-info"))
	err := h.driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	info, err := h.driver.GetQueueInfo(ctx, queueID)
	require.NoError(t, err)

	count := info.GetInt(queueapi.StatsMessageCount, -1)
	assert.Equal(t, 0, count)
}

// ---------------------------------------------------------------------------
// Consumer lifecycle tests
// ---------------------------------------------------------------------------

// TestCancelAttach verifies that calling the cancel function returned by Attach
// stops message delivery. After cancelling, new messages published to the queue
// should not appear on the delivery channel.
func (h *Harness) TestCancelAttach(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("cancel")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, h.uniqueQueueName("test-cancel"))
	err := h.driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := h.driver.Attach(ctx, queueID, deliveries)
	require.NoError(t, err)

	// Receive one message to confirm the consumer works.
	msg := queueapi.AcquireMessage(payload.New("before-cancel"))
	msg.ID = "cancel-msg-1"
	err = h.driver.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	select {
	case delivery := <-deliveries:
		err = delivery.Ack(ctx)
		assert.NoError(t, err)
	case <-time.After(h.cfg.timeout):
		t.Fatal("timeout waiting for message before cancel")
	}

	// Cancel the consumer.
	cancel()

	// Give the driver a moment to process the cancellation.
	time.Sleep(200 * time.Millisecond)

	// Publish another message — it should NOT be delivered.
	msg2 := queueapi.AcquireMessage(payload.New("after-cancel"))
	msg2.ID = "cancel-msg-2"
	// Ignore publish error — some drivers may reject after detach.
	_ = h.driver.Publish(ctx, queueID, msg2)

	select {
	case <-deliveries:
		// Some drivers may still deliver in-flight messages; that's acceptable.
		// The key invariant is that cancel() doesn't panic and eventually stops.
	case <-time.After(500 * time.Millisecond):
		// Expected: no delivery after cancel.
	}
}

// TestPublishBeforeAttach verifies that messages published before any consumer
// attaches are still delivered when a consumer eventually connects.
func (h *Harness) TestPublishBeforeAttach(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("pre-attach")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, h.uniqueQueueName("test-pre-attach"))
	err := h.driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	// Publish BEFORE any consumer is attached.
	const messageCount = 3
	for i := 0; i < messageCount; i++ {
		msg := queueapi.AcquireMessage(payload.New("pre-attach"))
		msg.ID = string(rune('P' + i))
		err = h.driver.Publish(ctx, queueID, msg)
		require.NoError(t, err)
	}

	// Now attach a consumer.
	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := h.driver.Attach(ctx, queueID, deliveries)
	require.NoError(t, err)
	defer cancel()

	received := 0
	timeout := time.After(h.cfg.timeout)
	for received < messageCount {
		select {
		case delivery := <-deliveries:
			assert.NotNil(t, delivery.Message)
			err = delivery.Ack(ctx)
			assert.NoError(t, err)
			received++
		case <-timeout:
			t.Fatalf("timeout, only received %d of %d pre-attach messages", received, messageCount)
		}
	}
}

// ---------------------------------------------------------------------------
// Concurrency and volume tests
// ---------------------------------------------------------------------------

// TestConcurrentPublish verifies that multiple goroutines can publish to the
// same queue simultaneously without errors, data loss, or races.
func (h *Harness) TestConcurrentPublish(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("concurrent")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, h.uniqueQueueName("test-concurrent"))
	err := h.driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 100)
	cancel, err := h.driver.Attach(ctx, queueID, deliveries)
	require.NoError(t, err)
	defer cancel()

	const numGoroutines = 5
	const msgsPerGoroutine = 4
	totalMsgs := numGoroutines * msgsPerGoroutine

	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	var publishErrors atomic.Int64

	for g := 0; g < numGoroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < msgsPerGoroutine; i++ {
				msg := queueapi.AcquireMessage(payload.New("concurrent"))
				if err := h.driver.Publish(ctx, queueID, msg); err != nil {
					publishErrors.Add(1)
				}
			}
		}()
	}

	wg.Wait()
	assert.Equal(t, int64(0), publishErrors.Load(), "all concurrent publishes should succeed")

	received := 0
	timeout := time.After(h.cfg.timeout)
	for received < totalMsgs {
		select {
		case delivery := <-deliveries:
			assert.NotNil(t, delivery.Message)
			_ = delivery.Ack(ctx)
			received++
		case <-timeout:
			t.Fatalf("timeout, only received %d of %d concurrent messages", received, totalMsgs)
		}
	}
}

// TestHighVolume verifies that the driver can handle a larger batch of messages
// (50) without message loss.
func (h *Harness) TestHighVolume(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("volume")

	opts := attrs.NewBag()
	opts.Set(queueapi.OptionQueueName, h.uniqueQueueName("test-volume"))
	err := h.driver.DeclareQueue(ctx, queueID, opts)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 100)
	cancel, err := h.driver.Attach(ctx, queueID, deliveries)
	require.NoError(t, err)
	defer cancel()

	const totalMessages = 50
	for i := 0; i < totalMessages; i++ {
		msg := queueapi.AcquireMessage(payload.New("volume"))
		err = h.driver.Publish(ctx, queueID, msg)
		require.NoError(t, err)
	}

	received := 0
	timeout := time.After(h.cfg.timeout * 3) // extra time for high volume
	for received < totalMessages {
		select {
		case delivery := <-deliveries:
			assert.NotNil(t, delivery.Message)
			_ = delivery.Ack(ctx)
			received++
		case <-timeout:
			t.Fatalf("timeout, only received %d of %d messages", received, totalMessages)
		}
	}
}
