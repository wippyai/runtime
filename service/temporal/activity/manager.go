package activity

import (
	"context"
	"github.com/ponyruntime/pony/api/runtime"
	"go.uber.org/zap"
)

type Manager struct {
	log      *zap.Logger
	executor runtime.Executor
}

func NewActivityManager(
	log *zap.Logger,
	executor runtime.Executor,
) *Manager {
	return &Manager{
		log:      log,
		executor: executor,
	}
}

func (m *Manager) Execute(ctx context.Context) error {
	return nil
}
