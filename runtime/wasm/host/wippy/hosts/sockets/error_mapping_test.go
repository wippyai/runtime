// SPDX-License-Identifier: MPL-2.0

package sockets

import (
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
)

func TestS14NetworkErrorMappingInvariant(t *testing.T) {
	cases := []struct {
		mapError func() *NetworkError
		name     string
		want     NetworkErrorCode
	}{
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
