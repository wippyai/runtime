// SPDX-License-Identifier: MPL-2.0

package sockets

import (
	"context"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"

	netapi "github.com/wippyai/runtime/api/net"
	socketapi "github.com/wippyai/runtime/api/socket"
	"github.com/wippyai/wasm-runtime/resource"
)

func TestS14NetworkErrorMappingInvariant(t *testing.T) {
	cases := []struct {
		mapError func() *NetworkError
		name     string
		want     NetworkErrorCode
	}{
		{
			name: "runtime access denied",
			mapError: func() *NetworkError {
				return mapNetError(fmt.Errorf("connect operation: %w", netapi.ErrAccessDenied))
			},
			want: NetworkErrorAccessDenied,
		},
		{
			name: "wrapped refused",
			mapError: func() *NetworkError {
				return mapNetError(fmt.Errorf("dial operation: %w", syscall.ECONNREFUSED))
			},
			want: NetworkErrorConnectionRefused,
		},
		{
			name: "wrapped timeout",
			mapError: func() *NetworkError {
				return mapNetError(fmt.Errorf("read operation: %w", os.ErrDeadlineExceeded))
			},
			want: NetworkErrorTimeout,
		},
		{
			name: "wrapped would-block operation",
			mapError: func() *NetworkError {
				return mapOpError(&net.OpError{Op: "read", Net: "udp", Err: fmt.Errorf("socket: %w", syscall.EWOULDBLOCK)})
			},
			want: NetworkErrorWouldBlock,
		},
		{
			name: "wrapped DNS not found",
			mapError: func() *NetworkError {
				return mapNetError(fmt.Errorf("lookup operation: %w", &net.DNSError{Name: "missing.invalid", IsNotFound: true}))
			},
			want: NetworkErrorNameUnresolvable,
		},
		{
			name: "literal reset errno",
			mapError: func() *NetworkError {
				return mapErrno(syscall.ECONNRESET)
			},
			want: NetworkErrorConnectionReset,
		},
	}

	for _, testCase := range cases {
		mapped := testCase.mapError()
		if mapped == nil || mapped.Code != testCase.want {
			t.Fatalf("%s mapped to %#v, want code %d", testCase.name, mapped, testCase.want)
		}
	}
}

func TestPendingNetworkErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want NetworkErrorCode
	}{
		{resource.ErrClosed, NetworkErrorInvalidState},
		{socketapi.ErrOperationClosed, NetworkErrorInvalidState},
		{socketapi.ErrAlreadyStarted, NetworkErrorConcurrencyConflict},
		{socketapi.ErrInvalidTimeout, NetworkErrorInvalidArgument},
		{context.Canceled, NetworkErrorConnectionAborted},
		{context.DeadlineExceeded, NetworkErrorTimeout},
	} {
		requireNetworkError(t, mapNetError(fmt.Errorf("pending operation: %w", tc.err)), tc.want)
	}
}
