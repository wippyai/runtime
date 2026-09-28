// SPDX-License-Identifier: MPL-2.0

package terminal

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInputEmitterCoalescesMotionAndPreservesDiscreteOrdering(t *testing.T) {
	var mu sync.Mutex
	var delivered []*TTYEvent
	emitter := newInputEmitter(func(event *TTYEvent) {
		copy := *event
		mu.Lock()
		delivered = append(delivered, &copy)
		mu.Unlock()
	})
	defer emitter.stop()
	emitter.emit(&TTYEvent{Type: "mouse", Action: "motion", X: 1})
	emitter.emit(&TTYEvent{Type: "mouse", Action: "motion", X: 9})
	emitter.emit(&TTYEvent{Type: "mouse", Action: "release", X: 9})
	mu.Lock()
	require.Len(t, delivered, 2)
	require.Equal(t, 9, delivered[0].X)
	require.Equal(t, "release", delivered[1].Action)
	mu.Unlock()
}

func TestInputEmitterHasNoIdleDeliveryAndStopsPendingMotion(t *testing.T) {
	delivered := make(chan *TTYEvent, 1)
	emitter := newInputEmitter(func(event *TTYEvent) { delivered <- event })
	emitter.emit(&TTYEvent{Type: "mouse", Action: "motion"})
	emitter.stop()
	emitter.emit(&TTYEvent{Type: "key", Key: "x"})
	select {
	case <-delivered:
		t.Fatal("motion delivered after emitter stopped")
	case <-time.After(2 * mouseMotionInterval):
	}
}
