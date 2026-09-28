// SPDX-License-Identifier: MPL-2.0

package poll

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/actor"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

// orderedSocketPollable models the notification behavior used by socket
// pollables. The optional gate lets the test place mailbox delivery before the
// mailbox's Notify call without racing the test driver itself.
type orderedSocketPollable struct {
	signal  chan struct{}
	entered chan struct{}
	release <-chan struct{}
	ready   atomic.Bool
}

func (*orderedSocketPollable) Type() preview2.ResourceType { return preview2.ResourcePollable }
func (*orderedSocketPollable) Drop()                       {}
func (p *orderedSocketPollable) Ready() bool               { return p.ready.Load() }
func (*orderedSocketPollable) Block(context.Context)       { panic("waitSources must not call Block") }
func (p *orderedSocketPollable) Notify() <-chan struct{} {
	select {
	case p.entered <- struct{}{}:
	default:
	}
	if p.release != nil {
		<-p.release
	}
	return p.signal
}
func (p *orderedSocketPollable) fire() {
	if p.ready.CompareAndSwap(false, true) {
		close(p.signal)
	}
}

func simultaneousMailboxEvent() process.Event {
	return process.Event{
		Type: process.EventMessage,
		Data: relay.NewPackage(
			pid.PID{Node: "local", Host: "actors", UniqID: "sender"},
			pid.PID{}, "mailbox", payload.NewPayload([]byte("x"), payload.Bytes),
		),
	}
}

func deliverSimultaneousMailbox(t testing.TB, mailbox *actor.Mailbox) {
	t.Helper()
	event := simultaneousMailboxEvent()
	admitted, err := mailbox.AdmitEvent(event)
	require.NoError(t, err)
	require.True(t, mailbox.Deliver(admitted))
}

func TestPollWaitReturnsBothMailboxAndSocketIndexesAcrossNotifyOrders(t *testing.T) {
	for _, tc := range []struct {
		name        string
		socketFirst bool
	}{
		{name: "mailbox-notify-before-delivery", socketFirst: false},
		{name: "mailbox-delivery-before-notify", socketFirst: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mailbox := actor.NewMailbox(actor.Limits{Capacity: 1, Bytes: 4096, MessageBytes: 1024})
			defer mailbox.Close()
			releaseSocketNotify := make(chan struct{})
			var releaseOnce sync.Once
			releaseGate := func() { releaseOnce.Do(func() { close(releaseSocketNotify) }) }
			socket := &orderedSocketPollable{
				signal:  make(chan struct{}),
				entered: make(chan struct{}, 1),
				release: releaseSocketNotify,
			}
			var sources []preview2.Pollable
			if tc.socketFirst {
				// waitSources calls socket.Notify before mailbox.Notify in this
				// order. Gate it to make the delivery-before-registration case
				// deterministic for the actual mailbox pollable.
				sources = []preview2.Pollable{socket, mailbox.Subscribe()}
			} else {
				// Seeing socket.Notify means mailbox.Notify has already returned;
				// the gate keeps both readiness changes ahead of the recheck.
				sources = []preview2.Pollable{mailbox.Subscribe(), socket}
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			result := make(chan []uint32, 1)
			done := make(chan struct{})
			go func() {
				defer close(done)
				indexes, err := (&waitSources{sources: sources}).wait(ctx)
				if err != nil {
					indexes = nil
				}
				result <- indexes
			}()
			t.Cleanup(func() {
				releaseGate()
				cancel()
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Error("mixed wait did not exit during cleanup")
				}
			})

			select {
			case <-socket.entered:
			case <-ctx.Done():
				t.Fatal("mixed wait did not reach socket notification")
			}

			deliverSimultaneousMailbox(t, mailbox)
			socket.fire()
			releaseGate()

			select {
			case indexes := <-result:
				require.Equal(t, []uint32{0, 1}, indexes)
			case <-ctx.Done():
				t.Fatal("mixed wait did not observe both ready sources")
			}

			// Returning a mailbox index must be observation only. The guest
			// still owns the explicit dequeue after poll returns.
			require.True(t, mailbox.Ready())
			message, err := mailbox.Take()
			require.NoError(t, err)
			require.NotNil(t, message)
			require.Equal(t, "mailbox", message.Topic)
			require.False(t, mailbox.Ready())
		})
	}
}
