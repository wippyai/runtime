// SPDX-License-Identifier: MPL-2.0
package process

import (
	"errors"
	"testing"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

type admissionTestEvent struct{ discarded int }

func (e *admissionTestEvent) DiscardEvent() { e.discarded++ }

type admissionTestPolicy struct {
	err   error
	event *admissionTestEvent
	calls int
}

func (p *admissionTestPolicy) AdmitEvent(e Event) (Event, error) {
	p.calls++
	if p.err != nil {
		return e, p.err
	}
	e.Data = p.event
	return e, nil
}

func TestQueueAdmissionOwnership(t *testing.T) {
	q := NewEventQueue()
	policy := &admissionTestPolicy{event: &admissionTestEvent{}}
	q.SetAdmission(policy)
	if err := q.PushWithError(Event{Type: EventMessage}, q.Generation()); err != nil {
		t.Fatal(err)
	}
	q.Close()
	q.Close()
	if policy.event.discarded != 1 {
		t.Fatalf("pending event discarded %d times", policy.event.discarded)
	}
	q.Reset()
	q.SetAdmission(policy)
	policy.event = &admissionTestEvent{}
	if !q.Push(Event{Type: EventMessage}, q.Generation()) {
		t.Fatal("push failed")
	}
	events := q.Drain()
	if len(events) != 1 {
		t.Fatal("missing delivery")
	}
	q.Close()
	if policy.event.discarded != 0 {
		t.Fatal("queue discarded consumer-owned event")
	}
}

func TestQueueRejectsStaleBeforeAdmission(t *testing.T) {
	q := NewEventQueue()
	old := q.Generation()
	q.Reset()
	policy := &admissionTestPolicy{event: &admissionTestEvent{}}
	q.SetAdmission(policy)
	if err := q.PushWithError(Event{Type: EventMessage}, old); !errors.Is(err, ErrProcessClosed) {
		t.Fatal(err)
	}
	if policy.calls != 0 {
		t.Fatal("stale producer reached admission")
	}
	want := errors.New("mailbox full")
	policy.err = want
	if err := q.PushWithError(Event{Type: EventMessage}, q.Generation()); !errors.Is(err, want) {
		t.Fatalf("lost overload error: %v", err)
	}
	if q.HasEvents() {
		t.Fatal("rejected event queued")
	}
	q.Reset()
	if !q.Push(Event{Type: EventMessage}, q.Generation()) {
		t.Fatal("policy retained across reset")
	}
	if policy.calls != 1 {
		t.Fatal("reset retained admission")
	}
	q.Close()
}

// PushMessage is the scheduler's message path. It must retain the generic
// process admission boundary before applying optional queue-local accounting:
// a WASM mailbox may copy and consume the relay package, then replaces it with
// an owned delivery event.
func TestQueuePushMessageRunsProcessAdmissionBeforeTopicAccounting(t *testing.T) {
	q := NewEventQueue()
	policy := &admissionTestPolicy{event: &admissionTestEvent{}}
	q.SetAdmission(policy)
	pkg := relay.NewPackage(pid.PID{}, pid.PID{}, "bounded")
	// A bounded topic would create queue accounting if it ran before the
	// process policy replaced the package with its own delivery event.
	pkg.Messages[0].MaxItems = 1

	admission, err := q.PushMessageWithError(Event{Type: EventMessage, Data: pkg}, q.Generation())
	if err != nil || admission != MessageAccepted {
		t.Fatalf("admission = (%v, %v), want (%v, nil)", admission, err, MessageAccepted)
	}
	if policy.calls != 1 || len(pkg.Messages) != 1 {
		t.Fatalf("policy calls=%d package ownership was unexpectedly changed", policy.calls)
	}
	if len(q.messageTopics) != 0 {
		t.Fatal("topic accounting ran after process admission replaced the package")
	}
	events := q.Drain()
	if len(events) != 1 || events[0].Data != policy.event {
		t.Fatal("message did not retain the policy-owned event")
	}
	q.Close()
	if policy.event.discarded != 0 {
		t.Fatal("consumer-owned drained event was discarded")
	}
	relay.ReleasePackage(pkg)
}
