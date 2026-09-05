// SPDX-License-Identifier: MPL-2.0
package process

import (
	"errors"
	"testing"
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
