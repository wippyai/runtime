package api

type Task struct {
	ID       string           `json:"id"`
	App      string           `json:"app"`
	Registry *Registry        `json:"registry"`
	Response chan *TaskResult `json:"payload"`
}

type TaskResult struct {
	ID      string `json:"id"`
	Payload []byte `json:"payload"`
	Error   error  `json:"error"`
}
