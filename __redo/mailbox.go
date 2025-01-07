package __redo

import (
	"context"
	"time"
)

// OverflowPolicy determines behavior when buffer is full
type OverflowPolicy int

const (
	PolicyDrop OverflowPolicy = iota
	PolicyBlock
	PolicyResize
)

// MailboxConfig holds mailbox configuration
type MailboxConfig struct {
	Buffer BufferConfig
	Policy OverflowPolicy
	// For PolicyBlock
	BlockTimeout time.Duration
	// opChan buffer sizes
	RecvChanSize uint64
}

// DefaultConfig returns default configuration
func DefaultConfig() MailboxConfig {
	return MailboxConfig{
		Buffer: BufferConfig{
			InitialSize:  1024,
			MaxSize:      1 << 20, // 1M messages
			GrowthFactor: 2.0,
			ResizeMode:   NoResize,
		},
		Policy:       PolicyDrop,
		BlockTimeout: time.Second * 5,
		RecvChanSize: 256,
	}
}

// Mailbox provides message queueing with policy-based overflow handling
type Mailbox struct {
	bufferMgr *BufferManager
	config    MailboxConfig
	notify    chan struct{} // Signals receivers when new messages arrive
}

// NewMailbox creates new mailbox with given config
func NewMailbox(config MailboxConfig) *Mailbox {
	// Set resize mode based on policy
	if config.Policy == PolicyResize {
		config.Buffer.ResizeMode = GrowOnFull
	}

	return &Mailbox{
		bufferMgr: NewBufferManager(config.Buffer),
		config:    config,
		notify:    make(chan struct{}, 1),
	}
}

// Send adds message using configured policy
func (m *Mailbox) Send(msg *Message) error {
	for {
		if m.bufferMgr.TryPush(msg) {
			// Signal receivers
			select {
			case m.notify <- struct{}{}:
			default:
			}
			return nil
		}

		// Handle overflow based on policy
		switch m.config.Policy {
		case PolicyDrop:
			return ErrBufferFull

		case PolicyBlock:
			// Wait with timeout
			select {
			case <-time.After(m.config.BlockTimeout):
				return ErrBufferFull
			case <-m.notify: // Space might be available
				continue
			}

		case PolicyResize:
			// Buffer manager handles resize
			continue
		}
	}
}

// Receive returns channel that yields messages matching pattern
func (m *Mailbox) Receive(ctx context.Context, pattern Pattern) <-chan *Message {
	ch := make(chan *Message, m.config.RecvChanSize)

	go func() {
		defer close(ch)

		for {
			// Try get message
			msg, err := m.bufferMgr.TryPop()
			if err == nil {
				if matchMessage(*pattern, *msg) {
					select {
					case ch <- msg:
						continue
					case <-ctx.Done():
						return
					}
				}
				// Non-matching message, try next
				continue
			}
			if err != ErrBufferEmpty {
				// Unexpected error, could log here
				continue
			}

			// No messages, wait for notification
			select {
			case <-m.notify:
				continue
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch
}

// ReceiveTimeout returns channel that yields messages with timeout
func (m *Mailbox) ReceiveTimeout(pattern Pattern, timeout time.Duration) <-chan *Message {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	ch := m.Receive(ctx, pattern)

	// Create wrapper channel to handle cleanup
	outCh := make(chan *Message, m.config.RecvChanSize)
	go func() {
		defer cancel()
		defer close(outCh)
		for msg := range ch {
			outCh <- msg
		}
	}()

	return outCh
}

// GetMetrics returns current mailbox metrics
func (m *Mailbox) GetMetrics() BufferMetrics {
	return m.bufferMgr.GetMetrics()
}

// Len returns current number of messages
func (m *Mailbox) Len() uint64 {
	return m.bufferMgr.GetBuffer().Len()
}

// Clear removes all messages
func (m *Mailbox) Clear() {
	m.bufferMgr.GetBuffer().Clear()
}
