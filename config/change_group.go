package config

import (
	eventsbus2 "github.com/ponyruntime/pony/component/eventbus"
	"go.uber.org/zap"
)

// todo: make global
type ChangeGroup struct {
	log     *zap.Logger
	evBusID string
	eb      *eventsbus2.Bus
}

func NewChangeGroup(log *zap.Logger, eb *eventsbus2.Bus) *ChangeGroup {
	return &ChangeGroup{
		log:     log,
		evBusID: "change_group",
		eb:      eb,
	}
}

//func NewReader() *ChangeGroup {
//	return &ChangeGroup{}
//}
//
//func (p *ChangeGroup) Parse(path string) error {
//	file, err := os.ReadFile(path)
//	if err != nil {
//		p.fatal(err)
//		return err
//	}
//
//	cfg := &api.JSONConfiguration{}
//	err = json.Unmarshal(file, cfg)
//	if err != nil {
//		p.fatal(err)
//		return err
//	}
//
//	// send the new config to all subsystems
//	p.eb.Send(context.Background(), eventsbus2.NewEvent(api.EventConfigurationUpdated, api.SubSystemAll, cfg))
//
//	return nil
//}
//
//func (p *ChangeGroup) ListenEvents() {
//	evCh := make(chan api.Event, 10)
//	// can't be an error here since we're provided all the data
//	_ = p.eb.SubscribeP(
//		context.Background(),
//		p.evBusID,
//		api.SubSystemEndpoints,
//		api.EventsAll,
//		evCh,
//	)
//
//	// why do we listen inside the provider?
//	for event := range evCh {
//		switch event.SubSystem() {
//		// broadcast event
//		case api.SubSystemAll:
//			p.log.Info("json: broadcast event", zap.Any("event", event.Kind()))
//
//		// listen only for config events
//		case api.Transaction:
//			// handle config events
//			switch event.Kind() {
//
//			}
//		}
//	}
//}
//
//func (p *ChangeGroup) fatal(err error) {
//	// parse each subsystem config and send events to the appropriate event bus
//	event := eventsbus2.NewEvent(api.EventFatalError, api.SubSystemAll, []byte(err.Deny()))
//	// fire an event
//	p.eb.Send(context.Background(), event)
//}
