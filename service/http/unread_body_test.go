// SPDX-License-Identifier: MPL-2.0

package http

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	config "github.com/wippyai/runtime/api/service/http"
)

func TestEndpointClosingConnectionWithUnreadBody(t *testing.T) {
	for _, bodySize := range []int{64 << 10, 512 << 10} {
		for _, consumed := range []int64{0, 3} {
			t.Run(fmt.Sprintf("size=%d/consumed=%d", bodySize, consumed), func(t *testing.T) {
				functions := NewSimpleFunctionRegistry()
				called, release := make(chan struct{}), make(chan struct{})
				id := registry.NewID("test", "ignore_body")
				functions.Register(id, func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
					close(called)
					<-release
					if consumed > 0 {
						request, ok := config.GetRequestContext(ctx)
						if !ok {
							return nil, fmt.Errorf("missing request context")
						}
						if _, err := io.CopyN(io.Discard, request.Request().Body, consumed); err != nil {
							return nil, err
						}
					}
					return &runtime.Result{Value: payload.New([]byte("success"))}, nil
				})
				factory, err := NewEndpointFactory(functions)
				require.NoError(t, err)
				handler, err := factory.CreateHandler(context.Background(), &config.EndpointConfig{
					Meta: map[string]any{config.RouterID: "test:router"}, Method: "POST", Path: "/", Func: id,
				})
				require.NoError(t, err)
				address := startUnreadBodyServer(t, handler)
				conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(t.Context(), "tcp", address)
				require.NoError(t, err)
				defer conn.Close()
				require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
				body := strings.Repeat("x", bodySize)
				_, err = fmt.Fprintf(conn, "POST / HTTP/1.1\r\nHost: example.test\r\nConnection: close\r\nContent-Length: %d\r\n\r\n", len(body))
				require.NoError(t, err)
				select {
				case <-called:
				case <-time.After(5 * time.Second):
					close(release)
					t.Fatal("endpoint does not receive request headers")
				}
				_, err = io.WriteString(conn, body)
				close(release)
				require.NoError(t, err)
				response, err := io.ReadAll(conn)
				require.NoError(t, err, "closing a completed response must not reset the connection")
				require.Contains(t, string(response), "200 OK")
				require.True(t, strings.HasSuffix(string(response), "success"), string(response))
			})
		}
	}
}

func TestEndpointUnreadBodyDoesNotDelayResponseUntilClientSends(t *testing.T) {
	functions := NewSimpleFunctionRegistry()
	id := registry.NewID("test", "ignore_body")
	functions.Register(id, func(context.Context, runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{Value: payload.New([]byte("success"))}, nil
	})
	factory, err := NewEndpointFactory(functions)
	require.NoError(t, err)
	handler, err := factory.CreateHandler(context.Background(), &config.EndpointConfig{
		Meta: map[string]any{config.RouterID: "test:router"}, Method: "POST", Path: "/", Func: id,
	})
	require.NoError(t, err)
	address := startUnreadBodyServer(t, handler)
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(t.Context(), "tcp", address)
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetDeadline(time.Now().Add(2*time.Second)))
	_, err = io.WriteString(conn, "POST / HTTP/1.1\r\nHost: example.test\r\nConnection: close\r\nContent-Length: 2\r\n\r\n")
	require.NoError(t, err)
	response, err := io.ReadAll(conn)
	require.NoError(t, err, "a client that withholds the unused body must still receive the response")
	require.Contains(t, string(response), "200 OK")
	require.True(t, strings.HasSuffix(string(response), "success"), string(response))
}

func startUnreadBodyServer(t *testing.T, handler http.Handler) string {
	t.Helper()
	server := makeServer(t, &config.ServerConfig{Addr: "127.0.0.1:0", Timeouts: config.TimeoutConfig{
		ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
	}})
	server.SetHandlerFunc(handler)
	status, err := server.Start(overlayCtx())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Stop(context.Background())) })
	select {
	case message := <-status:
		address, ok := message.(string)
		require.True(t, ok)
		return strings.TrimPrefix(address, "service listening on ")
	case <-time.After(5 * time.Second):
		t.Fatal("server does not report its listening address")
		return ""
	}
}
