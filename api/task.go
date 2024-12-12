package api

import "context"

// Task is a task that is sent to the executor (TODO: replace with the interface when using)
type Task struct {
	ctx context.Context
	// App name
	App string `json:"src"`
	// Payload is args for lua
	Payload []byte `json:"payload"` // todo: use payload type + any for internal types for later mapping
	// URL query
	Query string
	// internal response channel
	response chan *TaskResult
}

func (t *Task) SetCallback(ch chan *TaskResult) {
	t.response = ch
}

func (t *Task) Future() chan *TaskResult {
	return t.response
}

func (t *Task) Respond(tr *TaskResult) {
	t.response <- tr
}

func (t *Task) Context() context.Context {
	return t.ctx
}

func (t *Task) UpdateCtx(ctx context.Context) {
	t.ctx = ctx
}

type TaskResult struct {
	Payload []byte `json:"payload"` // todo: payload type and possibly any for internal types
	Error   error  `json:"error"`
}
