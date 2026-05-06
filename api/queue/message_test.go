// SPDX-License-Identifier: MPL-2.0

package queue_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/queue"
)

func TestNewMessage(t *testing.T) {
	body := payload.New("test message")
	msg := queue.NewMessage(body)

	assert.NotNil(t, msg)
	assert.Empty(t, msg.ID) // ID is driver-specific, not set by default
	assert.Equal(t, body, msg.Body)
	assert.NotNil(t, msg.Headers)

	// Check timestamp was set
	ts, ok := msg.Headers.Get(queue.HeaderTimestamp)
	assert.True(t, ok)
	assert.IsType(t, int64(0), ts)
	assert.Greater(t, ts.(int64), int64(0))
}

func TestNewMessageWithID(t *testing.T) {
	body := payload.New("test message")
	id := "test-id-123"
	msg := queue.NewMessageWithID(id, body)

	assert.NotNil(t, msg)
	assert.Equal(t, id, msg.ID)
	assert.Equal(t, body, msg.Body)
	assert.NotNil(t, msg.Headers)

	// Check timestamp was set
	ts, ok := msg.Headers.Get(queue.HeaderTimestamp)
	assert.True(t, ok)
	assert.IsType(t, int64(0), ts)
}

func TestMessagePool(t *testing.T) {
	t.Run("AcquireMessage", func(t *testing.T) {
		body := payload.New("pooled message")
		msg := queue.AcquireMessage(body)

		assert.NotNil(t, msg)
		assert.Empty(t, msg.ID)
		assert.Equal(t, body, msg.Body)
		assert.NotNil(t, msg.Headers)

		// Check timestamp
		ts, ok := msg.Headers.Get(queue.HeaderTimestamp)
		assert.True(t, ok)
		assert.Greater(t, ts.(int64), int64(0))

		// Clean up
		queue.ReleaseMessage(msg)
	})

	t.Run("AcquireMessageWithID", func(t *testing.T) {
		body := payload.New("pooled message")
		id := "pooled-id-456"
		msg := queue.AcquireMessageWithID(id, body)

		assert.NotNil(t, msg)
		assert.Equal(t, id, msg.ID)
		assert.Equal(t, body, msg.Body)
		assert.NotNil(t, msg.Headers)

		// Clean up
		queue.ReleaseMessage(msg)
	})

	t.Run("ReleaseMessage", func(t *testing.T) {
		body := payload.New("test")
		msg := queue.AcquireMessageWithID("test-id", body)

		// Add some headers
		msg.Headers.Set("custom", "value")
		msg.Headers.Set("amqp.priority", 5)

		// Release the message
		queue.ReleaseMessage(msg)

		// The message should be reset
		assert.Empty(t, msg.ID)
		assert.Nil(t, msg.Body)
		assert.NotNil(t, msg.Headers) // Headers bag is kept but cleared

		// Headers should be cleared
		_, ok := msg.Headers.Get("custom")
		assert.False(t, ok)
		_, ok = msg.Headers.Get("amqp.priority")
		assert.False(t, ok)
	})

	t.Run("ReleaseNilMessage", func(_ *testing.T) {
		// Should not panic
		queue.ReleaseMessage(nil)
	})

	t.Run("PoolReuse", func(t *testing.T) {
		// Acquire and release multiple messages
		for i := 0; i < 10; i++ {
			body := payload.New("test")
			msg := queue.AcquireMessage(body)
			msg.Headers.Set("iteration", i)
			queue.ReleaseMessage(msg)
		}

		// Acquire a new message - should be from pool
		msg := queue.AcquireMessage(payload.New("reused"))
		assert.NotNil(t, msg)
		assert.NotNil(t, msg.Headers)

		// Should have fresh timestamp
		ts, ok := msg.Headers.Get(queue.HeaderTimestamp)
		assert.True(t, ok)
		assert.Greater(t, ts.(int64), int64(0))

		// Should not have old data
		_, ok = msg.Headers.Get("iteration")
		assert.False(t, ok)

		queue.ReleaseMessage(msg)
	})
}

func TestMessageHeaders(t *testing.T) {
	msg := queue.NewMessage(payload.New("test"))

	// Driver-specific keys live under driver-prefixed namespaces.
	msg.Headers.Set("amqp.priority", 5)
	msg.Headers.Set("amqp.expiration", "3600")
	msg.Headers.Set(queue.HeaderCorrelationID, "corr-123")
	msg.Headers.Set(queue.HeaderReplyTo, "reply-queue")
	msg.Headers.Set(queue.HeaderContentType, "application/json")
	msg.Headers.Set(queue.HeaderMessageType, "order.created")

	// W3C trace headers
	msg.Headers.Set(queue.HeaderTraceparent, "00-trace-id-span-id-01")
	msg.Headers.Set(queue.HeaderTracestate, "vendor=value")

	// Dead letter headers
	msg.Headers.Set(queue.HeaderOriginalQueue, "original-queue")
	msg.Headers.Set(queue.HeaderDeadLetterReason, "max retries exceeded")
	msg.Headers.Set(queue.HeaderDeadLetterTime, time.Now().Unix())

	// Verify all headers
	assert.Equal(t, 5, msg.Headers.GetInt("amqp.priority", 0))
	assert.Equal(t, "3600", msg.Headers.GetString("amqp.expiration", ""))
	assert.Equal(t, "corr-123", msg.Headers.GetString(queue.HeaderCorrelationID, ""))
	assert.Equal(t, "reply-queue", msg.Headers.GetString(queue.HeaderReplyTo, ""))
	assert.Equal(t, "application/json", msg.Headers.GetString(queue.HeaderContentType, ""))
	assert.Equal(t, "order.created", msg.Headers.GetString(queue.HeaderMessageType, ""))
	assert.Equal(t, "00-trace-id-span-id-01", msg.Headers.GetString(queue.HeaderTraceparent, ""))
	assert.Equal(t, "vendor=value", msg.Headers.GetString(queue.HeaderTracestate, ""))
	assert.Equal(t, "original-queue", msg.Headers.GetString(queue.HeaderOriginalQueue, ""))
	assert.Equal(t, "max retries exceeded", msg.Headers.GetString(queue.HeaderDeadLetterReason, ""))
}

func TestMessageWithCustomHeaders(t *testing.T) {
	msg := queue.NewMessage(payload.New("test"))

	// Create custom headers
	customHeaders := attrs.NewBag()
	customHeaders.Set("user_id", "user-123")
	customHeaders.Set("tenant_id", "tenant-456")
	customHeaders.Set("request_id", "req-789")

	// Copy custom headers to message
	for k, v := range customHeaders {
		msg.Headers.Set(k, v)
	}

	// Verify custom headers
	assert.Equal(t, "user-123", msg.Headers.GetString("user_id", ""))
	assert.Equal(t, "tenant-456", msg.Headers.GetString("tenant_id", ""))
	assert.Equal(t, "req-789", msg.Headers.GetString("request_id", ""))

	// Timestamp should still be set
	_, ok := msg.Headers.Get(queue.HeaderTimestamp)
	assert.True(t, ok)
}

func TestCloneMessage(t *testing.T) {
	t.Run("Nil", func(t *testing.T) {
		assert.Nil(t, queue.CloneMessage(nil))
	})

	t.Run("CopiesIDBodyAndHeaders", func(t *testing.T) {
		body := payload.New("clone-body")
		msg := queue.NewMessageWithID("msg-1", body)
		msg.Headers.Set("job_id", "job-1")
		msg.Headers.Set("attempt", 2)

		clone := queue.CloneMessage(msg)

		assert.NotSame(t, msg, clone)
		assert.Equal(t, "msg-1", clone.ID)
		assert.Equal(t, body, clone.Body)
		assert.Equal(t, "job-1", clone.Headers.GetString("job_id", ""))
		assert.Equal(t, 2, clone.Headers.GetInt("attempt", 0))
	})

	t.Run("HeaderBagIsIndependent", func(t *testing.T) {
		msg := queue.NewMessageWithID("msg-1", payload.New("clone-body"))
		msg.Headers.Set("job_id", "job-1")

		clone := queue.CloneMessage(msg)
		msg.Headers.Set("job_id", "mutated")
		msg.Headers.Set("new_header", "new")
		clone.Headers.Set("clone_only", "yes")

		assert.Equal(t, "job-1", clone.Headers.GetString("job_id", ""))
		assert.Equal(t, "", clone.Headers.GetString("new_header", ""))
		assert.Equal(t, "", msg.Headers.GetString("clone_only", ""))
	})

	t.Run("SurvivesOriginalRelease", func(t *testing.T) {
		msg := queue.AcquireMessageWithID("pooled-1", payload.New("pooled-body"))
		msg.Headers.Set("job_id", "job-1")

		clone := queue.CloneMessage(msg)
		queue.ReleaseMessage(msg)

		assert.Equal(t, "pooled-1", clone.ID)
		assert.NotNil(t, clone.Body)
		assert.Equal(t, "pooled-body", clone.Body.Data())
		assert.Equal(t, "job-1", clone.Headers.GetString("job_id", ""))
	})

	t.Run("NilHeadersBecomeEmptyBag", func(t *testing.T) {
		msg := &queue.Message{ID: "manual", Body: payload.New("body")}

		clone := queue.CloneMessage(msg)

		assert.NotNil(t, clone.Headers)
		assert.Equal(t, "manual", clone.ID)
		assert.Equal(t, "body", clone.Body.Data())
	})
}

func BenchmarkNewMessage(b *testing.B) {
	body := payload.New("benchmark message")
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		msg := queue.NewMessage(body)
		_ = msg
	}
}

func BenchmarkMessagePool(b *testing.B) {
	body := payload.New("benchmark message")
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		msg := queue.AcquireMessage(body)
		queue.ReleaseMessage(msg)
	}
}

func BenchmarkMessageWithHeaders(b *testing.B) {
	body := payload.New("benchmark message")
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		msg := queue.AcquireMessage(body)
		msg.Headers.Set("amqp.priority", 5)
		msg.Headers.Set(queue.HeaderCorrelationID, "corr-123")
		msg.Headers.Set(queue.HeaderTraceparent, "trace-123")
		queue.ReleaseMessage(msg)
	}
}
