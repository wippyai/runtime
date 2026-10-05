// SPDX-License-Identifier: MPL-2.0

package process

import "testing"

type ownedCompletion struct{ discarded int }

func (c *ownedCompletion) DiscardEvent() { c.discarded++ }

func TestRetiredCompletionsReleaseOwnership(t *testing.T) {
	q := NewEventQueue()
	first, second, message := &ownedCompletion{}, &ownedCompletion{}, &ownedCompletion{}
	gen := q.Generation()
	for _, e := range []Event{
		{Type: EventYieldComplete, Tag: 1, Data: first},
		{Type: EventMessage, Data: message},
		{Type: EventYieldComplete, Tag: 2, Data: second},
	} {
		if !q.Push(e, gen) {
			t.Fatal("queue rejected event")
		}
	}
	q.RetireYieldCompletions()
	q.RetireYieldCompletions()
	if first.discarded != 1 || second.discarded != 1 {
		t.Fatalf("retired ownership: %d, %d; want once each", first.discarded, second.discarded)
	}
	if message.discarded != 0 || q.Generation() != gen {
		t.Fatal("retirement changed message ownership or queue generation")
	}
	q.Close()
	if first.discarded != 1 || second.discarded != 1 || message.discarded != 1 {
		t.Fatal("close must discard only the retained message")
	}
}
