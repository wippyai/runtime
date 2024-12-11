// Package futures provides a simple queue for tasks that are awaiting completion.
package exec

import (
	"context"
	"github.com/ponyruntime/pony/api/internal"
)

type Initiator interface {
	SetCallback(chan *internal.TaskResult)
	Future() chan *internal.TaskResult
}

type Responder interface {
	Respond(*internal.TaskResult)
}

// Queue is a super dumb and temporary queue for tasks that are awaiting completion (channel wrapper).
// TODO: replace with a futures executor
// TODO: mehods: Done(result), Await() <-chan result
// Done should send the result the awaiting Queue
// Await should be able to send a several tasks withing a one group
type Queue struct {
	awaitCh chan *internal.Task
}

func NewQueue() *Queue {
	return &Queue{
		awaitCh: make(chan *internal.Task, 100),
	}
}

// TODO: interface task *api.Task
func (q *Queue) Await(ctx context.Context, task *internal.Task) chan *internal.TaskResult {
	task.SetCallback(make(chan *internal.TaskResult, 1))
	q.awaitCh <- task
	return task.Future()
}

func (q *Queue) All() <-chan *internal.Task {
	return q.awaitCh
}
