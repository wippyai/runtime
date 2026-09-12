// SPDX-License-Identifier: MPL-2.0

package http

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	contextapi "github.com/wippyai/runtime/api/context"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/http"
	"go.uber.org/zap"
)

type failingProbeService struct {
	listened chan struct{}
}

func serverHostHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fc := contextapi.FrameFromContext(r.Context())
		host := ""
		if fc != nil {
			fc.Iterate(func(key any, value any) {
				if contextKey, ok := key.(*contextapi.Key); ok && contextKey.Name == "http.server.host" {
					host, _ = value.(string)
				}
			})
		}
		_, _ = w.Write([]byte(host))
	})
}

func TestServerService_PortZeroPublishesBoundAddress(t *testing.T) {
	server, err := NewServerService(
		registry.NewID("test", "port-zero"),
		&config.ServerConfig{Addr: "127.0.0.1:0"},
		NewMiddlewareRegistry(zap.NewNop()),
	)
	require.NoError(t, err)
	server.SetHandlerFunc(serverHostHandler())
	client := &http.Client{Timeout: time.Second}
	ctx, cancel := context.WithCancel(overlayCtx())
	defer cancel()
	statusCh, err := server.Start(ctx)
	require.NoError(t, err)

	select {
	case details := <-statusCh:
		address, ok := details.(string)
		require.True(t, ok)
		require.True(t, strings.HasPrefix(address, "service listening on 127.0.0.1:"))
		require.NotContains(t, address, ":0")
		bound := strings.TrimPrefix(address, "service listening on ")
		response, requestErr := client.Get("http://" + bound)
		require.NoError(t, requestErr)
		body, readErr := io.ReadAll(response.Body)
		require.NoError(t, response.Body.Close())
		require.NoError(t, readErr)
		require.Equal(t, bound, string(body))
	case <-time.After(time.Second):
		t.Fatal("server did not report readiness")
	}
	cancel()
	deadline := time.After(time.Second)
	for server.started.Load() {
		select {
		case <-deadline:
			t.Fatal("server did not stop after context cancellation")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	require.NoError(t, server.Stop(context.Background()))
}

func TestServerService_WildcardPortZeroPreservesConfiguredAddress(t *testing.T) {
	server, err := NewServerService(
		registry.NewID("test", "wildcard-port-zero"),
		&config.ServerConfig{Addr: ":0"},
		NewMiddlewareRegistry(zap.NewNop()),
	)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(overlayCtx())
	defer cancel()
	statusCh, err := server.Start(ctx)
	require.NoError(t, err)

	select {
	case details := <-statusCh:
		address, ok := details.(string)
		require.True(t, ok)
		bound := strings.TrimPrefix(address, "service listening on ")
		_, port, splitErr := net.SplitHostPort(bound)
		require.NoError(t, splitErr)
		require.NotEqual(t, "0", port)
	case <-time.After(time.Second):
		t.Fatal("server did not report readiness")
	}
	require.Equal(t, ":0", server.config.Addr)
	require.NoError(t, server.Stop(context.Background()))
}

func TestServerService_NonZeroNamedHostPreserved(t *testing.T) {
	reserved, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	_, port, err := net.SplitHostPort(reserved.Addr().String())
	require.NoError(t, err)
	require.NoError(t, reserved.Close())
	configured := "localhost:" + port
	server, err := NewServerService(
		registry.NewID("test", "named-host"),
		&config.ServerConfig{Addr: configured},
		NewMiddlewareRegistry(zap.NewNop()),
	)
	require.NoError(t, err)
	server.SetHandlerFunc(serverHostHandler())
	client := &http.Client{Timeout: time.Second}
	ctx, cancel := context.WithCancel(overlayCtx())
	defer cancel()
	statusCh, err := server.Start(ctx)
	require.NoError(t, err)
	select {
	case details := <-statusCh:
		require.Equal(t, "service listening on "+configured, details)
		response, requestErr := client.Get("http://" + configured)
		require.NoError(t, requestErr)
		body, readErr := io.ReadAll(response.Body)
		require.NoError(t, response.Body.Close())
		require.NoError(t, readErr)
		require.Equal(t, configured, string(body))
	case <-time.After(time.Second):
		t.Fatal("server did not report readiness")
	}
	require.NoError(t, server.Stop(context.Background()))
}

func TestServerService_PortZeroRestartReportsCurrentAddress(t *testing.T) {
	server, err := NewServerService(
		registry.NewID("test", "port-zero-restart"),
		&config.ServerConfig{Addr: "127.0.0.1:0"},
		NewMiddlewareRegistry(zap.NewNop()),
	)
	require.NoError(t, err)
	server.SetHandlerFunc(serverHostHandler())
	client := &http.Client{Timeout: time.Second}

	start := func(ctx context.Context) string {
		statusCh, startErr := server.Start(ctx)
		require.NoError(t, startErr)
		select {
		case details := <-statusCh:
			address, ok := details.(string)
			require.True(t, ok)
			return strings.TrimPrefix(address, "service listening on ")
		case <-time.After(time.Second):
			t.Fatal("server did not report readiness")
			return ""
		}
	}
	ctx1, cancel1 := context.WithCancel(overlayCtx())
	address1 := start(ctx1)
	response, err := client.Get("http://" + address1)
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, address1, string(body))
	require.NoError(t, server.Stop(context.Background()))
	cancel1()
	require.Eventually(t, func() bool { return !server.started.Load() }, time.Second, 10*time.Millisecond)
	reserved, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", address1)
	require.NoError(t, err)
	defer reserved.Close()

	ctx2, cancel2 := context.WithCancel(overlayCtx())
	defer cancel2()
	address2 := start(ctx2)
	require.NotEqual(t, address1, address2)
	response, err = client.Get("http://" + address2)
	require.NoError(t, err)
	body, err = io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, address2, string(body))
	require.NoError(t, server.Stop(context.Background()))
}

func (s *failingProbeService) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, errors.New("injected probe failure")
}

func (s *failingProbeService) Listen(ctx context.Context, network, address string) (net.Listener, error) {
	ln, err := (&net.ListenConfig{}).Listen(ctx, network, address)
	if err == nil {
		close(s.listened)
	}
	return ln, err
}

func (s *failingProbeService) ListenPacket(ctx context.Context, network, address string) (net.PacketConn, error) {
	return (&net.ListenConfig{}).ListenPacket(ctx, network, address)
}

func (s *failingProbeService) LookupHost(ctx context.Context, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}

func TestN01ServerProbeFailureRollsBack(t *testing.T) {
	reserved, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := reserved.Addr().String()
	require.NoError(t, reserved.Close())

	networkID := registry.NewID("test", "startup-probe")
	svc := &failingProbeService{listened: make(chan struct{})}
	reg := newMockNetRegistry()
	reg.register(networkID.String(), svc, netapi.KindI2P)

	server, err := NewServerService(
		registry.NewID("test", "startup-rollback"),
		&config.ServerConfig{Addr: addr, Network: networkID},
		NewMiddlewareRegistry(zap.NewNop()),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(overlayCtxWithRegistry(reg))
	startDone := make(chan error, 1)
	go func() {
		_, err := server.Start(ctx)
		startDone <- err
	}()

	select {
	case <-svc.listened:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("listener was not created")
	}

	select {
	case err := <-startDone:
		require.Error(t, err)
		require.Contains(t, err.Error(), "startup")
	case <-time.After(time.Second):
		t.Fatal("Start did not return after probe cancellation")
	}
	defer func() { _ = server.Stop(context.Background()) }()

	require.False(t, server.started.Load(), "failed startup must reset started state")
	require.Nil(t, server.server, "failed startup must discard the server")

	rebound, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", addr)
	require.NoError(t, err, "failed startup left its listener bound")
	require.NoError(t, rebound.Close())
}
