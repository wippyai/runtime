package temporal

import (
	"context"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/service/temporal"
	"github.com/ponyruntime/pony/api/supervisor"
	"go.uber.org/zap"
)

type WorkerHostAPI interface {
	pubsub.Host
	process.Delegated
	ID() registry.ID
	Update(*temporal.TaskQueueRegistration) error
	Start(context.Context) (<-chan any, error)
	Stop(context.Context) error

	RegisterWorkflow(ctx context.Context, registration *temporal.WorkflowRegistration) error
	DeleteWorkflowByName(ctx context.Context, workflowName string) error
	RegisterActivity(ctx context.Context, registration *temporal.ActivityRegistration) error
	DeleteActivityByName(ctx context.Context, activityName string) error
}

// Ensure WorkerHost implements required interfaces
var (
	_ process.Delegated  = (WorkerHostAPI)(nil)
	_ pubsub.Host        = (WorkerHostAPI)(nil)
	_ supervisor.Service = (WorkerHostAPI)(nil)
)

// HostFactory defines an interface for creating task queue hosts
type HostFactory interface {
	CreateHost(config *temporal.TaskQueueRegistration) (WorkerHostAPI, error)
}

// DefaultFactory is the standard implementation of HostFactory
type DefaultFactory struct {
	logger *zap.Logger
}

// NewDefaultHostFactory creates a new DefaultFactory with the provided logger
func NewDefaultHostFactory(logger *zap.Logger) *DefaultFactory {
	return &DefaultFactory{
		logger: logger,
	}
}

func (f *DefaultFactory) CreateHost(
	config *temporal.TaskQueueRegistration,
) (WorkerHostAPI, error) {
	// Create a new task queue host with provided configuration
	host := NewTaskQueueHost(config, f.logger)
	return host, nil
}
