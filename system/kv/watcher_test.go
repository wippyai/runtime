// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"github.com/wippyai/runtime/system/eventbus"
)

func TestWatcherCloseJoinsBlockedDelivery(t *testing.T) {
	bus := eventbus.NewBus()
	defer bus.Stop()
	w, err := newWatcher(context.Background(), bus, event.System("test"), "")
	require.NoError(t, err)
	defer w.Close()
	// Fill the public buffer without consuming it. Further input leaves delivery
	// blocked; Close must release both delivery stages and close the output.
	for i := 0; i < 256; i++ {
		bus.Send(context.Background(), event.Event{System: event.System("test"), Kind: "key", Data: kvapi.WatchEvent{Type: kvapi.WatchPut}})
	}
	require.Eventually(t, func() bool { return len(w.events) == cap(w.events) }, time.Second, time.Millisecond)
	var callers sync.WaitGroup
	for i := 0; i < 8; i++ {
		callers.Add(1)
		go func() {
			defer callers.Done()
			_ = w.Close()
			select {
			case <-w.done:
			default:
				t.Error("Close returned before delivery exited")
			}
		}()
	}
	joined := make(chan struct{})
	go func() { callers.Wait(); close(joined) }()
	select {
	case <-joined:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent Close did not join blocked delivery")
	}
	// Buffered events may remain, but no sender or open channel may remain.
	for i := 0; i <= cap(w.events); i++ {
		select {
		case _, open := <-w.Events():
			if !open {
				return
			}
		default:
			t.Fatal("output channel remains open after Close")
		}
	}
	t.Fatal("output did not terminate")
}
