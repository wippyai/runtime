package server

import (
	"context"
	"github.com/ponyruntime/pony/api"
	eventsbus "github.com/ponyruntime/pony/eventbus"
	"github.com/ponyruntime/pony/exec"
	"github.com/ponyruntime/pony/payload"
	"github.com/ponyruntime/pony/subsystem"
	"go.uber.org/zap"
	"sync"
)

type Hub struct {
	log        *zap.Logger
	exec       *exec.Queue
	components map[api.Component]subsystem.Server

	// active configuration scope
	ruw         *sync.RWMutex
	configuring bool
	states      map[api.Component]*subsystem.State

	// configuration pipeline
	eid string
	eb  *eventsbus.Bus
}

func NewHub(
	log *zap.Logger,
	queue *exec.Queue,
	subsystems ...subsystem.Subsystem,
) *Hub {
	eb, id := eventsbus.GlobalEventBus()

	// Initialize maps with appropriate capacity
	subs := make(map[api.Component]subsystem.Server)
	for _, sys := range subsystems {
		subs[sys.Subsystem] = sys.Server
	}

	return &Hub{
		components: subs,
		exec:       queue,
		log:        log,
		states:     make(map[api.Component]*subsystem.State),
		eid:        id,
		eb:         eb,
	}
}

func (r *Hub) ListenEvents() {
	r.log.Debug("server: listening to events")

	evCh := make(chan api.Event, 10)
	// can't be an error here since we're provided all the data
	err := r.eb.SubscribeAll(context.Background(), r.eid, evCh)
	if err != nil {
		r.log.Fatal("server: failed to subscribe to events", zap.Error(err))
		return
	}

	go func() {
		for event := range evCh {
			// looking for subsystem
			s, ok := r.components[event.Component()]
			if !ok {
				r.log.Warn("server: received an event for an unknown subsystem", zap.Any("type", event.Type()))
				continue
			}

			state, _ := r.states[event.Component()] // can be nil

			newState, err := s.Handle(context.Background(), event, state)
			if err != nil {
				r.log.Error("server: failed to handle an event", zap.Error(err))
				continue
			}

			if newState != nil && state != newState {
				r.configuring = true
				r.states[event.Component()] = newState
				r.eb.Send(
					context.Background(),
					// got state update, report update
					eventsbus.NewEvent(
						api.Transaction,
						"state",
						payload.New(state),
					),
				)
			}
		}
	}()
}
