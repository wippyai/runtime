package sockets

import (
	"errors"
	"net"
	"os"
	"syscall"
)

// mapNetError converts Go net package errors to WASI network error codes.
func mapNetError(err error) *NetworkError {
	if err == nil {
		return nil
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return mapOpError(opErr)
	}

	var addrErr *net.AddrError
	if errors.As(err, &addrErr) {
		return &NetworkError{Code: NetworkErrorInvalidArgument}
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsTemporary {
			return &NetworkError{Code: NetworkErrorTemporaryResolverFailure}
		}
		if dnsErr.IsNotFound {
			return &NetworkError{Code: NetworkErrorNameUnresolvable}
		}
		return &NetworkError{Code: NetworkErrorPermanentResolverFailure}
	}

	if os.IsTimeout(err) {
		return &NetworkError{Code: NetworkErrorTimeout}
	}

	if os.IsPermission(err) {
		return &NetworkError{Code: NetworkErrorAccessDenied}
	}

	return &NetworkError{Code: NetworkErrorUnknown}
}

func mapOpError(opErr *net.OpError) *NetworkError {
	var errno syscall.Errno
	if errors.As(opErr.Err, &errno) {
		return mapErrno(errno)
	}

	if opErr.Timeout() {
		return &NetworkError{Code: NetworkErrorTimeout}
	}

	if opErr.Err != nil {
		switch opErr.Err.Error() {
		case "connection refused":
			return &NetworkError{Code: NetworkErrorConnectionRefused}
		case "connection reset":
			return &NetworkError{Code: NetworkErrorConnectionReset}
		case "connection reset by peer":
			return &NetworkError{Code: NetworkErrorConnectionReset}
		case "broken pipe":
			return &NetworkError{Code: NetworkErrorConnectionAborted}
		case "network is unreachable":
			return &NetworkError{Code: NetworkErrorRemoteUnreachable}
		case "host is unreachable":
			return &NetworkError{Code: NetworkErrorRemoteUnreachable}
		case "no route to host":
			return &NetworkError{Code: NetworkErrorRemoteUnreachable}
		}
	}

	return &NetworkError{Code: NetworkErrorUnknown}
}

func mapErrno(errno syscall.Errno) *NetworkError {
	switch errno {
	case syscall.EACCES, syscall.EPERM:
		return &NetworkError{Code: NetworkErrorAccessDenied}
	case syscall.EADDRINUSE:
		return &NetworkError{Code: NetworkErrorAddressInUse}
	case syscall.EADDRNOTAVAIL:
		return &NetworkError{Code: NetworkErrorAddressNotBindable}
	case syscall.ECONNREFUSED:
		return &NetworkError{Code: NetworkErrorConnectionRefused}
	case syscall.ECONNRESET:
		return &NetworkError{Code: NetworkErrorConnectionReset}
	case syscall.ECONNABORTED:
		return &NetworkError{Code: NetworkErrorConnectionAborted}
	case syscall.EHOSTUNREACH:
		return &NetworkError{Code: NetworkErrorRemoteUnreachable}
	case syscall.ENETUNREACH:
		return &NetworkError{Code: NetworkErrorRemoteUnreachable}
	case syscall.ETIMEDOUT:
		return &NetworkError{Code: NetworkErrorTimeout}
	case syscall.EINVAL:
		return &NetworkError{Code: NetworkErrorInvalidArgument}
	case syscall.ENOMEM:
		return &NetworkError{Code: NetworkErrorOutOfMemory}
	case syscall.EWOULDBLOCK:
		return &NetworkError{Code: NetworkErrorWouldBlock}
	case syscall.EINPROGRESS:
		return &NetworkError{Code: NetworkErrorWouldBlock}
	case syscall.EALREADY:
		return &NetworkError{Code: NetworkErrorConcurrencyConflict}
	case syscall.ENOTSOCK:
		return &NetworkError{Code: NetworkErrorInvalidState}
	case syscall.ENOTCONN:
		return &NetworkError{Code: NetworkErrorInvalidState}
	case syscall.EISCONN:
		return &NetworkError{Code: NetworkErrorInvalidState}
	case syscall.EMSGSIZE:
		return &NetworkError{Code: NetworkErrorDatagramTooLarge}
	case syscall.EMFILE, syscall.ENFILE:
		return &NetworkError{Code: NetworkErrorNewSocketLimit}
	default:
		return &NetworkError{Code: NetworkErrorUnknown}
	}
}

// isWouldBlock returns true if the error indicates the operation would block.
func isWouldBlock(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Timeout() {
			return true
		}
		var errno syscall.Errno
		if errors.As(opErr.Err, &errno) {
			return errno == syscall.EWOULDBLOCK || errno == syscall.EAGAIN || errno == syscall.EINPROGRESS
		}
	}
	return false
}
