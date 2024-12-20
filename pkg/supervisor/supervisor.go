package supervisor

import (
	"context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/pkg/eventbus"
	"go.uber.org/zap"
)

type Supervisor struct {
	log *zap.Logger
	bus events.Bus
	dtt payload.Transcoder
	scr *eventbus.Subscriber
}

func NewSupervisor(
	log *zap.Logger,
	bus events.Bus,
	dtt payload.Transcoder,
) *Supervisor {
	return &Supervisor{
		log: log,
		bus: bus,
		dtt: dtt,
	}
}

func (s *Supervisor) Start(ctx context.Context) (err error) {
	s.scr, err = eventbus.NewSubscriber(
		ctx,
		s.bus,
		"(supervisor|registry)",           // we listen to registry to pick up new entries we have to manage
		"(supervisor|registry|entry).(*)", // all declarations, statuses and registry transitions
		s.handleEvent,
	)

	return
}

func (s *Supervisor) Stop() {
	s.scr.Close()
}

func (s *Supervisor) handleEvent(e events.Event) {
	s.log.Debug("received event", zap.Any("event", e))

	switch e.Kind {
	case registry.Create:

	case registry.Update:

	case registry.Delete:

	// handle register
	default:
		// ignore
	}
}
