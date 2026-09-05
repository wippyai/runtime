// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"unicode/utf8"

	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
)

var (
	ErrClosed             = apierror.New(apierror.Canceled, "closed").WithRetryable(apierror.False)
	ErrOverloaded         = apierror.New(apierror.Unavailable, "overloaded").WithRetryable(apierror.True)
	ErrTooLarge           = apierror.New(apierror.Invalid, "too-large").WithRetryable(apierror.False)
	ErrInvalidMessage     = apierror.New(apierror.Invalid, "invalid-message").WithRetryable(apierror.False)
	ErrUnsupportedPayload = apierror.New(apierror.Invalid, "unsupported-payload").WithRetryable(apierror.False)
	ErrActorRequired      = errors.New("actor-context-required")
)

const (
	MaxTopicBytes   = 256
	MaxPIDBytes     = 1024
	MaxPayloads     = 16
	messageOverhead = 256
)

// Limits bound both scheduler ingress and messages already delivered to the
// guest inbox. They are host-owned and cannot be enlarged by guest arguments.
type Limits struct {
	Capacity     int
	Bytes        int64
	MessageBytes int64
}

func DefaultLimits() Limits {
	return Limits{Capacity: 128, Bytes: 8 << 20, MessageBytes: 1 << 20}
}

// Payload uses stable wire formats, not runtime-specific value handles.
type Payload struct {
	Format string
	Data   []byte
}

type Message struct {
	From     string
	Topic    string
	Payloads []Payload
}

type queuedMessage struct {
	message Message
	charge  int64
}

type Mailbox struct {
	mu     sync.Mutex
	limits Limits
	inbox  []queuedMessage
	head   int
	length int
	count  int
	bytes  int64
	closed bool
}

// NewMailbox fails closed for invalid limits. Configuration loaders should
// validate these values before creating an actor.
func NewMailbox(limits Limits) *Mailbox {
	if limits.Capacity <= 0 || limits.Bytes <= 0 || limits.MessageBytes <= 0 || limits.MessageBytes > limits.Bytes || int64(limits.Capacity) > limits.Bytes/int64(messageOverhead) {
		return &Mailbox{closed: true}
	}
	return &Mailbox{limits: limits}
}

type mailboxKey struct{}

func WithMailbox(ctx context.Context, mailbox *Mailbox) context.Context {
	return context.WithValue(ctx, mailboxKey{}, mailbox)
}

func GetMailbox(ctx context.Context) *Mailbox {
	if ctx == nil {
		return nil
	}
	mailbox, _ := ctx.Value(mailboxKey{}).(*Mailbox)
	return mailbox
}

// delivery owns admission charges until it is handed to the inbox or discarded.
type delivery struct {
	mailbox  *Mailbox
	messages []queuedMessage
}

func (d *delivery) DiscardEvent() {
	if d == nil {
		return
	}
	d.mailbox.mu.Lock()
	defer d.mailbox.mu.Unlock()
	for i := range d.messages {
		d.mailbox.count--
		d.mailbox.bytes -= d.messages[i].charge
		d.messages[i] = queuedMessage{}
	}
	d.messages = nil
}

// AdmitEvent executes under the scheduler queue's generation lock. No raw
// package or mutable payload storage is retained after a successful admission.
func (m *Mailbox) AdmitEvent(event process.Event) (process.Event, error) {
	pkg, ok := event.Data.(*relay.Package)
	if !ok || pkg == nil {
		return event, nil
	}
	// Lifecycle messages originate from the runtime's reserved system sender.
	// Keep the existing event contract; user messages cannot spoof this sender.
	if pkg.Source == topology.SystemPID {
		return event, nil
	}
	if len(pkg.Messages) == 0 || len(pkg.Messages) > m.limits.Capacity {
		return event, ErrTooLarge
	}
	from := pkg.Source.String()
	if len(from) > MaxPIDBytes || !utf8.ValidString(from) {
		return event, ErrInvalidMessage
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return event, ErrClosed
	}
	if len(pkg.Messages) > m.limits.Capacity-m.count {
		return event, ErrOverloaded
	}
	// Preflight every payload before copying any data or accepting any part of a
	// batch. Only already encoded payloads (plus immutable Lua strings) cross ABI.
	var total int64
	var inlineCharges [8]int64
	charges := inlineCharges[:]
	if len(pkg.Messages) > len(charges) {
		charges = make([]int64, len(pkg.Messages))
	}
	for i, msg := range pkg.Messages {
		n, err := messageSize(from, msg, m.limits.MessageBytes)
		if err != nil {
			return event, err
		}
		if n > m.limits.Bytes-m.bytes-total {
			return event, ErrOverloaded
		}
		charges[i] = n
		total += n
	}
	d := &delivery{mailbox: m, messages: make([]queuedMessage, len(pkg.Messages))}
	for i, msg := range pkg.Messages {
		out := Message{From: strings.Clone(from), Topic: strings.Clone(msg.Topic), Payloads: make([]Payload, len(msg.Payloads))}
		for j, input := range msg.Payloads {
			format, data, str, _ := encodedPayload(input)
			if data != nil {
				out.Payloads[j] = Payload{Format: format, Data: append([]byte(nil), data...)}
			} else {
				out.Payloads[j] = Payload{Format: format, Data: []byte(str)}
			}
		}
		d.messages[i] = queuedMessage{message: out, charge: charges[i]}
	}
	m.count += len(d.messages)
	m.bytes += total
	relay.ReleasePackage(pkg)
	event.Data = d
	return event, nil
}

func messageSize(from string, msg *relay.Message, limit int64) (int64, error) {
	if msg == nil || len(msg.Topic) == 0 || len(msg.Topic) > MaxTopicBytes || strings.HasPrefix(msg.Topic, "@") || !utf8.ValidString(msg.Topic) || len(msg.Payloads) > MaxPayloads {
		return 0, ErrInvalidMessage
	}
	n := int64(messageOverhead + len(from) + len(msg.Topic) + len(msg.Payloads)*64)
	if n > limit {
		return 0, ErrTooLarge
	}
	for _, input := range msg.Payloads {
		format, data, str, err := encodedPayload(input)
		if err != nil {
			return 0, err
		}
		size := len(data)
		if data == nil {
			size = len(str)
		}
		if int64(size) > limit-n {
			return 0, ErrTooLarge
		}
		n += int64(size)
		if format == "text" && ((data != nil && !utf8.Valid(data)) || (data == nil && !utf8.ValidString(str))) {
			return 0, ErrInvalidMessage
		}
		if format == "json" {
			if data == nil {
				data = []byte(str)
			}
			if !utf8.Valid(data) || !json.Valid(data) {
				return 0, ErrInvalidMessage
			}
		}
	}
	return n, nil
}

func encodedPayload(input payload.Payload) (format string, data []byte, str string, err error) {
	if input == nil {
		return "", nil, "", ErrUnsupportedPayload
	}
	switch input.Format() {
	case payload.Bytes:
		format = "bytes"
	case payload.String:
		format = "text"
	case payload.JSON:
		format = "json"
	case payload.Lua:
		// Lua exports immutable LString values in a Lua-format envelope. Do not
		// stringify tables/userdata or invoke user-controlled conversion methods.
		v := reflect.ValueOf(input.Data())
		if v.IsValid() && v.Kind() == reflect.String {
			return "bytes", nil, v.String(), nil
		}
		return "", nil, "", ErrUnsupportedPayload
	default:
		return "", nil, "", ErrUnsupportedPayload
	}
	switch v := input.Data().(type) {
	case []byte:
		return format, v, "", nil
	case string:
		return format, nil, v, nil
	default:
		return "", nil, "", ErrUnsupportedPayload
	}
}

// Deliver adopts a charged scheduler event into the inbox. It runs on the
// actor's sole execution lane; admission may run concurrently on senders.
func (m *Mailbox) Deliver(event process.Event) bool {
	d, ok := event.Data.(*delivery)
	if !ok {
		return false
	}
	if d.mailbox != m {
		d.DiscardEvent()
		return true
	}
	m.mu.Lock()
	if !m.closed {
		if m.inbox == nil {
			m.inbox = make([]queuedMessage, m.limits.Capacity)
		}
		for i := range d.messages {
			m.inbox[(m.head+m.length)%len(m.inbox)] = d.messages[i]
			m.length++
			d.messages[i] = queuedMessage{}
		}
		d.messages = nil
		m.mu.Unlock()
	} else {
		m.mu.Unlock()
		d.DiscardEvent()
	}
	return true
}

func (m *Mailbox) Take() (*Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrClosed
	}
	if m.length == 0 {
		return nil, nil
	}
	msg := m.inbox[m.head]
	m.inbox[m.head] = queuedMessage{}
	m.head = (m.head + 1) % len(m.inbox)
	m.length--
	m.count--
	m.bytes -= msg.charge
	return &msg.message, nil
}

func (m *Mailbox) Ready() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed || m.length != 0
}

func (m *Mailbox) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	for m.length > 0 {
		i := m.head
		m.head = (m.head + 1) % len(m.inbox)
		m.length--
		m.count--
		m.bytes -= m.inbox[i].charge
		m.inbox[i] = queuedMessage{}
	}
	m.inbox = nil
}
