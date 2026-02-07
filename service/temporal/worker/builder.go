package worker

import (
	"fmt"

	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	api "github.com/wippyai/runtime/api/service/temporal"
	"go.temporal.io/sdk/interceptor"
	"go.uber.org/zap"
)

// WorkerBuilder configures and constructs Worker instances.
//
//nolint:revive // keep explicit API naming for external callers.
type WorkerBuilder struct {
	resourceReg  resource.Registry
	envReg       env.Registry
	dtt          payload.Transcoder
	logger       *zap.Logger
	config       *api.WorkerConfig
	id           registry.ID
	interceptors []interceptor.WorkerInterceptor
}

// NewWorkerBuilder creates a new WorkerBuilder.
func NewWorkerBuilder() *WorkerBuilder {
	return &WorkerBuilder{}
}

// WithLogger sets the logger used by the Worker.
func (b *WorkerBuilder) WithLogger(logger *zap.Logger) *WorkerBuilder {
	b.logger = logger
	return b
}

// WithID sets the registry ID for the Worker.
func (b *WorkerBuilder) WithID(id registry.ID) *WorkerBuilder {
	b.id = id
	return b
}

// WithConfig sets the Worker configuration.
func (b *WorkerBuilder) WithConfig(cfg *api.WorkerConfig) *WorkerBuilder {
	b.config = cfg
	return b
}

// WithResourceRegistry sets the resource registry.
func (b *WorkerBuilder) WithResourceRegistry(reg resource.Registry) *WorkerBuilder {
	b.resourceReg = reg
	return b
}

// WithEnvRegistry sets the environment registry.
func (b *WorkerBuilder) WithEnvRegistry(reg env.Registry) *WorkerBuilder {
	b.envReg = reg
	return b
}

// WithInterceptors sets worker interceptors.
func (b *WorkerBuilder) WithInterceptors(interceptors []interceptor.WorkerInterceptor) *WorkerBuilder {
	b.interceptors = interceptors
	return b
}

// WithTranscoder sets the payload transcoder.
func (b *WorkerBuilder) WithTranscoder(dtt payload.Transcoder) *WorkerBuilder {
	b.dtt = dtt
	return b
}

// Build creates a new Worker with the current configuration.
func (b *WorkerBuilder) Build() (*Worker, error) {
	if b.config == nil {
		return nil, fmt.Errorf("worker config is required")
	}
	if b.dtt == nil {
		return nil, fmt.Errorf("transcoder is required")
	}
	if b.logger == nil {
		b.logger = zap.NewNop()
	}

	return newWorker(
		b.logger,
		b.id,
		b.config,
		b.resourceReg,
		b.envReg,
		b.interceptors,
		b.dtt,
	), nil
}
