package runtime

import (
	"context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

const (
	System events.System = "executor"
)

type (
	Task struct {
		Context context.Context
		Target  registry.ID
		Payload payload.Payload
	}

	Result struct {
		Payload payload.Payload
		Error   error
	}

	Executor interface {
		Execute(Task) (chan *Result, error)
	}

	// todo: do we need it?
	Engine interface {
		Execute(Task) (*Result, error)
	}
)
