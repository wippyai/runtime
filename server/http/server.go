package http

import (
	"context"
	"log"
	"net/http"

	"github.com/ponyruntime/pony/component"

	"github.com/ponyruntime/pony/api"
	"github.com/ponyruntime/pony/exec"
	"go.uber.org/zap"
)

const Component api.Component = "http"

type Server struct {
	log    *zap.Logger
	queue  *exec.Queue
	server *http.Server
}

func NewComponent(log *zap.Logger) *Server {
	return &Server{
		log:    log,
		server: &http.Server{},
	}
}

func (s *Server) Register(ctx context.Context, ev api.Event, state component.State) (component.State, error) {
	if state == nil {
		return nil, nil
	}
	log.Println("Registering HTTP server")
	// processing all sorts of events to boot state

	return nil, nil
}

func (s *Server) Apply(ctx context.Context, state component.State) error {
	//hst, ok := state.(*httpState)

	// processing all sorts of events to boot state

	return nil
}

func (s *Server) Start(ctx context.Context, queue *exec.Queue) {
	s.queue = queue
	s.log.Debug("activating server routines")
}

func (s *Server) Stop(ctx context.Context) {
	s.log.Debug("stopping server routines")
}

//
//type Endpoint interface {
//	Configure(cfg *api.JSONConfiguration)
//	ServeHTTP(w http.ResponseWriter, r *http.Request)
//	start()
//	stop(ctx context.Context)
//}

//
//// Configure uses JSONConfiguration to configure the endpoint
//func (e *Server) Configure(cfg *api.JSONConfiguration) {
//	e.log.Info("http: configuring the endpoint")
//
//	for name, v := range cfg.Servers {
//		e.log.Info("http: configuring server", zap.String("name", name), zap.String("type", v.Type), zap.String("address", v.Address))
//		switch v.Type {
//		// we're particularly interested in the HTTP endpoint
//		case "http":
//			e.server.Addr = v.Address
//		default:
//			e.log.Warn("http: skipping other endpoint types", zap.String("type", v.Type))
//		}
//	}
//
//	// TODO: pipeline should be attached here, ServeHTTP is the last step
//	e.server.Handler = e
//}
//
//func (e *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
//	e.log.Info("http: request received", zap.String("path", r.URL.Path))
//	// TODO: futures with proper:
//	// 1. Error handling
//	// 2. Timeouts and retries
//	// 3. Interceptors/additional callbacks support (e.g. logging)
//
//	// lock
//	// switch / map -> switch routes -> check id + src from config
//	// query args
//	//
//
//	// todo: rewrite and encapsulate into pipeline
//
//	// TODO: we should parse a body here: body + query
//	data, err := io.ReadAll(r.Body)
//	if err != nil {
//		// WARN: proper error handling
//		panic(err)
//	}
//
//	// TODO: get the args from query
//	q := r.URL.Query()
//
//	// from BODY: todo: read on demand inside endpoint pipeline
//	task := &api.Task{
//		App: "my-app-1",
//		// args, todo: redo
//		Payload: data,
//		Query:   q.Encode(), // todo: remove from here
//	}
//
//	// todo: inside pipeline
//	fut := e.queue.Await(context.Background(), task)
//
//	select {
//	case res := <-fut:
//		e.log.Info("http: task has been processed", zap.String("path", r.URL.Path))
//		if res.Error != nil {
//			e.log.Error("http: error processing the request", zap.Error(res.Error))
//			w.WriteHeader(http.StatusInternalServerError)
//			_, _ = w.Write([]byte("Internal component Error"))
//			return
//		}
//
//		w.WriteHeader(http.StatusOK)
//		_, _ = w.Write(res.Payload)
//	}
//}
//
//// start starts the HTTP server
//func (e *Server) start() {
//	e.log.Info("http: starting the server", zap.String("address", e.server.Addr))
//	go func() {
//		err := e.server.ListenAndServe()
//		if err != nil {
//			if errors.Is(err, http.ErrServerClosed) {
//				e.log.Info("http: server has been closed")
//			} else {
//				e.log.Error("http: server has stopped with error", zap.Error(err))
//			}
//		}
//	}()
//}
//
//func (e *Server) stop(ctx context.Context) {
//	e.log.Info("http: stopping the server")
//	_ = e.server.Shutdown(ctx)
//}
