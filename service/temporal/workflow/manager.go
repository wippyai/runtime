package workflow

import "go.uber.org/zap"

type Manager struct {
}

func NewWorkflowManager(log *zap.Logger) *Manager {
	return &Manager{}
}
