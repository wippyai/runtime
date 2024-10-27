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

// Component is a component that can carry its config using the state.
type Component interface {
	Register(context.Context, api.Event, any) (any, error)
	Commit(context.Context, any)

	Start(context.Context, *exec.Queue)
	Stop(context.Context)
}

type State struct {
	Component api.Component
	State     any
}

// assists in state management
type stateManager struct {
	states map[api.Component]State
}

func newStateManager() *stateManager {
	return &stateManager{
		states: make(map[api.Component]State),
	}
}

func (s *stateManager) Get(component api.Component) State {
	st, ok := s.states[component]
	if !ok {
		return State{
			Component: component,
			State:     nil,
		}
	}

	return st
}

func (s *stateManager) Set(component api.Component, state any) {
	s.states[component] = State{
		Component: component,
		State:     state,
	}
}
