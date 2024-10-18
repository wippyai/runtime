// Package runtime provides the runtime environment for the application.
// It knows about all underlying components and is responsible for their lifecycle.
package runtime

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ponyruntime/pony/api"
	eventsbus "github.com/ponyruntime/pony/eventbus"
	"github.com/ponyruntime/pony/futures"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	httpM "github.com/ponyruntime/pony/runtime/lua/modules/http"
	jsonM "github.com/ponyruntime/pony/runtime/lua/modules/json"
	"go.uber.org/zap"
)

// app is an internal representation of the application
// it should be re-created on the configuration update event
type app struct {
	id  string
	eng *engine.Engine
	cfg *api.App
}

// Runtime ... TODO: add all components field here
type Runtime struct {
	queue   *futures.Queue
	apps    map[string]*app
	stop    chan struct{}
	log     *zap.Logger
	evBusID string
	eb      *eventsbus.Bus
}

func NewRuntime(log *zap.Logger, queue *futures.Queue) *Runtime {
	eb, id := eventsbus.NewEventBus()
	return &Runtime{
		queue:   queue,
		stop:    make(chan struct{}, 1),
		apps:    make(map[string]*app),
		log:     log,
		evBusID: id,
		eb:      eb,
	}
}

func (r *Runtime) ListenEvents() {
	evCh := make(chan api.Event, 10)
	// can't be an error here since we're provided all the data
	_ = r.eb.SubscribeP(
		context.Background(),
		r.evBusID,
		api.SubSystemRuntime,
		api.EventsAll,
		evCh,
	)

	// listen for events
	go func() {
		for event := range evCh {
			switch event.SubSystem() {
			// broadcast events
			case api.SubSystemAll:
				switch event.Type() {
				// handle configuration event
				// On configuration update, we should do the following:
				// 1. Check the apps configuration, lock the runtime (not done)
				// 2. Update the apps configuration (not done)
				// 3. Enable new apps and open for the new events (not done)
				case api.EventConfigurationUpdated:
					// handle configuration update
					r.log.Debug("received a configuration update event", zap.Any("content", event.Content()))
					// TODO: enable subsystems according to the configuration, e.g.:
					// TODO: unsafe
					cfg := event.Content().(*api.JSONConfiguration)
					for id, acfg := range cfg.Apps {
						le := engine.NewLuaEngine(context.Background(), r.log.Named(id))
						// preload modules
						for _, ext := range acfg.Extensions {
							switch ext {
							case "http":
								le.L.PreloadModule("http", httpM.NewHTTPModule(&http.Client{}, r.log.Named(fmt.Sprintf("%s:%s", id, "http"))).Loader)
							case "json":
								le.L.PreloadModule("json", jsonM.Loader)
							default:
								r.log.Warn("unknown extension", zap.Any("extension", ext))
							}

							// create an app which would be used to handle requests from the endpoints
							// here should be lua pool
							acfg := &app{
								id:  acfg.ID,
								cfg: acfg,
								eng: le,
							}

							r.apps[id] = acfg
						}

					}
				}
			// listen only for the runtime events
			case api.SubSystemRuntime:
				// handle events
				switch event.Type() {
				case api.EventFatalError:
					r.log.Error("received a fatal error event", zap.Any("message", event.Content()))
					return
				default:
					r.log.Info("received an unknown event", zap.Any("type", event.Type()))
				}
			}
		}
	}()

	go func() {
		// start processing the queue
		r.Process()
	}()
}

func (r *Runtime) Process() {
	// TODO: we should be able to stop processing, it probably should be done in the Queue itself, it should close a channel on a broadcast stop event
	// BUT!!! we also need to track responses from all subsystems
	for v := range r.queue.All() {
		resp := &api.TaskResult{}
		v.Respond(resp)

	}
}

func (r *Runtime) Stop(ctx context.Context) {}
