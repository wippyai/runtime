package worker

import (
	"context"

	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	api "github.com/wippyai/runtime/api/service/temporal"
	"go.temporal.io/sdk/interceptor"
	"go.uber.org/zap"
)

// Factory creates Temporal workers
type Factory interface {
	CreateWorker(
		ctx context.Context,
		logger *zap.Logger,
		id registry.ID,
		config *api.WorkerConfig,
		resourceReg resource.Registry,
	) (*Worker, error)
}

// DefaultWorkerFactory is the default implementation of Factory
type DefaultWorkerFactory struct {
	envReg       env.Registry
	dtt          payload.Transcoder
	interceptors []interceptor.WorkerInterceptor
}

// NewDefaultWorkerFactory creates a new DefaultWorkerFactory
func NewDefaultWorkerFactory(envReg env.Registry, interceptors []interceptor.WorkerInterceptor, dtt payload.Transcoder) *DefaultWorkerFactory {
	return &DefaultWorkerFactory{
		envReg:       envReg,
		interceptors: interceptors,
		dtt:          dtt,
	}
}

// CreateWorker creates a new Worker instance
func (f *DefaultWorkerFactory) CreateWorker(
	_ context.Context,
	logger *zap.Logger,
	id registry.ID,
	config *api.WorkerConfig,
	resourceReg resource.Registry,
) (*Worker, error) {
	return NewWorkerBuilder().
		WithLogger(logger).
		WithID(id).
		WithConfig(config).
		WithResourceRegistry(resourceReg).
		WithEnvRegistry(f.envReg).
		WithInterceptors(f.interceptors).
		WithTranscoder(f.dtt).
		Build()
}
