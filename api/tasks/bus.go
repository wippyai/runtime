package tasks

import (
	"context"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

type (
	Task struct {
		Context context.Context
		Target  registry.Path
		Payload payload.Payload
	}

	Result struct {
		Payload payload.Payload
		Error   error
	}

	Executor interface {
		Execute(Task) (chan *Result, error)
	}
)
