package tasks

import (
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

type Task struct {
	Target  registry.Path
	Payload payload.Payload
	result  chan *Result
}

type Result struct {
	Payload payload.Payload
	Error   error
}

func (t *Task) SetCallback(ch chan *Result) {
	t.result = ch
}

func (t *Task) Future() chan *Result {
	return t.result
}

func (t *Task) Respond(tr *Result) {
	t.result <- tr
}
