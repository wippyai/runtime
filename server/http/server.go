package http

import (
	"context"
	"errors"
	"github.com/ponyruntime/pony/server"
	"io"
	"net/http"

	"github.com/ponyruntime/pony/api"
	"github.com/ponyruntime/pony/exec"
	"go.uber.org/zap"
)

const Subsystem api.Component = "http"

type Endpoint interface {
	Configure(cfg *api.JSONConfiguration)
	ServeHTTP(w http.ResponseWriter, r *http.Request)
	Start()
	Stop(ctx context.Context)
}

type HttpServer struct {
	log    *zap.Logger
	queue  *exec.Queue
	server *http.Server
}

func NewSubsystem(log *zap.Logger) server.Subsystem {
	return server.Subsystem{
		Subsystem: Subsystem,
		Server:    newServer(log),
	}
}

func newServer(log *zap.Logger) HttpServer {
	return nil
}

func NewHttpEndpoint(log *zap.Logger, queue *exec.Queue) *HttpServer {
	return &HttpServer{
		log:    log,
		queue:  queue,
		server: &http.Server{},
	}
}

// Configure uses JSONConfiguration to configure the endpoint
func (e *HttpServer) Configure(cfg *api.JSONConfiguration) {
	e.log.Info("http: configuring the endpoint")

	for name, v := range cfg.Servers {
		e.log.Info("http: configuring server", zap.String("name", name), zap.String("type", v.Type), zap.String("address", v.Address))
		switch v.Type {
		// we're particularly interested in the HTTP endpoint
		case "http":
			e.server.Addr = v.Address
		default:
			e.log.Warn("http: skipping other endpoint types", zap.String("type", v.Type))
		}
	}

	// TODO: pipeline should be attached here, ServeHTTP is the last step
	e.server.Handler = e
}

func (e *HttpServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.log.Info("http: request received", zap.String("path", r.URL.Path))
	// TODO: futures with proper:
	// 1. Error handling
	// 2. Timeouts and retries
	// 3. Interceptors/additional callbacks support (e.g. logging)

	// lock
	// switch / map -> switch routes -> check id + src from config
	// query args
	//

	// todo: rewrite and encapsulate into pipeline

	// TODO: we should parse a body here: body + query
	data, err := io.ReadAll(r.Body)
	if err != nil {
		// WARN: proper error handling
		panic(err)
	}

	// TODO: get the args from query
	q := r.URL.Query()

	// from BODY: todo: read on demand inside endpoint pipeline
	task := &api.Task{
		App: "my-app-1",
		// args, todo: redo
		Payload: data,
		Query:   q.Encode(), // todo: remove from here
	}

	// todo: inside pipeline
	fut := e.queue.Await(context.Background(), task)

	select {
	case res := <-fut:
		e.log.Info("http: task has been processed", zap.String("path", r.URL.Path))
		if res.Error != nil {
			e.log.Error("http: error processing the request", zap.Error(res.Error))
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Internal Component Error"))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(res.Payload)
	}
}

// Start starts the HTTP server
func (e *HttpServer) Start() {
	e.log.Info("http: starting the server", zap.String("address", e.server.Addr))
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

func (e *HttpServer) Stop(ctx context.Context) {
	e.log.Info("http: stopping the server")
	_ = e.server.Shutdown(ctx)
}
