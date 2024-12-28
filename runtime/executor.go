package runtime

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/pkg/eventbus"
	"go.uber.org/zap"
)

type Executor struct {
	ctx        context.Context
	logger     *zap.Logger
	bus        events.Bus
	handlers   sync.Map
	subscriber *eventbus.Subscriber
}

func NewExecutor(bus events.Bus, logger *zap.Logger) *Executor {
	return &Executor{
		bus:    bus,
		logger: logger,
	}
}

func (e *Executor) Start(ctx context.Context) error {
	e.ctx = ctx

	// Subscribe to executor events
	sub, err := eventbus.NewSubscriber(
		e.ctx,
		e.bus,
		runtime.System,
		"executor.*",
		e.handleEvent,
	)
	if err != nil {
		return fmt.Errorf("failed to create subscriber: %w", err)
	}
	e.subscriber = sub

	return nil
}

func (e *Executor) Stop() error {
	if e.subscriber != nil {
		e.subscriber.Close()
	}
	return nil
}

func (e *Executor) handleEvent(evt events.Event) {
	switch evt.Kind {

	case runtime.RegisterHandlerEvent:

		if data, ok := evt.Data.(runtime.RegisterHandler); ok {
			if data.Handler == nil {
				e.logger.Warn("handler is nil", zap.String("target", string(data.Target)))
				return
			}
			e.handlers.Store(data.Target, data.Handler)
			e.logger.Debug("handler registered",
				zap.String("target", string(data.Target)))
		}
	case runtime.DeleteHandlerEvent:
		if data, ok := evt.Data.(runtime.DeleteHandler); ok {
			e.handlers.Delete(data.Target)
			e.logger.Debug("handler removed",
				zap.String("target", string(data.Target)))
		}
	}
}

func (e *Executor) Execute(task runtime.Task) (chan *runtime.Result, error) {
	handler, exists := e.handlers.Load(task.Target)
	if !exists {
		return nil, fmt.Errorf("no handler registered for target: %s", task.Target)
	}

	if execHandler, ok := handler.(runtime.ExecutorHandler); ok {
		resultChan, err := execHandler(task)
		if err != nil {
			return nil, fmt.Errorf("handler execution failed: %w", err)
		}

		return resultChan, nil
	}

	return nil, fmt.Errorf("invalid handler type for target: %s", task.Target)
}
