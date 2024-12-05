package component

import (
	"context"
	"github.com/ponyruntime/pony/api"
	"github.com/ponyruntime/pony/exec"
	"sync"
)

type cmap map[api.Component]Component

func (m cmap) Register(ctx context.Context, event api.Event, changes State) (State, error) {
	if c, ok := m[event.Component()]; ok {
		return c.Register(ctx, event, changes)
	}
	return nil, nil
}

func (m cmap) Start(ctx context.Context, queue *exec.Queue) {
	for _, c := range m {
		c.Start(ctx, queue)
	}
}

func (m cmap) Stop(ctx context.Context) {
	wg := sync.WaitGroup{}

	for _, c := range m {
		wg.Add(1)
		go func() { defer wg.Done(); c.Stop(ctx) }()
	}

	wg.Wait()
}

type state struct {
	component api.Component
	changes   State
}

type smap struct {
	components map[api.Component]state
	order      []api.Component // Maintains registration order
}

// New creates a new smap
func newStateMap() *smap {
	return &smap{
		components: make(map[api.Component]state),
		order:      make([]api.Component, 0),
	}
}

func (s *smap) get(component api.Component) state {
	st, ok := s.components[component]
	if !ok {
		return state{
			component: component,
			changes:   nil,
		}
	}
	return st
}

func (s *smap) set(component api.Component, changes State) {
	_, exists := s.components[component]
	if !exists {
		// Only append to order slice if this is a new component
		s.order = append(s.order, component)
	}

	s.components[component] = state{
		component: component,
		changes:   changes,
	}
}

func (s *smap) discard(ctx context.Context) {
	for _, state := range s.components {
		if state.changes != nil {
			state.changes.Discard(ctx)
		}
	}
}

func (s *smap) states() []state {
	// Return states in registration order
	states := make([]state, 0, len(s.components))
	for _, component := range s.order {
		if state, ok := s.components[component]; ok {
			states = append(states, state)
		}
	}

	return states
}
