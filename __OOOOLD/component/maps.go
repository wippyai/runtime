package component

import (
	"context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/components/exec"
	"sync"
)

type cmap map[events.System]Component

func (m cmap) Register(ctx context.Context, event events.Event, changes State) (State, error) {
	if c, ok := m[event.System()]; ok {
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
	component events.System
	changes   State
}

type smap struct {
	components map[events.System]state
	order      []events.System // Maintains registration order
}

// New creates a new smap
func newStateMap() *smap {
	return &smap{
		components: make(map[events.System]state),
		order:      make([]events.System, 0),
	}
}

func (s *smap) get(component events.System) state {
	st, ok := s.components[component]
	if !ok {
		return state{
			component: component,
			changes:   nil,
		}
	}
	return st
}

func (s *smap) set(component events.System, changes State) {
	_, exists := s.components[component]
	if !exists {
		// Only append to order slice if this is a new components
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
