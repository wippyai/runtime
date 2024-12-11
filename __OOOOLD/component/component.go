package component

import (
	"context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/components/exec"
)

// Component is a components that can carry its chart using the state.
type Component interface {
	Register(context.Context, events.Event, State) (State, error)
	Apply(context.Context, State) error
	Start(context.Context, *exec.Queue)
	Stop(context.Context)
}

type State interface {
	Discard(context.Context)
}

type Declaration struct {
	ID        events.System
	Component Component
}
