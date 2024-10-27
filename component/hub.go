package component

import (
	"context"
	"github.com/ponyruntime/pony/api"
	"github.com/ponyruntime/pony/api/payload"
	ebs "github.com/ponyruntime/pony/component/eventbus"
	"github.com/ponyruntime/pony/exec"
	"go.uber.org/zap"
	"sync"
)

// Hub manages states of multiple nested components.
type Hub struct {
	log        *zap.Logger
	exec       *exec.Queue
	components map[api.Component]Component

	// active config scope
	sm *stateManager

	// config pipeline
	eid string
	eb  *ebs.Bus
}

func NewHub(
	log *zap.Logger,
	queue *exec.Queue,
	components ...Declaration,
) *Hub {
	eb, id := ebs.GlobalEventBus()

	// Initialize maps with appropriate capacity
	cmp := make(map[api.Component]Component)
	for _, sys := range components {
		cmp[sys.ID] = sys.Component
		log.Debug("registered component", zap.String("component", string(sys.ID)))
	}

	return &Hub{
		components: cmp,
		exec:       queue,
		log:        log,
		eid:        id,
		eb:         eb,
	}
}

func (r *Hub) Close() {
	r.eb.Unsubscribe(context.Background(), r.eid)
	r.eb = nil
	r.eid = ""

	wg := sync.WaitGroup{}
	for _, s := range r.components {
		wg.Add(1)
		go func() { defer wg.Done(); s.Stop(context.Background()) }()
	}
	wg.Wait()
}

func (r *Hub) Boot(ctx context.Context) {
	r.log.Debug("listening to configuration events")

	// start services
	for _, s := range r.components {
		s.Start(ctx, r.exec)
	}

	evCh := make(chan api.Event, 10)
	// can't be an error here since we're provided all the data
	err := r.eb.SubscribeAll(ctx, r.eid, evCh)
	if err != nil {
		r.log.Fatal("failed to subscribe to events", zap.Error(err))
		return
	}

	go func() {
		for event := range evCh {
			if event.Component() == api.Transaction {
				r.onTransaction(ctx, event)
				continue
			}

			// looking for subsystem
			s, ok := r.components[event.Component()]
			if !ok {
				// hub does not handle this component
				continue
			}

			if r.sm == nil {
				r.log.Warn("no open transaction, skipping event", zap.String("event", string(event.Kind())))
				continue
			}

			// looking for state
			state := r.sm.Get(event.Component())

			newState, err := s.Register(ctx, event, state.State)
			if err != nil {
				r.log.Error("failed to handle an event", zap.Error(err))
				continue
			}

			// registering state change
			if newState != nil && newState != state.State {
				r.sm.Set(event.Component(), newState)

				r.eb.Send(
					ctx,
					ebs.NewEvent(
						api.Transaction,
						api.EventRegisterChange,
						payload.New(State{
							Component: event.Component(),
							State:     newState,
						}),
					),
				)
			}
		}
	}()
}

func (r *Hub) onTransaction(ctx context.Context, e api.Event) {
	if e.Kind() == api.EventBegin {
		if r.sm != nil {
			r.log.Warn("working withing internal transaction")
		}

		if r.sm == nil {
			r.sm = newStateManager()
		}
		return
	}

	if e.Kind() == api.EventRollback {
		r.log.Debug("rolling back transaction")
		r.sm = nil
		return
	}

	if e.Kind() == api.EventCommit {
		if r.sm == nil {
			r.log.Warn("no transaction to commit")
			return
		}

		defer func() { r.sm = nil }()

		r.log.Debug("committing transaction")

		for _, state := range r.sm.states {
			s, ok := r.components[state.Component]
			if !ok {
				r.log.Warn("state/component mismatch", zap.Any("type", e.Kind()))
				continue
			}

			s.Commit(ctx, state.State)
			r.log.Debug("commited component state", zap.Any("component", state.Component))

			r.eb.Send(
				ctx,
				ebs.NewEvent(
					api.Transaction,
					api.EventRegisterCommit,
					payload.New(state),
				),
			)
		}

		return
	}
}
