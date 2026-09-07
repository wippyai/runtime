// SPDX-License-Identifier: MPL-2.0

package process

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

func boundedPackage(topic string, items int, bytes int64, value string) *relay.Package {
	pkg := relay.NewPackage(pid.PID{}, pid.PID{}, topic, payload.NewString(value))
	pkg.Messages[0].MaxItems = items
	pkg.Messages[0].MaxBytes = bytes
	pkg.Messages[0].PayloadBytes = bytes
	return pkg
}

func TestEventQueueMessageAdmissionEmitsOneTerminal(t *testing.T) {
	q := NewEventQueue()
	gen := q.Generation()

	first := boundedPackage("cdc:a", 1, 100, "first")
	require.Equal(t, MessageAccepted, q.PushMessage(Event{Type: EventMessage, Data: first}, gen))

	second := boundedPackage("cdc:a", 1, 100, "second")
	require.Equal(t, MessageAccepted, q.PushMessage(Event{Type: EventMessage, Data: second}, gen), "overflow terminal remains admissible")
	require.Len(t, second.Messages, 1)
	require.True(t, payload.IsTerminal(second.Messages[0].Payloads[len(second.Messages[0].Payloads)-1]))

	late := boundedPackage("cdc:a", 1, 100, "late")
	require.Equal(t, MessageDropped, q.PushMessage(Event{Type: EventMessage, Data: late}, gen))
	require.Empty(t, late.Messages, "caller owns and releases a fully dropped package")
	relay.ReleasePackage(late)

	events := q.Drain()
	require.Len(t, events, 2)
	for _, event := range events {
		if pkg, ok := event.Data.(*relay.Package); ok {
			relay.ReleasePackage(pkg)
		}
	}

	// A reset clears the overflow tombstone and permits a new stream
	// incarnation to reuse the same topic.
	q.Reset()
	reuse := boundedPackage("cdc:a", 1, 100, "reuse")
	require.Equal(t, MessageAccepted, q.PushMessage(Event{Type: EventMessage, Data: reuse}, q.Generation()))
	for _, event := range q.Drain() {
		if pkg, ok := event.Data.(*relay.Package); ok {
			relay.ReleasePackage(pkg)
		}
	}
}

func TestEventQueueMessageLimitsArePerTopic(t *testing.T) {
	q := NewEventQueue()
	gen := q.Generation()

	for _, topic := range []string{"cdc:a", "cdc:b"} {
		first := boundedPackage(topic, 1, 0, "first")
		require.Equal(t, MessageAccepted, q.PushMessage(Event{Type: EventMessage, Data: first}, gen))
		second := boundedPackage(topic, 1, 0, "second")
		require.Equal(t, MessageAccepted, q.PushMessage(Event{Type: EventMessage, Data: second}, gen))
	}

	events := q.Drain()
	require.Len(t, events, 4)
	for _, event := range events {
		if pkg, ok := event.Data.(*relay.Package); ok {
			relay.ReleasePackage(pkg)
		}
	}
}
