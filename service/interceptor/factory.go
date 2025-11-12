package interceptor

import (
	"github.com/ponyruntime/pony/api/event"
	"go.uber.org/zap"
)

// FactoryAPI defines the interface for creating interceptor managers
type FactoryAPI interface {
	CreateManager(bus event.Bus, logger *zap.Logger) *Manager
}

// DefaultFactory is the default implementation of FactoryAPI
type DefaultFactory struct{}

// NewDefaultFactory creates a new default factory
func NewDefaultFactory() FactoryAPI {
	return &DefaultFactory{}
}

// CreateManager implements FactoryAPI
func (f *DefaultFactory) CreateManager(bus event.Bus, logger *zap.Logger) *Manager {
	return NewManager(bus, logger)
}
