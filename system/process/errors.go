package process

import (
	"strconv"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/dispatcher"
	apierror "github.com/wippyai/runtime/api/error"
	apiprocess "github.com/wippyai/runtime/api/process"
)

var (
	ErrTerminated = apiprocess.NewError(apiprocess.Internal, "process terminated")
)

// UnknownCommandError indicates an unregistered command.
type UnknownCommandError struct {
	CmdID   dispatcher.CommandID
	details attrs.Attributes
}

// NewUnknownCommandError creates an error for unregistered commands.
func NewUnknownCommandError(cmdID dispatcher.CommandID) *UnknownCommandError {
	return &UnknownCommandError{
		CmdID:   cmdID,
		details: attrs.NewBagFrom(map[string]any{"command_id": int(cmdID)}),
	}
}

func (e *UnknownCommandError) Error() string {
	return "unknown command: " + strconv.Itoa(int(e.CmdID))
}

func (e *UnknownCommandError) Kind() apierror.Kind {
	return apierror.NotFound
}

func (e *UnknownCommandError) Retryable() apierror.Ternary {
	return apierror.False
}

func (e *UnknownCommandError) Details() attrs.Attributes {
	return e.details
}

// NewFactoryNotFoundError creates an error for missing factory.
func NewFactoryNotFoundError(factoryID string) *apiprocess.Error {
	return apiprocess.NewError(apiprocess.NotFound, "no factory registered for: "+factoryID).
		WithDetails(attrs.NewBagFrom(map[string]any{"factory_id": factoryID}))
}

// NewHostNotFoundError creates an error for missing host.
func NewHostNotFoundError(hostID string) *apiprocess.Error {
	return apiprocess.NewError(apiprocess.NotFound, "host not found: "+hostID).
		WithDetails(attrs.NewBagFrom(map[string]any{"host_id": hostID}))
}

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
