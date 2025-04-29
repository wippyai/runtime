package native

import (
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/service/exec"
	"go.uber.org/zap"
)

// ExecutorFactory creates native executor instances
type ExecutorFactory struct {
	log *zap.Logger
}

// NewExecutorFactory creates a new factory for native executors
func NewExecutorFactory(log *zap.Logger) *ExecutorFactory {
	return &ExecutorFactory{
		log: log,
	}
}

// CreateExecutor implements ExecutorFactoryAPI
func (f *ExecutorFactory) CreateExecutor(_ registry.ID, cfg *exec.NativeExecutorConfig) (exec.ProcessExecutor, error) {
	return NewNativeExecutor(f.log, cfg), nil
}
