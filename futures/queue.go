// Package futures provides a simple queue for tasks that are awaiting completion.
package futures

import (
	"context"

	"github.com/ponyruntime/pony/api"
)

type Initiator interface {
	SetCallback(chan *api.TaskResult)
	Future() chan *api.TaskResult
}

type Responder interface {
	Respond(*api.TaskResult)
}

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

// TODO: interface task *api.Task
func (q *Queue) Await(ctx context.Context, task *api.Task) chan *api.TaskResult {
	task.SetCallback(make(chan *api.TaskResult, 1))
	q.awaitCh <- task
	return task.Future()
}

func (q *Queue) All() <-chan *api.Task {
	return q.awaitCh
}
