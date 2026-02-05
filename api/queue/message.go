package queue

import (
	"sync"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
)

// HeaderTimestamp is a standard message header key
const (
	HeaderTimestamp     = "timestamp"      // Message creation timestamp
	HeaderPriority      = "priority"       // Message priority (0-9, higher = more important)
	HeaderTTL           = "ttl"            // Time to live in seconds
	HeaderCorrelationID = "correlation_id" // For request-response correlation
	HeaderReplyTo       = "reply_to"       // Queue name for responses
	HeaderContentType   = "content_type"   // MIME type of body
	HeaderMessageType   = "message_type"   // Application-specific message type

	// HeaderTraceparent is a W3C Trace Context header
	HeaderTraceparent = "traceparent" // W3C trace context
	HeaderTracestate  = "tracestate"  // W3C trace state

	// HeaderOriginalQueue is a dead letter queue header
	HeaderOriginalQueue    = "x_original_queue"     // Original queue name
	HeaderDeadLetterReason = "x_dead_letter_reason" // Why message was dead-lettered
	HeaderDeadLetterTime   = "x_dead_letter_time"   // When message was dead-lettered
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
