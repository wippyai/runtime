package json

import (
	"context"
	"encoding/json"
	eventsbus2 "github.com/ponyruntime/pony/component/eventbus"
	"os"

	"github.com/ponyruntime/pony/api"
	"go.uber.org/zap"
)

type Provider struct {
	log     *zap.Logger
	evBusID string
	eb      *eventsbus2.Bus
}

func NewProvider(log *zap.Logger) *Provider {
	eb, id := eventsbus2.GlobalEventBus()
	return &Provider{
		evBusID: id,
		eb:      eb,
	}
}

func (p *Provider) Parse(path string) error {
	file, err := os.ReadFile(path)
	if err != nil {
		p.fatal(err)
		return err
	}

	cfg := &api.JSONConfiguration{}
	err = json.Unmarshal(file, cfg)
	if err != nil {
		p.fatal(err)
		return err
	}

	// send the new configuration to all subsystems
	p.eb.Send(context.Background(), eventsbus2.NewEvent(api.EventConfigurationUpdated, api.SubSystemAll, cfg))

	return nil
}

func (p *Provider) ListenEvents() {
	evCh := make(chan api.Event, 10)
	// can't be an error here since we're provided all the data
	_ = p.eb.SubscribeP(
		context.Background(),
		p.evBusID,
		api.SubSystemEndpoints,
		api.EventsAll,
		evCh,
	)

	// why do we listen inside the provider?
	for event := range evCh {
		switch event.Component() {
		// broadcast event
		case api.SubSystemAll:
			p.log.Info("json: broadcast event", zap.Any("event", event.Type()))

		// listen only for configuration events
		case api.Transaction:
			// handle configuration events
			switch event.Type() {

			}
		}
	}
}

func (p *Provider) fatal(err error) {
	// parse each subsystem configuration and send events to the appropriate event bus
	event := eventsbus2.NewEvent(api.EventFatalError, api.SubSystemAll, []byte(err.Error()))
	// fire an event
	p.eb.Send(context.Background(), event)
}
