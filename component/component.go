package component

import (
	"context"

	"github.com/ponyruntime/pony/api"
	"github.com/ponyruntime/pony/exec"
)

// Component is a component that can carry its config using the state.
type Component interface {
	Register(context.Context, api.Event, State) (State, error)
	Apply(context.Context, State) error
	Start(context.Context, *exec.Queue)
	Stop(context.Context)
}

type State interface {
	Discard(context.Context)
}

type Declaration struct {
	ID        api.Component
	Component Component
}
