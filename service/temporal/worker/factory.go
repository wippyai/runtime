package worker

import (
	"context"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	api "github.com/wippyai/runtime/api/service/temporal"
	"go.temporal.io/sdk/interceptor"
	"go.uber.org/zap"
)

// WorkerFactory creates Temporal workers
type WorkerFactory interface {
	CreateWorker(
		ctx context.Context,
		logger *zap.Logger,
		id registry.ID,
		config *api.WorkerConfig,
		resourceReg resource.Registry,
	) (*Worker, error)
}

// DefaultWorkerFactory is the default implementation of WorkerFactory
type DefaultWorkerFactory struct {
	interceptors []interceptor.WorkerInterceptor
}

// NewDefaultWorkerFactory creates a new DefaultWorkerFactory
func NewDefaultWorkerFactory(interceptors []interceptor.WorkerInterceptor) *DefaultWorkerFactory {
	return &DefaultWorkerFactory{
		interceptors: interceptors,
	}
}

// CreateWorker creates a new Worker instance
func (f *DefaultWorkerFactory) CreateWorker(
	ctx context.Context,
	logger *zap.Logger,
	id registry.ID,
	config *api.WorkerConfig,
	resourceReg resource.Registry,
) (*Worker, error) {
	return NewWorker(logger, id, config, resourceReg, f.interceptors), nil
}
