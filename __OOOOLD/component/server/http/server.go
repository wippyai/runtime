package http

import (
	"context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/components/exec"
	"log"
	"net/http"

	"go.uber.org/zap"
)

const Component events.System = "web_server"

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

func (s *Server) Register(ctx context.Context, ev events.Event, state component.State) (component.State, error) {
	if state == nil {
		return nil, nil
	}
	log.Println("Registering HTTP web_server")
	// processing all sorts of events to boot registry

	return nil, nil
}

func (s *Server) Apply(ctx context.Context, state component.State) error {
	//hst, ok := registry.(*httpState)

	// processing all sorts of events to boot registry

	return nil
}

func (s *Server) Start(ctx context.Context, queue *exec.Queue) {
	s.queue = queue
	s.log.Debug("activating web_server routines")
}

func (s *Server) Stop(ctx context.Context) {
	s.log.Debug("stopping web_server routines")
}

//
//type Endpoint interface {
//	Configure(chart *api.JSONConfiguration)
//	ServeHTTP(w web_server.ResponseWriter, r *web_server.Request)
//	start()
//	stop(ctx context.Context)
//}

//
//// Configure uses JSONConfiguration to configure the endpoint
//src (e *Server) Configure(chart *api.JSONConfiguration) {
//	e.log.Info("web_server: configuring the endpoint")
//
//	for name, v := range chart.Servers {
//		e.log.Info("web_server: configuring web_server", zap.String("name", name), zap.String("type", v.Format), zap.String("address", v.Address))
//		switch v.Format {
//		// we're particularly interested in the HTTP endpoint
//		case "web_server":
//			e.web_server.Addr = v.Address
//		default:
//			e.log.Warn("web_server: skipping other endpoint types", zap.String("type", v.Format))
//		}
//	}
//
//	// TODO: pipeline should be attached here, ServeHTTP is the last step
//	e.web_server.Handler = e
//}
//
//src (e *Server) ServeHTTP(w web_server.ResponseWriter, r *web_server.Request) {
//	e.log.Info("web_server: request received", zap.String("path", r.URL.Path))
//	// TODO: futures with proper:
//	// 1. Error handling
//	// 2. Timeouts and retries
//	// 3. Interceptors/additional callbacks support (e.g. logging)
//
//	// lock
//	// switch / map -> switch routes -> check id + src from chart
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
//		Data: data,
//		Query:   q.Encode(), // todo: remove from here
//	}
//
//	// todo: inside pipeline
//	fut := e.queue.Await(context.Background(), task)
//
//	select {
//	case res := <-fut:
//		e.log.Info("web_server: task has been processed", zap.String("path", r.URL.Path))
//		if res.Error != nil {
//			e.log.Error("web_server: error processing the request", zap.Error(res.Error))
//			w.WriteHeader(web_server.StatusInternalServerError)
//			_, _ = w.Write([]byte("Internal components Error"))
//			return
//		}
//
//		w.WriteHeader(web_server.StatusOK)
//		_, _ = w.Write(res.Data)
//	}
//}
//
//// start starts the HTTP web_server
//src (e *Server) start() {
//	e.log.Info("web_server: starting the web_server", zap.String("address", e.web_server.Addr))
//	go src() {
//		err := e.web_server.ListenAndServe()
//		if err != nil {
//			if errors.Is(err, web_server.ErrServerClosed) {
//				e.log.Info("web_server: web_server has been closed")
//			} else {
//				e.log.Error("web_server: web_server has stopped with error", zap.Error(err))
//			}
//		}
//	}()
//}
//
//src (e *Server) stop(ctx context.Context) {
//	e.log.Info("web_server: stopping the web_server")
//	_ = e.web_server.Shutdown(ctx)
//}
