package api

type Task struct {
	App string `json:"app"`
	// Payload is args for lua
	Payload []byte `json:"payload"`
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

type TaskResult struct {
	ID      string `json:"id"`
	Payload []byte `json:"payload"`
	Error   error  `json:"error"`
}
