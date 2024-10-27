package component

import (
	"context"
	"github.com/ponyruntime/pony/api"
	eventsbus2 "github.com/ponyruntime/pony/component/eventbus"
	"github.com/ponyruntime/pony/exec"
	"github.com/ponyruntime/pony/payload"
	"go.uber.org/zap"
)

type Hub struct {
	log        *zap.Logger
	exec       *exec.Queue
	components map[api.Component]Component

	// active configuration scope
	configuring bool
	states      map[api.Component]any

	// configuration pipeline
	eid string
	eb  *eventsbus2.Bus
}

func NewHub(
	log *zap.Logger,
	queue *exec.Queue,
	components ...Declaration,
) *Hub {
	eb, id := eventsbus2.GlobalEventBus()

	// Initialize maps with appropriate capacity
	cmp := make(map[api.Component]Component)
	for _, sys := range components {
		cmp[sys.ID] = sys.Component
	}

	return &Hub{
		components: cmp,
		exec:       queue,
		log:        log,
		states:     make(map[api.Component]any),
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
			// todo: handle transaction level events here
			if event.Type() == api.EventBegin {
				// todo: finish
				r.configuring = true
			}

			// looking for subsystem
			s, ok := r.components[event.Component()]
			if !ok {
				r.log.Warn("server: received an event for an unknown subsystem", zap.Any("type", event.Type()))
				continue
			}

			state, _ := r.states[event.Component()] // can be nil

			cst, err := s.Handle(context.Background(), event, state)
			if err != nil {
				r.log.Error("server: failed to handle an event", zap.Error(err))
				continue
			}

			// registering state change
			if cst != nil && state != cst {
				r.configuring = true
				r.states[event.Component()] = cst

				r.eb.Send(
					context.Background(),
					// got state update, report update
					eventsbus2.NewEvent(
						api.Transaction,
						api.EventStateChange,
						payload.New(state),
					),
				)
			}
		}
	}()
}
