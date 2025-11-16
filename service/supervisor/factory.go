package supervisor

import (
	"github.com/wippyai/runtime/api/registry"
	processApi "github.com/wippyai/runtime/api/service/supervisor"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/internal/uniqid"
)

// DefaultServiceFactory is the standard implementation of ServiceFactory
type DefaultServiceFactory struct {
	pidGen *uniqid.PIDGenerator
}

// NewDefaultServiceFactory creates a new DefaultServiceFactory
func NewDefaultServiceFactory(pidGen *uniqid.PIDGenerator) *DefaultServiceFactory {
	return &DefaultServiceFactory{
		pidGen: pidGen,
	}
}

// CreateService implements ServiceFactory interface
func (f *DefaultServiceFactory) CreateService(id registry.ID, config processApi.ServiceConfig) supervisor.Service {
	return &Service{
		id:     id,
		config: config,
		status: make(chan any, 1),
		pidGen: f.pidGen,
	}
}
