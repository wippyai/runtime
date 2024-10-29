package component

import (
	"context"
	"github.com/ponyruntime/pony/api"
	"github.com/ponyruntime/pony/exec"
)

// Component is a component that can carry its config using the state.
type Component interface {
	Register(context.Context, api.Event, State) (State, error)
	Start(context.Context, *exec.Queue)
	Stop(context.Context)
}

type State interface {
	// Apply must activate current changeset in related component, must not return any since it is expected to be
	// atomic in ideal world. Validate and compile at event registration phase.
	Apply(context.Context) error
	Discard(context.Context)
}

type Declaration struct {
	ID        api.Component
	Component Component
}
