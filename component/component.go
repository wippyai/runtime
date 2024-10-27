package component

import (
	"context"
	"github.com/ponyruntime/pony/api"
	"github.com/ponyruntime/pony/exec"
)

type Declaration struct {
	ID        api.Component
	Component Component
}

type Component interface {
	Handle(context.Context, api.Event, any) (any, error)
	Commit(context.Context, any)

	Start(context.Context, *exec.Queue)
	Stop(context.Context)
}
