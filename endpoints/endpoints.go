package endpoints

import (
	"context"
	"net/http"

	"github.com/ponyruntime/pony/api"
	httpEndp "github.com/ponyruntime/pony/endpoints/http"
	eventsbus "github.com/ponyruntime/pony/eventbus"
	"github.com/ponyruntime/pony/futures"
	"go.uber.org/zap"
)

type Endpoint interface {
	Configure(cfg *api.JSONConfiguration)
	ServeHTTP(w http.ResponseWriter, r *http.Request)
	Start()
	Stop(ctx context.Context)
}

// Endpoints ... TODO: add all components field here
type Endpoints struct {
	log               *zap.Logger
	queue             *futures.Queue
	internalEndpoints map[string]Endpoint
	evBusID           string
	eb                *eventsbus.Bus
}

func NewEndpoints(log *zap.Logger, queue *futures.Queue) *Endpoints {
	eb, id := eventsbus.GlobalEventBus()

	intEndp := make(map[string]Endpoint, 10)
	// TODO: better init + to const
	intEndp["http"] = httpEndp.NewHttpEndpoint(log.Named("http"), queue)

	return &Endpoints{
		internalEndpoints: intEndp,
		queue:             queue,
		log:               log,
		evBusID:           id,
		eb:                eb,
	}
}

func (r *Endpoints) ListenEvents() {
	evCh := make(chan api.Event, 10)
	// can't be an error here since we're provided all the data
	_ = r.eb.SubscribeP(
		context.Background(),
		r.evBusID,
		// listen to all events
		api.SubSystemEndpoints,
		api.EventsAll,
		evCh,
	)

	go func() {
		for event := range evCh {
			switch event.Subsystem() {
			// listen only for endpoint events
			case api.SubSystemEndpoints:
				switch event.Type() {
				case api.EventFatalError:
					r.log.Error("endpoints: received a fatal error event, stopping endpoints", zap.Any("message", event.Content()))

					for _, v := range r.internalEndpoints {
						v.Stop(context.Background())
					}
					return
					// stop event
				case api.EventStop:
					r.log.Info("endpoints: received a stop event, stopping endpoints")
					// map should be protected by RWMutex
					// lock here
					for endp, v := range r.internalEndpoints {
						r.log.Info("endpoints: stopping endpoint", zap.String("type", endp))
						// stop all servers
						v.Stop(context.Background())
					}
					return
				default:
					r.log.Info("endpoints: received an unknown event", zap.Any("type", event.Type()))
				}
				// broadcast events
			case api.SubSystemAll:
				switch event.Type() {
				case api.EventConfigurationUpdated: // todo: make http specific event types to setup
					// handle configuration update
					r.log.Debug("endpoints: received a configuration update event", zap.Any("content", event.Content()))
					// TODO: UNSAFE

					switch cfg := event.Content().(type) {
					case *api.JSONConfiguration: // todo: http configuration is specific to http service?
						for name, srv := range cfg.Servers {
							r.log.Info("endpoints: configuring server", zap.String("name", name), zap.String("type", srv.Type))
							// we only accept http apps in the http plugin
							switch srv.Type {
							// Init HTTP
							// TODO: add types to the api constants
							case "http":
								// TODO: builder?
								h := r.internalEndpoints[srv.Type]
								h.Configure(cfg)
								h.Start()
							}
						}
					default:
						r.log.Error("endpoints: received an invalid configuration event", zap.Any("content", event.Content()))
					}
				}
			}
		}
	}()
}
