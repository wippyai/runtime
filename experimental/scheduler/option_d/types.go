package option_d

import (
	"sync"
	"unsafe"

	"github.com/wippyai/runtime/api/relay"
)

// YieldResult represents completion of a yielded command.
// Embeds EventNode for zero-allocation intrusive list.
type YieldResult struct {
	EventNode
	Tag   any
	Data  any
	Error error
}

// Reset clears the result for reuse.
func (r *YieldResult) Reset() {
	r.EventNode.Reset()
	r.Tag = nil
	r.Data = nil
	r.Error = nil
}

// Message represents an incoming process message.
// Embeds EventNode for zero-allocation intrusive list.
type Message struct {
	EventNode
	From    relay.PID
	Payload any
}

// Reset clears the message for reuse.
func (m *Message) Reset() {
	m.EventNode.Reset()
	m.From = relay.PID{}
	m.Payload = nil
}

// Pre-allocated pools for event nodes.
var (
	yieldResultPool = sync.Pool{
		New: func() any {
			return &YieldResult{}
		},
	}

	messagePool = sync.Pool{
		New: func() any {
			return &Message{}
		},
	}
)

// AcquireYieldResult gets a YieldResult from pool.
func AcquireYieldResult() *YieldResult {
	r := yieldResultPool.Get().(*YieldResult)
	r.Reset()
	return r
}

// ReleaseYieldResult returns a YieldResult to pool.
func ReleaseYieldResult(r *YieldResult) {
	r.Reset()
	yieldResultPool.Put(r)
}

// AcquireMessage gets a Message from pool.
func AcquireMessage() *Message {
	m := messagePool.Get().(*Message)
	m.Reset()
	return m
}

// ReleaseMessage returns a Message to pool.
func ReleaseMessage(m *Message) {
	m.Reset()
	messagePool.Put(m)
}

// asEventNode converts YieldResult to EventNode pointer.
func asEventNode(r *YieldResult) *EventNode {
	return &r.EventNode
}

// asYieldResult converts EventNode to YieldResult pointer.
// Uses unsafe pointer arithmetic to recover the embedding struct.
func asYieldResult(n *EventNode) *YieldResult {
	if n == nil {
		return nil
	}
	// EventNode is embedded at offset 0 in YieldResult
	// So we can directly cast the pointer
	type wrapper struct {
		EventNode
		Tag   any
		Data  any
		Error error
	}
	return (*YieldResult)(unsafe.Pointer(n))
}

// asMessage converts EventNode to Message pointer.
// Uses unsafe pointer arithmetic to recover the embedding struct.
func asMessage(n *EventNode) *Message {
	if n == nil {
		return nil
	}
	// EventNode is embedded at offset 0 in Message
	type wrapper struct {
		EventNode
		From    relay.PID
		Payload any
	}
	return (*Message)(unsafe.Pointer(n))
}
