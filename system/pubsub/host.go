package pubsub

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/pubsub"
	"sync"
)

// Host implements a local pubsub for a single host.
type Host struct {
	mu        sync.RWMutex
	receivers map[string]pubsub.Receiver
}

// NewHostPubSub returns a new Host instance.
func NewHostPubSub() *Host {
	return &Host{
		receivers: make(map[string]pubsub.Receiver),
	}
}

// Attach attaches a receiver to a PID.
// If a receiver is already attached, it returns pubsub.ErrAlreadyAttached.
func (h *Host) Attach(pid pubsub.PID, receiver pubsub.Receiver) (error, context.CancelFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()

	key := pid.String()
	if _, exists := h.receivers[key]; exists {
		return pubsub.ErrAlreadyAttached, nil
	}
	h.receivers[key] = receiver
	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.receivers, key)
	}

	return nil, cancel
}

// Send sends the message(s) to the attached receiver.
func (h *Host) Send(ctx context.Context, pid pubsub.PID, msgs ...*pubsub.Message) error {
	h.mu.RLock()
	receiver, exists := h.receivers[pid.String()]
	h.mu.RUnlock()
	if !exists {
		return fmt.Errorf("no receiver attached for pid %s", pid.String())
	}
	return receiver.Send(msgs...)
}
