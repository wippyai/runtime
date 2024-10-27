package subsystem

import (
	"context"
	"github.com/ponyruntime/pony/api"
	"github.com/ponyruntime/pony/exec"
)

type StateID string

type Subsystem struct {
	Subsystem api.Subsystem
	Server    Server
}

type State struct {
	Subsystem api.Subsystem
	State     any
}

type Server interface {
	Handle(context.Context, api.Event, *State) (*State, error)
	Commit(context.Context, *State) error
	Start(context.Context, *exec.Queue)
	Stop(context.Context)
}
