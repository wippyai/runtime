// Package queue provides queue command handlers for the dispatcher system.
package queue

import (
	"context"

	"github.com/wippyai/runtime/api/dispatcher"
	queueapi "github.com/wippyai/runtime/api/dispatcher/queue"
)

// QueuePublishHandler handles queue publish commands.
type QueuePublishHandler struct{}

func NewQueuePublishHandler() *QueuePublishHandler {
	return &QueuePublishHandler{}
}

func (h *QueuePublishHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	pubCmd := cmd.(*queueapi.QueuePublishCmd)

	err := pubCmd.Manager.Publish(ctx, pubCmd.QueueID, pubCmd.Message)
	emit(queueapi.QueuePublishResponse{Error: err})
	return nil
}

// Service bundles all queue handlers.
type Service struct {
	Publish *QueuePublishHandler
}

// NewService creates a new queue service with all handlers initialized.
func NewService() *Service {
	return &Service{
		Publish: NewQueuePublishHandler(),
	}
}

// RegisterAll registers all queue handlers with the given registry function.
func (s *Service) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(queueapi.CmdQueuePublish, s.Publish)
}
