// Package runtime provides the runtime environment for the application.
// It knows about all underlying components and is responsible for their lifecycle.
package runtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/internal"
	"github.com/ponyruntime/pony/components/exec"
	"net/http"

	"github.com/ponyruntime/pony/api"
	eventsbus2 "github.com/ponyruntime/pony/components/events"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	jsonM "github.com/ponyruntime/pony/runtime/lua/modules/json"
	httpM "github.com/ponyruntime/pony/runtime/lua/modules/web_server"
	"go.uber.org/zap"
)

// app is an internal representation of the application
// it should be re-created on the chart update events
type app struct {
	id   string
	eng  *engine.Engine
	cfg  *api.App
	code string // todo: isolate
}

// Runtime ... TODO: add all components field here
type Runtime struct {
	queue   *exec.Queue
	apps    map[string]*app
	stop    chan struct{}
	log     *zap.Logger
	evBusID string
	eb      *eventsbus2.Bus
}

func NewHub(log *zap.Logger, queue *exec.Queue) *Runtime {
	eb, id := eventsbus2.GlobalEventBus()
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
	evCh := make(chan events.Event, 10)
	// can't be an error here since we're provided all the data
	_ = r.eb.SubscribeP(
		context.Background(),
		r.evBusID,
		api.Servers,
		api.EventsAll,
		evCh,
	)

	// todo: must not contain anything about lua

	// listen for events
	go func() {
		for event := range evCh {
			switch event.System() {
			// broadcast events
			case api.All:
				switch event.Kind() {
				// handle chart events
				// On chart update, we should do the following:
				// 1. Check the apps chart, lock the runtime (not done)
				// 2. Update the apps chart (not done)
				// 3. Enable new apps and open for the new events (not done)
				case api.EventConfigurationUpdated:
					// handle chart update
					r.log.Debug("received a chart update events", zap.Any("content", event.Payload()))
					// TODO: enable subsystems according to the chart, e.g.:
					// TODO: unsafe
					// TODO: change to type selection
					cfg := event.Payload().Data().(*api.JSONConfiguration)
					for id, acfg := range cfg.Apps {
						le := engine.NewLuaEngine(context.Background(), r.log.Named(id))

						// preload modules
						for _, ext := range acfg.Extensions {
							r.log.Debug("preloading module", zap.Any("extension", ext))
							// todo: muse be isolated and dynamic
							switch ext {
							case "web_server":
								le.L.PreloadModule("web_server", httpM.NewHTTPModule(&http.Client{}, r.log.Named(fmt.Sprintf("%s:%s", id, "web_server"))).Loader)
							case "json":
								le.L.PreloadModule("json", jsonM.Loader)
							default:
								r.log.Warn("unknown extension", zap.Any("extension", ext))
							}

							// base64 decode of code
							code, err := base64.StdEncoding.DecodeString(acfg.SourceCode)
							if err != nil {
								r.log.Error("failed to decode source code", zap.Error(err))
								continue
							}

							// create an src which would be used to handle requests from the web_server
							// here should be lua pool
							lease := &app{
								id:   acfg.ID,
								cfg:  acfg,
								eng:  le,
								code: string(code),
							}

							r.apps[id] = lease
						}

					}
				}
			// listen only for the runtime events
			case api.Servers:
				// handle events
				switch event.Kind() {
				case api.EventFatalError:
					r.log.Error("received a fatal error events", zap.Any("message", event.Payload()))
					return
				default:
					r.log.Info("received an unknown events", zap.Any("type", event.Kind()))
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
	// TODO: we should be able to stop processing, it probably should be done in the Queue itself, it should close a channel on a broadcast stop events
	// BUT!!! we also need to track responses from all subsystems
	// todo: assuming it can be run in multiple coroutines
	for v := range r.queue.All() { // todo: redo using select
		resp := &internal.TaskResult{}
		r.log.Debug("processing a task", zap.Any("task", v))

		// todo: expect some routing from the handler side
		app := r.apps[v.App]

		err := app.eng.DoString(app.code, "handler_code")
		if err != nil {
			r.log.Error("failed to tasks the handler", zap.Error(err))
			v.Respond(&internal.TaskResult{
				Error: err,
			})
			continue
		}

		tres := app.eng.Get(-1)
		// we have a function, need to call it
		if tres.Type() == lua.LTFunction {
			// push the function to the lua stack
			app.eng.L.Push(tres)
			// call the function with the argument
			err = app.eng.L.PCall(0, 1, nil)
			if err != nil {
				r.log.Error("failed to tasks PCall", zap.Error(err))
				v.Respond(&internal.TaskResult{
					Error: err,
				})
				continue
			}

			// we should not forget to get the result of the function
			if app.eng.L.GetTop() != 0 {
				// here is we overwrite the tres variable
				tres = app.eng.Get(-1)
			} else {
				r.log.Warn("no result from the function call")
				tres = lua.LNil
			}
		}

		// we should not Pop values if there are no values on the Lua stack
		if app.eng.L.GetTop() != 0 {
			app.eng.Pop(1)
		}

		// todo: protect as well (Top)
		result := engine.ToGoAny(tres)

		// Convert the result back to JSON
		jsonResult, err := json.Marshal(result)
		if err != nil {
			v.Respond(&internal.TaskResult{
				Error: err,
			})
			continue
		}

		resp.Payload = jsonResult

		v.Respond(resp)
	}
}

func (r *Runtime) Stop(ctx context.Context) {}
