// SPDX-License-Identifier: MPL-2.0

package process

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

type countingRetentionLease struct {
	releases atomic.Int32
}

func (l *countingRetentionLease) Release() {
	l.releases.Add(1)
}

func TestEventQueuePushDirectClosedReleasesPackage(t *testing.T) {
	q := NewEventQueue()
	q.Close()

	lease := &countingRetentionLease{}
	msg := relay.AcquireMessage()
	msg.Topic = "closed"
	msg.Payloads = payload.Payloads{payload.NewString("value")}
	msg.SetRetentionLease(lease)
	pkg := relay.NewMessagePackage(pid.PID{}, pid.PID{}, msg)

	require.False(t, q.PushDirect(Event{Type: EventMessage, Data: pkg}))
	require.Equal(t, int32(1), lease.releases.Load(), "closed PushDirect must consume package ownership")
}

func TestEventQueueRetentionSurvivesDrainAndTopicReuse(t *testing.T) {
	q := NewEventQueue()
	gen := q.Generation()

	first := boundedPackage("reuse", 1, 100, "first")
	require.Equal(t, MessageAccepted, q.PushMessage(Event{Type: EventMessage, Data: first}, gen))
	second := boundedPackage("reuse", 1, 100, "second")
	require.Equal(t, MessageAccepted, q.PushMessage(Event{Type: EventMessage, Data: second}, gen))

	events := q.Drain()
	require.Len(t, events, 2)
	firstPkg, ok := events[0].Data.(*relay.Package)
	require.True(t, ok)
	firstLease := firstPkg.Messages[0].TakeRetentionLease()
	require.NotNil(t, firstLease)
	require.Len(t, q.messageTopics, 0, "terminal drain retires the topic state")

	// Release the terminal package but keep the first data reservation alive.
	secondPkg, ok := events[1].Data.(*relay.Package)
	require.True(t, ok)
	relay.ReleasePackage(secondPkg)
	relay.ReleasePackage(firstPkg)

	// The new stream gets a new topic state. The old lease must not debit its
	// counters when it is released after the replacement has admitted data.
	replacement := boundedPackage("reuse", 1, 100, "replacement")
	require.Equal(t, MessageAccepted, q.PushMessage(Event{Type: EventMessage, Data: replacement}, q.Generation()))
	overflow := boundedPackage("reuse", 1, 100, "overflow")
	require.Equal(t, MessageAccepted, q.PushMessage(Event{Type: EventMessage, Data: overflow}, q.Generation()))
	require.Len(t, overflow.Messages, 1)
	require.True(t, payload.IsTerminal(overflow.Messages[0].Payloads[len(overflow.Messages[0].Payloads)-1]))

	firstLease.Release()
	for _, event := range q.Drain() {
		if pkg, ok := event.Data.(*relay.Package); ok {
			relay.ReleasePackage(pkg)
		}
	}
}

func TestEventQueueRetiresUniqueTopicState(t *testing.T) {
	q := NewEventQueue()
	for i := 0; i < 256; i++ {
		topic := "churn:" + strconv.Itoa(i)
		data := boundedPackage(topic, 1, 32, "data")
		require.Equal(t, MessageAccepted, q.PushMessage(Event{Type: EventMessage, Data: data}, q.Generation()))
		terminal := relay.NewPackage(pid.PID{}, pid.PID{}, topic, payload.NewTerminal())
		terminal.Messages[0].MaxItems = 1
		terminal.Messages[0].MaxBytes = 32
		require.Equal(t, MessageAccepted, q.PushMessage(Event{Type: EventMessage, Data: terminal}, q.Generation()))
		for _, event := range q.Drain() {
			if pkg, ok := event.Data.(*relay.Package); ok {
				relay.ReleasePackage(pkg)
			}
		}
		require.Empty(t, q.messageTopics, "terminal topic state must not grow with churn")
	}
}

func TestEventQueueDrainClearsPreviousData(t *testing.T) {
	q := NewEventQueue()
	gen := q.Generation()
	require.True(t, q.Push(Event{Data: &struct{ Value string }{"stale"}}, gen))
	events := q.Drain()
	require.Len(t, events, 1)
	require.NotNil(t, events[0].Data)

	require.Nil(t, q.Drain())
	require.Nil(t, events[0].Data, "queue must clear the reusable drain buffer before reuse")
}

func TestEventQueueCloseReleasesQueuedPackageOnce(t *testing.T) {
	q := NewEventQueue()
	lease := &countingRetentionLease{}
	msg := relay.AcquireMessage()
	msg.Topic = "close"
	msg.Payloads = payload.Payloads{payload.NewString("value")}
	msg.SetRetentionLease(lease)
	pkg := relay.NewMessagePackage(pid.PID{}, pid.PID{}, msg)
	require.True(t, q.PushDirect(Event{Type: EventMessage, Data: pkg}))

	q.Close()
	q.Close()
	require.Equal(t, int32(1), lease.releases.Load(), "repeated close must not release a queued package twice")
}

func TestEventQueueConcurrentDirectCloseOwnsEachPackage(t *testing.T) {
	q := NewEventQueue()
	const n = 128
	leases := make([]*countingRetentionLease, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		lease := &countingRetentionLease{}
		leases[i] = lease
		msg := relay.AcquireMessage()
		msg.Topic = "race"
		msg.Payloads = payload.Payloads{payload.NewString("value")}
		msg.SetRetentionLease(lease)
		pkg := relay.NewMessagePackage(pid.PID{}, pid.PID{}, msg)
		wg.Add(1)
		go func(pkg *relay.Package) {
			defer wg.Done()
			q.PushDirect(Event{Type: EventMessage, Data: pkg})
		}(pkg)
	}
	q.Close()
	wg.Wait()

	for _, lease := range leases {
		require.Equal(t, int32(1), lease.releases.Load())
	}
	for _, event := range q.Drain() {
		if pkg, ok := event.Data.(*relay.Package); ok {
			relay.ReleasePackage(pkg)
		}
	}
}
