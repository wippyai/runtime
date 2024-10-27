package config

import (
	"encoding/json"
	"github.com/ponyruntime/pony/api"
	"go.uber.org/zap"
	"os"
)

package json

import (
"context"
"encoding/json"
"github.com/ponyruntime/pony/api"
"go.uber.org/zap"
"os"
)

type ChangeReader struct {
	log     *zap.Logger
	evBusID string
	eb      *eventsbus2.Bus
}

func NewReader() *ChangeReader {
	return &ChangeReader{}
}

func (p *ChangeReader) Parse(path string) error {
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

	// send the new config to all subsystems
	p.eb.Send(context.Background(), eventsbus2.NewEvent(api.EventConfigurationUpdated, api.SubSystemAll, cfg))

	return nil
}

func (p *ChangeReader) ListenEvents() {
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
		switch event.SubSystem() {
		// broadcast event
		case api.SubSystemAll:
			p.log.Info("json: broadcast event", zap.Any("event", event.Kind()))

		// listen only for config events
		case api.Transaction:
			// handle config events
			switch event.Kind() {

			}
		}
	}
}

func (p *ChangeReader) fatal(err error) {
	// parse each subsystem config and send events to the appropriate event bus
	event := eventsbus2.NewEvent(api.EventFatalError, api.SubSystemAll, []byte(err.Error()))
	// fire an event
	p.eb.Send(context.Background(), event)
}
