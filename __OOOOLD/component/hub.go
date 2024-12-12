package component

import (
	"context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/components/config"
	"github.com/ponyruntime/pony/components/exec"

	"github.com/ponyruntime/pony/api/payload"
	ebs "github.com/ponyruntime/pony/components/events"
	"go.uber.org/zap"
)

// Hub manages states of multiple nested components.
type Hub struct {
	log  *zap.Logger
	exec *exec.Queue

	components cmap
	changes    *smap

	// chart pipeline
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
	cmp := make(cmap)
	for _, sys := range components {
		cmp[sys.ID] = sys.Component
		log.Debug("registered components", zap.String("components", string(sys.ID)))
	}

	return &Hub{
		components: cmp,
		exec:       queue,
		log:        log,
		eid:        id,
		eb:         eb,
	}
}

func (r *Hub) Close(ctx context.Context) {
	r.eb.Unsubscribe(ctx, r.eid)
	r.eb = nil
	r.eid = ""

	r.components.Stop(ctx)
}

func (r *Hub) Boot(ctx context.Context) {
	r.log.Debug("listening to configuration events")

	// Start services
	r.components.Start(ctx, r.exec)

	evCh := make(chan events.Event, 10)
	// can't be an error here since we're provided all the data
	err := r.eb.SubscribeAll(ctx, r.eid, evCh)
	if err != nil {
		r.log.Fatal("failed to subscribe to events", zap.Error(err))
		return
	}

	go func() {
		for event := range evCh {
			if event.System() == config.ConfigGroup {
				r.onConfigurationChange(ctx, event)
				continue
			}

			if r.changes == nil {
				r.log.Warn("unable to apply change without open changeset map")
				continue
			}

			cset := r.changes.get(event.System()).changes

			updated, err := r.components.Register(ctx, event, cset)
			if err != nil {
				r.log.Error(
					"failed to register an events",
					zap.String("components", string(event.System())),
					zap.Error(err),
				)
				continue
			}

			// updating the reference
			if updated != cset {
				r.changes.set(event.System(), updated)

				r.eb.Send(
					ctx,
					ebs.NewEvent(
						config.ConfigGroup,
						config.AckState,
						payload.New(state{
							component: event.System(),
							changes:   cset,
						}),
					),
				)
			}
		}
	}()
}

func (r *Hub) onConfigurationChange(ctx context.Context, e events.Event) {
	switch e.Kind() {
	case config.Begin:
		if r.changes != nil {
			r.log.Error("overlapping transactions detected")
			return
		}

		if r.changes == nil {
			r.changes = newStateMap()
		}

	case config.Discard:
		if r.changes == nil {
			r.log.Error("no transaction to discard")
			return
		}

		r.log.Debug("discard all changes")
		for _, state := range r.changes.states() {
			cset := state.changes
			if cset == nil {
				r.log.Warn("no changes to apply")
				continue
			}

			cset.Discard(ctx)

			r.eb.Send(
				ctx,
				ebs.NewEvent(config.ConfigGroup, config.Done, payload.New(state)),
			)
		}
		r.changes = nil

	case config.Apply:
		if r.changes == nil {
			r.log.Warn("no transaction to commit")
			return
		}

		defer func() { r.changes = nil }()
		r.log.Debug("committing transaction")

		for _, state := range r.changes.states() {
			cmp, ok := r.components[state.component]
			if !ok {
				r.log.Warn("registry/components mismatch", zap.Any("type", e.Kind()))
				continue
			}

			r.log.Debug("apply components registry", zap.Any("components", state.component))

			cset := state.changes
			if cset == nil {
				r.log.Warn("no changes to apply")
				continue
			}

			err := cmp.Apply(ctx, cset)
			if err != nil {
				r.log.Error("failed to apply changes", zap.Error(err))
				r.eb.Send(
					ctx,
					ebs.NewEvent(config.ConfigGroup, config.Deny, payload.New(state)),
				)
				continue
			}

			r.eb.Send(
				ctx,
				ebs.NewEvent(config.ConfigGroup, config.Done, payload.New(state)),
			)
		}
	}
}
