package http

import (
	"context"
	"errors"
	"net/http"

	"github.com/ponyruntime/pony/api"
	"github.com/ponyruntime/pony/futures"
	"go.uber.org/zap"
)

type Endpoint struct {
	log    *zap.Logger
	queue  *futures.Queue
	server *http.Server
}

func NewEndpoint(log *zap.Logger, queue *futures.Queue) *Endpoint {
	return &Endpoint{
		log:    log,
		queue:  queue,
		server: &http.Server{},
	}
}

// Configure uses JSONConfiguration to configure the endpoint
func (e *Endpoint) Configure(cfg *api.JSONConfiguration) {
	e.log.Info("http: configuring the endpoint")

	mux := http.NewServeMux()
	for name, v := range cfg.Servers {
		switch v.Type {
		// we're particularly interested in the HTTP endpoint
		case "http":
			e.server.Addr = v.Address
			mux.Handle(v.Path, e)
		default:
			e.log.Warn("http: skipping other endpoint types", zap.String("type", v.Type))
		}
	}

	e.server.Handler = mux
}

func (e *Endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.log.Info("http: request received", zap.String("path", r.URL.Path))
	// TODO: futures with proper:
	// 1. Error handling
	// 2. Timeouts and retries
	// 3. Interceptors/additional callbacks support (e.g. logging)

	// lock
	// switch / map -> switch routes -> check id + app from config
	// query args
	//

	callback := make(chan *api.TaskResult)

	// TODO: we should parse a body here: body + query

	// from BODY
	task := &api.Task{
		ID:       "http-id",
		App:      "http",
		Registry: &api.Registry{},
		Response: callback,
	}

	e.queue.Await(context.Background(), task)

	select {
	case res := <-callback:
		e.log.Info("http: task has been processed", zap.String("path", r.URL.Path))
		if res.Error != nil {
			e.log.Error("http: error processing the request", zap.Error(res.Error))
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Internal Server Error"))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(res.Payload)
	}
}

// Start starts the HTTP server
func (e *Endpoint) Start() {
	e.log.Info("http: starting the server")
	go func() {
		err := e.server.ListenAndServe()
		if err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				e.log.Info("http: server has been closed")
			} else {
				e.log.Error("http: server has stopped with error", zap.Error(err))
			}
		}
	}()
}

func (e *Endpoint) Stop(ctx context.Context) {
	e.log.Info("http: stopping the server")
	_ = e.server.Shutdown(ctx)
}
