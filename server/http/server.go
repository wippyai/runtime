package http

import (
	"context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/tasks"
	registry "github.com/ponyruntime/pony/pkg/registry/listener"
	"go.uber.org/zap"
)

type Server struct {
	log      *zap.Logger
	listener registry.entryListener
	exec     tasks.Executor
	server   []serverWorker
	configCh []chan *registry.Operation
}

func NewComponent(bus events.Bus, log *zap.Logger) *Server {
	configCh := make(chan *registry.Operation, 10)
	//	listener := registry.eewEntryListener(bus, configCh)

	return &Server{
		log: log,

		//		server: &http.Server{},
	}
}

func (e *Server) Stop(ctx context.Context) {
	e.log.Info("http: stopping the server")
	e.listener.Close()

	//_ = e.server.Shutdown(ctx)
}
