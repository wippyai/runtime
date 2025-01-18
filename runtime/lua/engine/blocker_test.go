package engine

import (
	"testing"
	"time"
)

type mockLayer struct{}

func (m *mockLayer) Step(cvm CVM, tasks ...*Task) ([]*Task, error) {
	return nil, nil
}

func TestBlockingState_Basic(t *testing.T) {
	notify := make(chan LayerState, 10)
	layer := &mockLayer{}
	state := NewBlocker(layer, notify)

	if state.IsBlocked() {
		t.Error("New state should not be blocked")
	}

	state.Add()
	if !state.IsBlocked() {
		t.Error("LayerState should be blocked after Add")
	}

	state.Done()
	if state.IsBlocked() {
		t.Error("LayerState should not be blocked after Done")
	}
}

func TestBlockingState_FlushState(t *testing.T) {
	notify := make(chan LayerState, 10)
	layer := &mockLayer{}
	state := NewBlocker(layer, notify)

	// Test flush with no blocking
	state.FlushState()
	select {
	case <-notify:
		t.Error("Should not notify when not blocked")
	default:
		// Expected - no notification
	}

	// Test flush with blocking
	state.Add()
	state.FlushState()
	select {
	case l := <-notify:
		if l.Layer != layer {
			t.Error("Wrong layer notified")
		}
	default:
		t.Error("Should notify when blocked")
	}

	// Test that multiple flushes don't send duplicate notifications
	state.FlushState()
	select {
	case <-notify:
		t.Error("Should not notify on second flush without state change")
	default:
		// Expected - no notification
	}
}

func TestBlockingState_NotificationDeduplication(t *testing.T) {
	notify := make(chan LayerState, 10)
	layer := &mockLayer{}
	state := NewBlocker(layer, notify)

	// Add and flush should notify once
	state.Add()
	state.FlushState()

	select {
	case <-notify:
		// Expected - got notification
	default:
		t.Error("Should get notification after Add + Flush")
	}

	// Done should notify because we were previously notified as blocked
	state.Done()
	select {
	case <-notify:
		// Expected - got unblock notification
	default:
		t.Error("Should get notification after Done when previously notified")
	}

	// Another Done should not notify
	state.Done()
	select {
	case <-notify:
		t.Error("Should not notify on Done when already unblocked")
	default:
		// Expected - no notification
	}
}

func TestBlockingState_EdgeCases(t *testing.T) {
	notify := make(chan LayerState, 10)
	layer := &mockLayer{}
	state := NewBlocker(layer, notify)

	// Test rapid Add/Done cycles without Flush
	for i := 0; i < 100; i++ {
		state.Add()
		state.Done()
	}
	select {
	case <-notify:
		t.Error("Should not get notifications without Flush")
	default:
		// Expected
	}

	// Test multiple Adds with single Done
	state.Add()
	state.Add()
	state.FlushState()
	state.Done()

	if !state.IsBlocked() {
		t.Error("Should still be blocked after single Done")
	}
}

func TestBlockingState_ZeroNotifyChannel(t *testing.T) {
	notify := make(chan LayerState) // Zero buffer
	layer := &mockLayer{}
	state := NewBlocker(layer, notify)

	// Create synchronization channel
	ready := make(chan struct{})
	done := make(chan bool)

	// Start goroutine to drain notify channel
	go func() {
		// Signal that we're ready to receive
		close(ready)
		// Wait for notification
		<-notify
		done <- true
	}()

	// Wait for goroutine to be ready
	<-ready

	state.Add()
	state.FlushState() // Should not block now that receiver is ready

	select {
	case <-done:
		// Expected - notification received
	case <-time.After(time.Second):
		t.Error("Notification send blocked")
	}
}
