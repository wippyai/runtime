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

type State struct {
	Component api.Component
	State     any
}

type Component interface {
	Handle(context.Context, api.Event, *State) (*State, error)
	Commit(context.Context, *State) error
	Start(context.Context, *exec.Queue)
	Stop(context.Context)
}
