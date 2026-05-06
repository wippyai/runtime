// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"sync"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
)

// Neutral message headers — application metadata carried verbatim by
// every driver. Broker-specific concepts (priority, ttl, delay, partition
// key, dedup, etc.) live under driver-prefixed keys (e.g. "amqp.priority",
// "sqs.delay_seconds", "kafka.key") because their semantics don't
// cross-map cleanly.
const (
	HeaderTimestamp     = "timestamp"
	HeaderCorrelationID = "correlation_id"
	HeaderReplyTo       = "reply_to"
	HeaderContentType   = "content_type"
	HeaderMessageType   = "message_type"
	HeaderEncoding      = "encoding"
	HeaderSchema        = "schema"
	HeaderSource        = "source"

	// W3C Trace Context.
	HeaderTraceparent = "traceparent"
	HeaderTracestate  = "tracestate"

	// Dead-letter bookkeeping written by the manager layer.
	HeaderAttempts         = "attempts"
	HeaderOriginalQueue    = "x_original_queue"
	HeaderDeadLetterReason = "x_dead_letter_reason"
	HeaderDeadLetterTime   = "x_dead_letter_time"
)

// Message represents a queue message
type Message struct {
	Body    payload.Payload
	Headers attrs.Bag
	ID      string
}

var messagePool = sync.Pool{
	New: func() any {
		return &Message{
			Headers: attrs.NewBag(),
		}
	},
}

// AcquireMessage acquires a message from the pool and initializes it
func AcquireMessage(body payload.Payload) *Message {
	msg := messagePool.Get().(*Message)
	msg.Body = body
	if msg.Headers == nil {
		msg.Headers = attrs.NewBag()
	}

	msg.Headers.Set(HeaderTimestamp, time.Now().Unix())
	return msg
}

// AcquireMessageWithID acquires a message from the pool with a specific ID
func AcquireMessageWithID(id string, body payload.Payload) *Message {
	msg := AcquireMessage(body)
	msg.ID = id
	return msg
}

// ReleaseMessage returns a message to the pool after resetting it
func ReleaseMessage(msg *Message) {
	if msg == nil {
		return
	}
	msg.ID = ""
	msg.Body = nil
	// Clear headers but keep the bag allocated
	if msg.Headers != nil {
		clear(msg.Headers)
	}
	messagePool.Put(msg)
}

// NewMessage creates a new message with the given body (non-pooled version)
func NewMessage(body payload.Payload) *Message {
	headers := attrs.NewBag()
	headers.Set(HeaderTimestamp, time.Now().Unix())
	return &Message{
		Body:    body,
		Headers: headers,
	}
}

// NewMessageWithID creates a new message with a specific ID (non-pooled version)
func NewMessageWithID(id string, body payload.Payload) *Message {
	msg := NewMessage(body)
	msg.ID = id
	return msg
}

// CloneMessage returns a non-pooled copy of msg that is safe to keep after
// the original message is released back to the pool.
func CloneMessage(msg *Message) *Message {
	if msg == nil {
		return nil
	}
	headers := attrs.NewBag()
	for k, v := range msg.Headers {
		headers.Set(k, v)
	}
	return &Message{
		ID:      msg.ID,
		Body:    msg.Body,
		Headers: headers,
	}
}
