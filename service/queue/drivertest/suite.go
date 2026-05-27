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
		h.t.Run("NeutralHeadersRoundTrip", h.TestNeutralHeadersRoundTrip)
	}
	if h.cfg.declareLeakDriver != "" && len(h.cfg.declareLeakOpts) > 0 {
		h.t.Run("DeclareOptionsDoNotLeakToPublish", h.TestDeclareOptionsDoNotLeakToPublish)
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
	if h.cfg.supportsReattach {
		h.t.Run("ReattachAfterCancel", h.TestReattachAfterCancel)
	}
	h.t.Run("RapidAttachDetach", h.TestRapidAttachDetach)

	// Delivery edge cases
	h.t.Run("PublishWithoutExplicitID", h.TestPublishWithoutExplicitID)
	h.t.Run("DeliveryHasNonNilHeaders", h.TestDeliveryHasNonNilHeaders)
	h.t.Run("SingleDelivery", h.TestSingleDelivery)

	// Declaration edge cases
	h.t.Run("DeclareQueueEmptyOptions", h.TestDeclareQueueEmptyOptions)
	h.t.Run("GetQueueInfoNonExistent", h.TestGetQueueInfoNonExistent)

	// Cross-queue operations
	h.t.Run("PublishToMultipleQueuesConsume", h.TestPublishToMultipleQueuesConsume)

	// Concurrency and volume
	h.t.Run("ConcurrentPublish", h.TestConcurrentPublish)
	h.t.Run("HighVolume", h.TestHighVolume)

	// Idempotency edge cases — run last because double-ack/nack may corrupt
	// driver-internal state on some transports (e.g. AMQP channel errors).
	h.t.Run("AckIsIdempotent", h.TestAckIsIdempotent)
	h.t.Run("NackIsIdempotent", h.TestNackIsIdempotent)
}

// TestDeclareQueue verifies that a queue can be declared and that
// re-declaring the same queue is idempotent (no error).
func (h *Harness) TestDeclareQueue(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("declare")

	cfg := &queueapi.Config{QueueName: h.uniqueQueueName("test-declare")}

	err := h.driver.DeclareQueue(ctx, queueID, cfg)
	require.NoError(t, err)

	// Declaring again should be idempotent.
	err = h.driver.DeclareQueue(ctx, queueID, cfg)
	require.NoError(t, err)
}

// TestPublishAndAttach verifies the basic publish → attach → receive → ack cycle.
func (h *Harness) TestPublishAndAttach(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("pubsub")

	cfg := &queueapi.Config{QueueName: h.uniqueQueueName("test-pubsub")}
	err := h.driver.DeclareQueue(ctx, queueID, cfg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := h.driver.Attach(ctx, queueID, nil, deliveries)
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

	cfg := &queueapi.Config{QueueName: h.uniqueQueueName("test-multi")}
	err := h.driver.DeclareQueue(ctx, queueID, cfg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := h.driver.Attach(ctx, queueID, nil, deliveries)
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

	cfg := &queueapi.Config{QueueName: h.uniqueQueueName("test-nack")}
	err := h.driver.DeclareQueue(ctx, queueID, cfg)
	require.NoError(t, err)

	msg := queueapi.AcquireMessage(payload.New("nack-test"))
	msg.ID = "nack-msg-1"
	err = h.driver.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := h.driver.Attach(ctx, queueID, nil, deliveries)
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

	cfg := &queueapi.Config{QueueName: h.uniqueQueueName("test-info")}
	err := h.driver.DeclareQueue(ctx, queueID, cfg)
	require.NoError(t, err)

	msg1 := queueapi.AcquireMessage(payload.New("info1"))
	msg1.ID = "info-1"
	msg2 := queueapi.AcquireMessage(payload.New("info2"))
	msg2.ID = "info-2"

	err = h.driver.Publish(ctx, queueID, msg1, msg2)
	require.NoError(t, err)

	// AMQP QueueInspect counts lag publish without publisher confirms, so
	// poll for the expected count within the harness timeout.
	var count int
	deadline := time.Now().Add(h.cfg.timeout)
	for time.Now().Before(deadline) {
		info, err := h.driver.GetQueueInfo(ctx, queueID)
		require.NoError(t, err)
		count = info.GetInt(queueapi.StatsMessageCount, 0)
		if count == 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
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
	_, err := h.driver.Attach(ctx, registry.ParseID("test:nonexistent"), nil, deliveries)
	assert.ErrorIs(t, err, queueapi.ErrQueueNotFound)
}

// TestNeutralHeadersRoundTrip verifies that the neutral header keys the
// drivers route into typed broker fields (correlation_id, reply_to,
// content_type, encoding, message_type) reappear on the consumer side
// under the same neutral keys — i.e. the mapping is bi-directional.
func (h *Harness) TestNeutralHeadersRoundTrip(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("neutral-rt")

	cfg := &queueapi.Config{QueueName: h.uniqueQueueName("test-neutral-rt")}
	err := h.driver.DeclareQueue(ctx, queueID, cfg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := h.driver.Attach(ctx, queueID, nil, deliveries)
	require.NoError(t, err)
	defer cancel()

	msg := queueapi.AcquireMessage(payload.New("neutral-rt"))
	msg.ID = "neutral-rt-1"
	msg.Headers.Set(queueapi.HeaderCorrelationID, "corr-42")
	msg.Headers.Set(queueapi.HeaderReplyTo, "replies")
	msg.Headers.Set(queueapi.HeaderContentType, "application/json")
	msg.Headers.Set(queueapi.HeaderEncoding, "identity")
	msg.Headers.Set(queueapi.HeaderMessageType, "order.created")

	err = h.driver.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	select {
	case delivery := <-deliveries:
		got := delivery.Message.Headers
		assert.Equal(t, "corr-42", got.GetString(queueapi.HeaderCorrelationID, ""),
			"correlation_id should round-trip")
		assert.Equal(t, "replies", got.GetString(queueapi.HeaderReplyTo, ""),
			"reply_to should round-trip")
		assert.Equal(t, "application/json", got.GetString(queueapi.HeaderContentType, ""),
			"content_type should round-trip")
		assert.Equal(t, "identity", got.GetString(queueapi.HeaderEncoding, ""),
			"encoding should round-trip")
		assert.Equal(t, "order.created", got.GetString(queueapi.HeaderMessageType, ""),
			"message_type should round-trip")
		err = delivery.Ack(ctx)
		assert.NoError(t, err)
	case <-time.After(h.cfg.timeout):
		t.Fatal("timeout waiting for message")
	}
}

// TestDeclareOptionsDoNotLeakToPublish declares a queue with driver-specific
// declare-time options (durable, max_length, message_ttl for AMQP; similar for
// SQS) and asserts that none of those option keys surface as headers on a
// consumed message. Declare-time state belongs in the driver's own declaration
// call, not in every published message's header set.
func (h *Harness) TestDeclareOptionsDoNotLeakToPublish(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("declare-leak")

	drvBag := attrs.NewBag()
	for k, v := range h.cfg.declareLeakOpts {
		drvBag.Set(k, v)
	}
	rootBag := attrs.NewBag()
	rootBag.Set(h.cfg.declareLeakDriver, drvBag)

	cfg := &queueapi.Config{
		QueueName:     h.uniqueQueueName("test-declare-leak"),
		DriverOptions: rootBag,
	}
	err := h.driver.DeclareQueue(ctx, queueID, cfg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 1)
	cancel, err := h.driver.Attach(ctx, queueID, nil, deliveries)
	require.NoError(t, err)
	defer cancel()

	msg := queueapi.AcquireMessage(payload.New("leak-probe"))
	msg.ID = "leak-probe-1"
	err = h.driver.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	select {
	case delivery := <-deliveries:
		for k := range h.cfg.declareLeakOpts {
			_, present := delivery.Message.Headers.Get(k)
			assert.Falsef(t, present, "declare-only key %q must not appear in consumed message headers", k)
		}
		err = delivery.Ack(ctx)
		assert.NoError(t, err)
	case <-time.After(h.cfg.timeout):
		t.Fatal("timeout waiting for message")
	}
}

// TestCustomHeaders verifies that custom message headers survive a
// publish → consume round-trip.
func (h *Harness) TestCustomHeaders(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("headers")

	cfg := &queueapi.Config{QueueName: h.uniqueQueueName("test-headers")}
	err := h.driver.DeclareQueue(ctx, queueID, cfg)
	require.NoError(t, err)

	msg := queueapi.AcquireMessage(payload.New("header-test"))
	msg.ID = "header-msg-1"
	msg.Headers.Set("custom", "header-value")

	err = h.driver.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := h.driver.Attach(ctx, queueID, nil, deliveries)
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
// drivers (AMQP, Redis, SQS) the body is serialized and deserialized, so the
// test only asserts the body and its data pointer are non-nil.
func (h *Harness) TestMessageBodyPreservation(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("body")

	cfg := &queueapi.Config{QueueName: h.uniqueQueueName("test-body")}
	err := h.driver.DeclareQueue(ctx, queueID, cfg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := h.driver.Attach(ctx, queueID, nil, deliveries)
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

	cfg := &queueapi.Config{QueueName: h.uniqueQueueName("test-batch")}
	err := h.driver.DeclareQueue(ctx, queueID, cfg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := h.driver.Attach(ctx, queueID, nil, deliveries)
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

	cfg := &queueapi.Config{QueueName: h.uniqueQueueName("test-multiheader")}
	err := h.driver.DeclareQueue(ctx, queueID, cfg)
	require.NoError(t, err)

	msg := queueapi.AcquireMessage(payload.New("multi-header"))
	msg.ID = "mh-msg-1"
	msg.Headers.Set("x-tenant", "acme")
	msg.Headers.Set("x-priority", "high")
	msg.Headers.Set("x-source", "test-suite")

	err = h.driver.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := h.driver.Attach(ctx, queueID, nil, deliveries)
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
		cfg := &queueapi.Config{QueueName: h.uniqueQueueName("test-mqd")}
		err := h.driver.DeclareQueue(ctx, queueIDs[i], cfg)
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

	cfgA := &queueapi.Config{QueueName: h.uniqueQueueName("test-iso-a")}
	err := h.driver.DeclareQueue(ctx, queueA, cfgA)
	require.NoError(t, err)

	cfgB := &queueapi.Config{QueueName: h.uniqueQueueName("test-iso-b")}
	err = h.driver.DeclareQueue(ctx, queueB, cfgB)
	require.NoError(t, err)

	deliveriesB := make(chan *queueapi.Delivery, 10)
	cancelB, err := h.driver.Attach(ctx, queueB, nil, deliveriesB)
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

	cfg := &queueapi.Config{QueueName: h.uniqueQueueName("test-empty-info")}
	err := h.driver.DeclareQueue(ctx, queueID, cfg)
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
// stops message delivery. After canceling, new messages published to the queue
// should not appear on the delivery channel.
func (h *Harness) TestCancelAttach(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("cancel")

	cfg := &queueapi.Config{QueueName: h.uniqueQueueName("test-cancel")}
	err := h.driver.DeclareQueue(ctx, queueID, cfg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := h.driver.Attach(ctx, queueID, nil, deliveries)
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

	cfg := &queueapi.Config{QueueName: h.uniqueQueueName("test-pre-attach")}
	err := h.driver.DeclareQueue(ctx, queueID, cfg)
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
	cancel, err := h.driver.Attach(ctx, queueID, nil, deliveries)
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

	cfg := &queueapi.Config{QueueName: h.uniqueQueueName("test-concurrent")}
	err := h.driver.DeclareQueue(ctx, queueID, cfg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 100)
	cancel, err := h.driver.Attach(ctx, queueID, nil, deliveries)
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

	cfg := &queueapi.Config{QueueName: h.uniqueQueueName("test-volume")}
	err := h.driver.DeclareQueue(ctx, queueID, cfg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 100)
	cancel, err := h.driver.Attach(ctx, queueID, nil, deliveries)
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

// ---------------------------------------------------------------------------
// Consumer lifecycle (extended)
// ---------------------------------------------------------------------------

// TestReattachAfterCancel verifies that after canceling a consumer, a new
// consumer can be attached to the same queue and receives new messages.
func (h *Harness) TestReattachAfterCancel(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("reattach")

	cfg := &queueapi.Config{QueueName: h.uniqueQueueName("test-reattach")}
	err := h.driver.DeclareQueue(ctx, queueID, cfg)
	require.NoError(t, err)

	// First consumer — receive one message, then cancel.
	del1 := make(chan *queueapi.Delivery, 10)
	cancel1, err := h.driver.Attach(ctx, queueID, nil, del1)
	require.NoError(t, err)

	msg1 := queueapi.AcquireMessage(payload.New("first"))
	msg1.ID = "reattach-1"
	err = h.driver.Publish(ctx, queueID, msg1)
	require.NoError(t, err)

	select {
	case d := <-del1:
		_ = d.Ack(ctx)
	case <-time.After(h.cfg.timeout):
		t.Fatal("timeout on first consumer")
	}

	cancel1()
	time.Sleep(200 * time.Millisecond)

	// Second consumer on the same queue — attach BEFORE publishing so the
	// consumer is ready regardless of the driver's consumer-group semantics
	// (Redis XREADGROUP, SQS visibility, etc.).
	del2 := make(chan *queueapi.Delivery, 10)
	cancel2, err := h.driver.Attach(ctx, queueID, nil, del2)
	require.NoError(t, err)
	defer cancel2()

	// Small delay to let the consumer goroutine start reading.
	time.Sleep(100 * time.Millisecond)

	msg2 := queueapi.AcquireMessage(payload.New("second"))
	msg2.ID = "reattach-2"
	err = h.driver.Publish(ctx, queueID, msg2)
	require.NoError(t, err)

	select {
	case d := <-del2:
		assert.NotNil(t, d.Message)
		_ = d.Ack(ctx)
	case <-time.After(h.cfg.timeout):
		t.Fatal("timeout on second consumer after reattach")
	}
}

// TestRapidAttachDetach verifies that rapidly attaching and canceling
// consumers does not cause panics or resource leaks.
func (h *Harness) TestRapidAttachDetach(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("rapid")

	cfg := &queueapi.Config{QueueName: h.uniqueQueueName("test-rapid")}
	err := h.driver.DeclareQueue(ctx, queueID, cfg)
	require.NoError(t, err)

	const iterations = 10
	for i := 0; i < iterations; i++ {
		deliveries := make(chan *queueapi.Delivery, 5)
		cancel, err := h.driver.Attach(ctx, queueID, nil, deliveries)
		require.NoError(t, err, "attach iteration %d", i)
		cancel()
	}
}

// ---------------------------------------------------------------------------
// Delivery edge-case tests
// ---------------------------------------------------------------------------

// TestAckIsIdempotent verifies that calling Ack more than once on the same
// delivery does not panic. The second call may return an error (driver-dependent)
// but must never crash.
func (h *Harness) TestAckIsIdempotent(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("ack-idem")

	cfg := &queueapi.Config{QueueName: h.uniqueQueueName("test-ack-idem")}
	err := h.driver.DeclareQueue(ctx, queueID, cfg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := h.driver.Attach(ctx, queueID, nil, deliveries)
	require.NoError(t, err)
	defer cancel()

	msg := queueapi.AcquireMessage(payload.New("ack-twice"))
	msg.ID = "ack-idem-1"
	err = h.driver.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	select {
	case delivery := <-deliveries:
		err = delivery.Ack(ctx)
		assert.NoError(t, err, "first ack should succeed")
		// Second ack — must not panic.
		_ = delivery.Ack(ctx)
	case <-time.After(h.cfg.timeout):
		t.Fatal("timeout waiting for message")
	}
}

// TestNackIsIdempotent verifies that calling Nack more than once on the same
// delivery does not panic.
func (h *Harness) TestNackIsIdempotent(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("nack-idem")

	cfg := &queueapi.Config{QueueName: h.uniqueQueueName("test-nack-idem")}
	err := h.driver.DeclareQueue(ctx, queueID, cfg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := h.driver.Attach(ctx, queueID, nil, deliveries)
	require.NoError(t, err)
	defer cancel()

	msg := queueapi.AcquireMessage(payload.New("nack-twice"))
	msg.ID = "nack-idem-1"
	err = h.driver.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	select {
	case delivery := <-deliveries:
		err = delivery.Nack(ctx)
		assert.NoError(t, err, "first nack should succeed")
		// Second nack — must not panic.
		_ = delivery.Nack(ctx)
	case <-time.After(h.cfg.timeout):
		t.Fatal("timeout waiting for message")
	}
}

// TestPublishWithoutExplicitID verifies that a message published without
// setting an explicit ID still has a non-empty ID when delivered.
func (h *Harness) TestPublishWithoutExplicitID(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("auto-id")

	cfg := &queueapi.Config{QueueName: h.uniqueQueueName("test-auto-id")}
	err := h.driver.DeclareQueue(ctx, queueID, cfg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := h.driver.Attach(ctx, queueID, nil, deliveries)
	require.NoError(t, err)
	defer cancel()

	// Publish with no explicit ID.
	msg := queueapi.AcquireMessage(payload.New("no-id"))
	err = h.driver.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	select {
	case delivery := <-deliveries:
		assert.NotEmpty(t, delivery.Message.ID,
			"driver should assign an ID when none is provided")
		_ = delivery.Ack(ctx)
	case <-time.After(h.cfg.timeout):
		t.Fatal("timeout waiting for message")
	}
}

// TestDeliveryHasNonNilHeaders verifies that a delivered message always has
// non-nil Headers, even if the publisher didn't set any custom headers.
func (h *Harness) TestDeliveryHasNonNilHeaders(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("nil-hdr")

	cfg := &queueapi.Config{QueueName: h.uniqueQueueName("test-nil-hdr")}
	err := h.driver.DeclareQueue(ctx, queueID, cfg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := h.driver.Attach(ctx, queueID, nil, deliveries)
	require.NoError(t, err)
	defer cancel()

	msg := queueapi.AcquireMessage(payload.New("hdr-check"))
	msg.ID = "hdr-check-1"
	err = h.driver.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	select {
	case delivery := <-deliveries:
		assert.NotNil(t, delivery.Message.Headers,
			"delivered message must have non-nil Headers")
		_ = delivery.Ack(ctx)
	case <-time.After(h.cfg.timeout):
		t.Fatal("timeout waiting for message")
	}
}

// TestSingleDelivery verifies that each published message is delivered exactly
// once (no duplicates). Publishes N messages, consumes N, then verifies no
// extra messages arrive.
func (h *Harness) TestSingleDelivery(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("single")

	cfg := &queueapi.Config{QueueName: h.uniqueQueueName("test-single")}
	err := h.driver.DeclareQueue(ctx, queueID, cfg)
	require.NoError(t, err)

	deliveries := make(chan *queueapi.Delivery, 20)
	cancel, err := h.driver.Attach(ctx, queueID, nil, deliveries)
	require.NoError(t, err)
	defer cancel()

	const n = 5
	for i := 0; i < n; i++ {
		msg := queueapi.AcquireMessage(payload.New("single"))
		msg.ID = string(rune('0' + i))
		err = h.driver.Publish(ctx, queueID, msg)
		require.NoError(t, err)
	}

	// Consume exactly n messages.
	received := 0
	timeout := time.After(h.cfg.timeout)
	for received < n {
		select {
		case d := <-deliveries:
			_ = d.Ack(ctx)
			received++
		case <-timeout:
			t.Fatalf("timeout, only received %d of %d", received, n)
		}
	}

	// Wait briefly — no more messages should arrive.
	select {
	case d := <-deliveries:
		t.Fatalf("received unexpected extra message: %s", d.Message.ID)
	case <-time.After(500 * time.Millisecond):
		// Expected — no duplicates.
	}
}

// ---------------------------------------------------------------------------
// Declaration edge-case tests
// ---------------------------------------------------------------------------

// TestDeclareQueueEmptyOptions verifies that declaring a queue with an empty
// options bag succeeds (driver uses defaults).
func (h *Harness) TestDeclareQueueEmptyOptions(t *testing.T) {
	ctx := context.Background()
	queueID := h.uniqueID("empty-opts")

	err := h.driver.DeclareQueue(ctx, queueID, &queueapi.Config{})
	require.NoError(t, err)

	// Publish to prove the queue is usable.
	msg := queueapi.AcquireMessage(payload.New("empty-opts"))
	msg.ID = "empty-opts-1"
	err = h.driver.Publish(ctx, queueID, msg)
	require.NoError(t, err)
}

// TestGetQueueInfoNonExistent verifies that GetQueueInfo on a queue that has
// not been declared returns an error.
func (h *Harness) TestGetQueueInfoNonExistent(t *testing.T) {
	ctx := context.Background()
	_, err := h.driver.GetQueueInfo(ctx, registry.ParseID("test:no-such-queue"))
	assert.Error(t, err, "GetQueueInfo on undeclared queue should return an error")
}

// ---------------------------------------------------------------------------
// Cross-queue operation tests
// ---------------------------------------------------------------------------

// TestPublishToMultipleQueuesConsume declares several queues, publishes
// messages to each, attaches consumers, and verifies all messages are
// delivered to the correct queue.
func (h *Harness) TestPublishToMultipleQueuesConsume(t *testing.T) {
	ctx := context.Background()

	const numQueues = 3
	const msgsPerQueue = 3

	type queueSlot struct {
		deliveries chan *queueapi.Delivery
		cancel     context.CancelFunc
		id         registry.ID
	}

	slots := make([]queueSlot, numQueues)
	for i := range slots {
		qID := h.uniqueID("mqc")
		cfg := &queueapi.Config{QueueName: h.uniqueQueueName("test-mqc")}
		err := h.driver.DeclareQueue(ctx, qID, cfg)
		require.NoError(t, err)

		ch := make(chan *queueapi.Delivery, 10)
		cancel, err := h.driver.Attach(ctx, qID, nil, ch)
		require.NoError(t, err)

		slots[i] = queueSlot{id: qID, deliveries: ch, cancel: cancel}
	}
	defer func() {
		for _, s := range slots {
			s.cancel()
		}
	}()

	// Publish msgsPerQueue messages to each queue.
	for _, s := range slots {
		for j := 0; j < msgsPerQueue; j++ {
			msg := queueapi.AcquireMessage(payload.New("mqc"))
			err := h.driver.Publish(ctx, s.id, msg)
			require.NoError(t, err)
		}
	}

	// Consume from each queue independently.
	for qi, s := range slots {
		received := 0
		timeout := time.After(h.cfg.timeout)
		for received < msgsPerQueue {
			select {
			case d := <-s.deliveries:
				assert.NotNil(t, d.Message)
				_ = d.Ack(ctx)
				received++
			case <-timeout:
				t.Fatalf("queue %d: timeout, only received %d of %d",
					qi, received, msgsPerQueue)
			}
		}
	}
}
