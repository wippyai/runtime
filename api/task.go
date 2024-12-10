package api

type Task struct {
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

func (t *Task) StreamingResponse() *Streamer {
	return &Streamer{ch: t.response}
}

type Streamer struct {
	ch chan *TaskResult
}

func (s *Streamer) ReadChunk() *TaskResult {
	select {
	case data := <-s.ch:
		return data
	default:
		// no data
		return nil
	}
}

type TaskResult struct {
	Payload []byte `json:"payload"` // todo: payload type and possibly any for internal types
	Error   error  `json:"error"`
}
