// SPDX-License-Identifier: MPL-2.0

package native

import (
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/service/exec"
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
	if cfg.Confine != nil {
		if err := validateConfinementHost(cfg.Confine); err != nil {
			return nil, err
		}
	}
	executor := NewNativeExecutor(f.log, cfg)
	if cfg.Confine != nil {
		if err := executor.bindConfinementEntry(); err != nil {
			return nil, err
		}
	}
	return executor, nil
}
