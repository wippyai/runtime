package sockets

import (
	"context"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

const TCPCreateSocketNamespace = "wasi:sockets/tcp-create-socket@0.2.0"

// NetworkError represents a WASI network error.
type NetworkError struct {
	Code NetworkErrorCode
}

type NetworkErrorCode uint8

const (
	NetworkErrorUnknown NetworkErrorCode = iota
	NetworkErrorAccessDenied
	NetworkErrorNotSupported
	NetworkErrorInvalidArgument
	NetworkErrorOutOfMemory
	NetworkErrorTimeout
	NetworkErrorConcurrencyConflict
	NetworkErrorNotInProgress
	NetworkErrorWouldBlock
	NetworkErrorInvalidState
	NetworkErrorNewSocketLimit
	NetworkErrorAddressNotBindable
	NetworkErrorAddressInUse
	NetworkErrorRemoteUnreachable
	NetworkErrorConnectionRefused
	NetworkErrorConnectionReset
	NetworkErrorConnectionAborted
	NetworkErrorDatagramTooLarge
	NetworkErrorNameUnresolvable
	NetworkErrorTemporaryResolverFailure
	NetworkErrorPermanentResolverFailure
)

func (e *NetworkError) Error() string {
	return "network error"
}

const (
	AddressFamilyIPv4 uint8 = 0
	AddressFamilyIPv6 uint8 = 1
)

// TCPCreateSocketHost implements wasi:sockets/tcp-create-socket@0.2.0.
type TCPCreateSocketHost struct {
	resources *preview2.ResourceTable
}

func NewTCPCreateSocketHost(resources *preview2.ResourceTable) *TCPCreateSocketHost {
	return &TCPCreateSocketHost{resources: resources}
}

func (h *TCPCreateSocketHost) Namespace() string {
	return TCPCreateSocketNamespace
}

func (h *TCPCreateSocketHost) CreateTCPSocket(_ context.Context, addressFamily uint8) (uint32, *NetworkError) {
	if addressFamily != AddressFamilyIPv4 && addressFamily != AddressFamilyIPv6 {
		return 0, &NetworkError{Code: NetworkErrorInvalidArgument}
	}

	socket := preview2.NewTCPSocketResource(addressFamily)
	handle := h.resources.Add(socket)
	return handle, nil
}

func (h *TCPCreateSocketHost) Register() map[string]any {
	return map[string]any{
		"create-tcp-socket": h.CreateTCPSocket,
	}
}
