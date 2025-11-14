package supervisor

import (
	"github.com/wippyai/runtime/api/registry"
	processApi "github.com/wippyai/runtime/api/service/supervisor"
	"github.com/wippyai/runtime/api/supervisor"
)

// DefaultServiceFactory is the standard implementation of ServiceFactory
// that creates Service instances as they were created before
type DefaultServiceFactory struct{}

// NewDefaultServiceFactory creates a new DefaultServiceFactory
func NewDefaultServiceFactory() *DefaultServiceFactory {
	return &DefaultServiceFactory{}
}

// CreateService implements ServiceFactory interface
// Creates a service instance just like the original implementation
func (f *DefaultServiceFactory) CreateService(id registry.ID, config processApi.ServiceConfig) supervisor.Service {
	return &Service{
		id:     id,
		config: config,
		status: make(chan any, 1),
	}
}
