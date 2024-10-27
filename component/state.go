package component

import "github.com/ponyruntime/pony/api"

type State struct {
	Component api.Component
	State     any
}

type stateManager struct {
	states map[api.Component]State
}

func newStateManager() *stateManager {
	return &stateManager{
		states: make(map[api.Component]State),
	}
}

func (s *stateManager) Get(component api.Component) State {
	return s.states[component]
}

func (s *stateManager) Set(component api.Component, state any) {
	s.states[component] = State{
		Component: component,
		State:     state,
	}
}
