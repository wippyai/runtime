package process

import (
	"github.com/wippyai/runtime/api/attrs"
	apiprocess "github.com/wippyai/runtime/api/process"
)

var (
	ErrTerminated = apiprocess.NewError(apiprocess.Internal, "process terminated")
)

// NewInvalidFactoryEntryError creates an error for invalid factory entry.
func NewInvalidFactoryEntryError(factoryID string) *apiprocess.Error {
	return apiprocess.NewError(apiprocess.Internal, "invalid factory entry for: "+factoryID).
		WithDetails(attrs.NewBagFrom(map[string]any{"factory_id": factoryID}))
}

// NewProcessCreateError creates an error for process creation failures.
func NewProcessCreateError(err error) *apiprocess.Error {
	return apiprocess.NewError(apiprocess.Internal, "failed to create process").WithCause(err)
}

// NewInvalidHostError creates an error for host that doesn't implement process.Host.
func NewInvalidHostError(hostID string) *apiprocess.Error {
	return apiprocess.NewError(apiprocess.Internal, "host "+hostID+" does not implement process.Host").
		WithDetails(attrs.NewBagFrom(map[string]any{"host_id": hostID}))
}

// NewSubscriberError creates an error for event subscriber failures.
func NewSubscriberError(err error) *apiprocess.Error {
	return apiprocess.NewError(apiprocess.Internal, "failed to create subscriber").WithCause(err)
}
