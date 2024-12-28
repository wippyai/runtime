package runtime

import (
	"context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

const (
	System events.System = "executor"

	RegisterHandlerEvent events.Kind = "executor.set_handler"
	DeleteHandlerEvent   events.Kind = "executor.remove_handler"
)

type (
	RegisterHandler struct {
		Target  registry.ID
		Handler ExecutorHandler
	}

	DeleteHandler struct {
		Target registry.ID
	}

	Task struct {
		Context context.Context
		Target  registry.ID
		Payload payload.Payload
	}

	Result struct {
		Payload payload.Payload
		Error   error
	}

	ExecutorHandler func(Task) (chan *Result, error)

	Executor interface {
		Execute(Task) (chan *Result, error)
	}
)
