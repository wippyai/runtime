package process

import (
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/dispatcher"
	apierror "github.com/wippyai/runtime/api/error"
	apiprocess "github.com/wippyai/runtime/api/process"
)

var (
	ErrTerminated = apierror.New(apiprocess.Internal, "process terminated").WithRetryable(apierror.False)
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
	return "unknown command"
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
func NewFactoryNotFoundError(factoryID string) apierror.Error {
	return apierror.New(apiprocess.NotFound, "factory not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"factory_id": factoryID}))
}

// NewHostNotFoundError creates an error for missing host.
func NewHostNotFoundError(hostID string) apierror.Error {
	return apierror.New(apiprocess.NotFound, "host not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"host_id": hostID}))
}

// NewInvalidFactoryEntryError creates an error for invalid factory entry.
func NewInvalidFactoryEntryError(factoryID string) apierror.Error {
	return apierror.New(apiprocess.Internal, "invalid factory entry").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"factory_id": factoryID}))
}

// NewProcessCreateError creates an error for process creation failures.
func NewProcessCreateError(err error) apierror.Error {
	return apierror.New(apiprocess.Internal, "failed to create process").
		WithCause(err).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()}))
}

// NewInvalidHostError creates an error for host that doesn't implement process.Host.
func NewInvalidHostError(hostID string) apierror.Error {
	return apierror.New(apiprocess.Internal, "host does not implement process.Host").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"host_id": hostID}))
}

// NewSubscriberError creates an error for event subscriber failures.
func NewSubscriberError(err error) apierror.Error {
	return apierror.New(apiprocess.Internal, "failed to create subscriber").
		WithCause(err).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()}))
}
