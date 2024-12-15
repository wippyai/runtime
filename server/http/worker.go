package http

import "github.com/ponyruntime/pony/api/tasks"

type serverWorker struct {
	exec      tasks.Executor
	endpoints []EndpointConfig
}
