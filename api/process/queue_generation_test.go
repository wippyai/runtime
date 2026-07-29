// SPDX-License-Identifier: MPL-2.0

package process

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type boundaryWakeRecorder struct {
	queue *EventQueue
	calls int
	gen   uint64
}

func (r *boundaryWakeRecorder) WakeProcessor(queue *EventQueue, gen uint64) {
	r.calls++
	r.queue = queue
	r.gen = gen
}

func TestA03StaleCompletionDoesNotEnqueue(t *testing.T) {
	queue := NewEventQueue()
	wake := &boundaryWakeRecorder{}
	stale := queue.NewYieldCompleter(wake)
	queue.Reset()

	stale.CompleteYield(41, "stale-data", errors.New("stale-error"))

	assert.False(t, queue.HasEvents())
	assert.Nil(t, queue.Drain())
	assert.Zero(t, wake.calls)
	select {
	case <-queue.Signal():
		t.Fatal("stale completion signaled the queue")
	default:
	}
}

func TestA04FreshCompletionEnqueuesAndSignals(t *testing.T) {
	queue := NewEventQueue()
	wake := &boundaryWakeRecorder{}
	completer := queue.NewYieldCompleter(wake)
	completionErr := errors.New("literal completion failure")

	completer.CompleteYield(73, "literal completion data", completionErr)

	require.Equal(t, 1, wake.calls)
	assert.Same(t, queue, wake.queue)
	assert.Equal(t, queue.Generation(), wake.gen)
	events := queue.Drain()
	require.Len(t, events, 1)
	assert.Equal(t, EventYieldComplete, events[0].Type)
	assert.Equal(t, uint64(73), events[0].Tag)
	assert.Equal(t, "literal completion data", events[0].Data)
	assert.Same(t, completionErr, events[0].Error)
	select {
	case <-queue.Signal():
	default:
		t.Fatal("fresh completion did not signal the queue")
	}
	select {
	case <-queue.Signal():
		t.Fatal("fresh completion signaled more than once")
	default:
	}
}

func TestA05ResetDrainsStaleSignal(t *testing.T) {
	queue := NewEventQueue()
	staleGeneration := queue.Generation()
	queue.Close()

	queue.Reset()
	select {
	case <-queue.Signal():
		t.Fatal("reset retained the close signal")
	default:
	}
	assert.False(t, queue.Push(Event{Data: "stale"}, staleGeneration))
	select {
	case <-queue.Signal():
		t.Fatal("stale push signaled the reset queue")
	default:
	}

	freshGeneration := queue.Generation()
	require.True(t, queue.Push(Event{Data: "fresh"}, freshGeneration))
	select {
	case <-queue.Signal():
	default:
		t.Fatal("fresh push did not signal the reset queue")
	}
	select {
	case <-queue.Signal():
		t.Fatal("fresh push signaled more than once")
	default:
	}
}
