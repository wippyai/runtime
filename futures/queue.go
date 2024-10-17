// Package futures provides a simple queue for tasks that are awaiting completion.
package futures

import (
	"context"

	"github.com/ponyruntime/pony/api"
)

// Queue is a super dumb and temporary queue for tasks that are awaiting completion (channel wrapper).
// TODO: replace with a futures executor
// TODO: mehods: Done(result), Await() <-chan result
// Done should send the result the awaiting Queue
// Await should be able to send a several tasks withing a one group
type Queue struct {
	awaitCh chan *api.Task
}

func NewQueue() *Queue {
	return &Queue{
		awaitCh: make(chan *api.Task, 100),
	}
}

func (q *Queue) Await(ctx context.Context, task *api.Task) {
	q.awaitCh <- task
}

func (q *Queue) All() <-chan *api.Task {
	return q.awaitCh
}
