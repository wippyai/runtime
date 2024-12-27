package runtime

import (
	"context"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
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

	Runtime interface {
		AddLibrary(registry.ID, LibraryConfig) error
		UpdateLibrary(registry.ID, LibraryConfig) error
		AddFunction(registry.ID, FunctionConfig) error
		UpdateFunction(registry.ID, FunctionConfig) error
		Delete(registry.ID) error
	}
)
