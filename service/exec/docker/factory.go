// SPDX-License-Identifier: MPL-2.0

package docker

import (
	"github.com/wippyai/runtime/api/registry"
	execapi "github.com/wippyai/runtime/api/service/exec"
	"go.uber.org/zap"
)

// ExecutorFactory creates Docker executors
type ExecutorFactory struct {
	log *zap.Logger
}

// NewExecutorFactory creates a new Docker executor factory
func NewExecutorFactory(log *zap.Logger) *ExecutorFactory {
	return &ExecutorFactory{log: log}
}

// CreateExecutor creates a new Docker executor with the given configuration
func (f *ExecutorFactory) CreateExecutor(id registry.ID, cfg *execapi.DockerExecutorConfig) (execapi.ProcessExecutor, error) {
	return NewDockerExecutor(f.log.Named(id.String()), cfg)
}
