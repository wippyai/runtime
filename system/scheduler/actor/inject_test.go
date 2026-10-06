// SPDX-License-Identifier: MPL-2.0

package actor

import "testing"

func TestInjectQueueIsEmpty(t *testing.T) {
	q := NewInjectQueue()
	if !q.IsEmpty() {
		t.Fatal("a new queue is empty")
	}
	q.Push(&Processor{})
	if q.IsEmpty() {
		t.Fatal("a queue holding a processor is not empty")
	}
	if q.Pop() == nil {
		t.Fatal("expected the queued processor")
	}
	if !q.IsEmpty() {
		t.Fatal("a drained queue is empty")
	}
}
