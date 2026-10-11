// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	lua "github.com/wippyai/go-lua"
)

func TestSQLTransportErrorClassification(t *testing.T) {
	cases := []struct {
		name      string
		cause     error
		kind      lua.Kind
		retryable lua.Ternary
	}{
		{"bad connection", driver.ErrBadConn, lua.Unavailable, lua.TernaryTrue},
		{"connection closed", io.EOF, lua.Unavailable, lua.TernaryTrue},
		{"incomplete response", io.ErrUnexpectedEOF, lua.Unavailable, lua.TernaryFalse},
		{"network", &net.OpError{Op: "read", Net: "tcp", Err: errors.New("opaque transport failure")}, lua.Unavailable, lua.TernaryFalse},
		{"timeout", &net.OpError{Op: "read", Net: "tcp", Err: context.DeadlineExceeded}, lua.Timeout, lua.TernaryFalse},
		{"canceled", context.Canceled, lua.Canceled, lua.TernaryFalse},
		{"deadline", context.DeadlineExceeded, lua.Timeout, lua.TernaryFalse},
		{"unknown", errors.New("SQLSTATE 23505: database locked"), lua.Unknown, lua.TernaryUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()
			wrapped := wrapSQLError(l, fmt.Errorf("driver wrapper: %w", tc.cause), "query")
			if wrapped.Kind() != tc.kind || wrapped.Retryable() != tc.retryable {
				t.Fatalf("kind/retryable = %q/%v, want %q/%v", wrapped.Kind(), wrapped.Retryable(), tc.kind, tc.retryable)
			}
			if !errors.Is(wrapped, tc.cause) {
				t.Fatal("native cause is lost")
			}
		})
	}
}
