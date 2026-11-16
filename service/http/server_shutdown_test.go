// SPDX-License-Identifier: MPL-2.0

package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	contextapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	config "github.com/wippyai/runtime/api/service/http"
	"go.uber.org/zap"
)

func TestServerService_StopEndsRequestsThatWaitForShutdown(t *testing.T) {
	port, err := findFreePort()
	require.NoError(t, err)

	cfg := &config.ServerConfig{Addr: fmt.Sprintf("127.0.0.1:%d", port)}
	server, err := NewServerService(registry.NewID("test", "shutdown_server"), cfg, NewMiddlewareRegistry(zap.NewNop()))
	require.NoError(t, err)

	routerID := registry.NewID("test", "shutdown_router")
	require.NoError(t, server.UpsertRouter(routerID, &config.RouterConfig{Prefix: "/api"}))
	streaming := make(chan struct{})
	stream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		signal, _ := contextapi.FrameFromContext(r.Context()).Get(config.ServerShutdownKey())
		shuttingDown, _ := signal.(<-chan struct{})
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(streaming)
		<-shuttingDown
	})
	require.NoError(t, server.UpsertEndpoint(routerID, registry.NewID("test", "shutdown_stream"), "/stream", http.MethodGet, stream))
	require.NoError(t, server.Rebuild(context.Background()))

	ctx, cancel := context.WithTimeout(contextapi.NewRootContext(), 10*time.Second)
	defer cancel()
	_, err = server.Start(ctx)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s/api/stream", cfg.Addr), nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	<-streaming

	stopCtx, stopCancel := context.WithTimeout(ctx, 2*time.Second)
	defer stopCancel()
	began := time.Now()
	require.NoError(t, server.Stop(stopCtx))
	assert.Less(t, time.Since(began), time.Second)
}

func TestServerService_RelayStaysReachableWhileStopDrainsRequests(t *testing.T) {
	cfg := &config.ServerConfig{Addr: "127.0.0.1:0"}
	server, err := NewServerService(registry.NewID("test", "drain_server"), cfg, NewMiddlewareRegistry(zap.NewNop()))
	require.NoError(t, err)

	routerID := registry.NewID("test", "drain_router")
	require.NoError(t, server.UpsertRouter(routerID, &config.RouterConfig{Prefix: "/api"}))
	target := pid.PID{Host: "drain", UniqID: "session"}
	delivered := make(chan *relay.Package, 1)
	relayed := make(chan error, 1)
	release := make(chan struct{})
	held := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		signal, _ := contextapi.FrameFromContext(r.Context()).Get(config.ServerShutdownKey())
		shuttingDown, _ := signal.(<-chan struct{})
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-shuttingDown

		// A request that is still being served gracefully keeps its relay host.
		inbox := make(chan *relay.Package, 1)
		_, err := server.Attach(target, inbox)
		if err == nil {
			err = server.Send(&relay.Package{Target: target, Messages: []*relay.Message{{Topic: "drain"}}})
		}
		if err == nil {
			select {
			case pkg := <-inbox:
				delivered <- pkg
			case <-time.After(2 * time.Second):
			}
			server.Detach(target)
		}
		relayed <- err
		<-release
	})
	require.NoError(t, server.UpsertEndpoint(routerID, registry.NewID("test", "drain_held"), "/held", http.MethodGet, held))
	require.NoError(t, server.Rebuild(context.Background()))

	ctx, cancel := context.WithTimeout(contextapi.NewRootContext(), 20*time.Second)
	defer cancel()
	status, err := server.Start(ctx)
	require.NoError(t, err)
	addr := strings.TrimPrefix((<-status).(string), "service listening on ")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/api/held", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	stopCtx, stopCancel := context.WithTimeout(ctx, 10*time.Second)
	defer stopCancel()
	stopped := make(chan error, 1)
	go func() { stopped <- server.Stop(stopCtx) }()

	select {
	case pkg := <-delivered:
		assert.Equal(t, "drain", pkg.Messages[0].Topic)
	case err := <-relayed:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("relay calls block while the server drains its requests")
	}
	select {
	case err := <-relayed:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("relay detach blocks while the server drains its requests")
	}
	select {
	case err := <-stopped:
		t.Fatalf("stop returned while a request is still in flight: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	require.NoError(t, <-stopped)
	_, err = server.Attach(target, delivered)
	assert.ErrorIs(t, err, ErrServerHostNotInitialized)
	assert.ErrorIs(t, server.Send(&relay.Package{Target: target}), ErrServerHostNotInitialized)
}

func TestServerService_StopWaitsForTheDrainInProgress(t *testing.T) {
	for _, test := range []struct {
		retire func(ctx context.Context, server *ServerService, cancelStart context.CancelFunc) <-chan error
		name   string
	}{
		{name: "earlier stop", retire: func(ctx context.Context, server *ServerService, _ context.CancelFunc) <-chan error {
			done := make(chan error, 1)
			go func() { done <- server.Stop(ctx) }()
			return done
		}},
		{name: "start context canceled", retire: func(_ context.Context, _ *ServerService, cancelStart context.CancelFunc) <-chan error {
			cancelStart()
			return nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := &config.ServerConfig{Addr: "127.0.0.1:0"}
			server, err := NewServerService(registry.NewID("test", "drain_wait_server"), cfg, NewMiddlewareRegistry(zap.NewNop()))
			require.NoError(t, err)

			routerID := registry.NewID("test", "drain_wait_router")
			require.NoError(t, server.UpsertRouter(routerID, &config.RouterConfig{Prefix: "/api"}))
			draining := make(chan struct{})
			release := make(chan struct{})
			held := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				signal, _ := contextapi.FrameFromContext(r.Context()).Get(config.ServerShutdownKey())
				shuttingDown, _ := signal.(<-chan struct{})
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				<-shuttingDown
				close(draining)
				<-release
			})
			require.NoError(t, server.UpsertEndpoint(routerID, registry.NewID("test", "drain_wait_held"), "/held", http.MethodGet, held))
			require.NoError(t, server.Rebuild(context.Background()))

			ctx, cancel := context.WithTimeout(contextapi.NewRootContext(), 20*time.Second)
			defer cancel()
			startCtx, cancelStart := context.WithCancel(ctx)
			defer cancelStart()
			status, err := server.Start(startCtx)
			require.NoError(t, err)
			addr := strings.TrimPrefix((<-status).(string), "service listening on ")

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/api/held", nil)
			require.NoError(t, err)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			first := test.retire(ctx, server, cancelStart)
			<-draining

			second := make(chan error, 1)
			go func() { second <- server.Stop(ctx) }()
			select {
			case err := <-second:
				t.Fatalf("stop returned while the retired server drains: %v", err)
			case <-time.After(100 * time.Millisecond):
			}

			expired, expire := context.WithCancel(ctx)
			expire()
			assert.ErrorIs(t, server.Stop(expired), context.Canceled)

			close(release)
			if first != nil {
				require.NoError(t, <-first)
			}
			require.NoError(t, <-second)
		})
	}
}

// serveHeld registers handler at /api/held on a new server for cfg and returns the server.
func serveHeld(t *testing.T, cfg *config.ServerConfig, handler http.HandlerFunc) *ServerService {
	t.Helper()
	server, err := NewServerService(registry.NewID("test", "held_server"), cfg, NewMiddlewareRegistry(zap.NewNop()))
	require.NoError(t, err)
	routerID := registry.NewID("test", "held_router")
	require.NoError(t, server.UpsertRouter(routerID, &config.RouterConfig{Prefix: "/api"}))
	require.NoError(t, server.UpsertEndpoint(routerID, registry.NewID("test", "held"), "/held", http.MethodGet, handler))
	require.NoError(t, server.Rebuild(context.Background()))
	return server
}

// startHeld starts server and opens a request to /api/held whose response headers have
// arrived. The caller closes the response body.
func startHeld(ctx context.Context, t *testing.T, server *ServerService) (string, *http.Response) {
	t.Helper()
	status, err := server.Start(ctx)
	require.NoError(t, err)
	addr := strings.TrimPrefix((<-status).(string), "service listening on ")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/api/held", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return addr, resp
}

// holdUntilShutdown answers with headers, waits for the server shutdown signal, closes
// draining and runs during before it waits for release.
func holdUntilShutdown(draining chan<- struct{}, during func(), release <-chan struct{}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		signal, _ := contextapi.FrameFromContext(r.Context()).Get(config.ServerShutdownKey())
		shuttingDown, _ := signal.(<-chan struct{})
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-shuttingDown
		close(draining)
		if during != nil {
			during()
		}
		<-release
	}
}

func TestServerService_StartWaitsForTheDrainOfTheRetiredServer(t *testing.T) {
	port, err := findFreePort()
	require.NoError(t, err)
	draining, release := make(chan struct{}), make(chan struct{})
	server := serveHeld(t, &config.ServerConfig{Addr: fmt.Sprintf("127.0.0.1:%d", port)}, holdUntilShutdown(draining, nil, release))

	ctx, cancel := context.WithTimeout(contextapi.NewRootContext(), 20*time.Second)
	defer cancel()
	_, resp := startHeld(ctx, t, server)
	defer resp.Body.Close()

	stopped := make(chan error, 1)
	go func() { stopped <- server.Stop(ctx) }()
	<-draining

	canceled, cancelStart := context.WithCancel(ctx)
	cancelStart()
	_, err = server.Start(canceled)
	require.ErrorIs(t, err, context.Canceled, "a start that cannot wait for the drain reports its context")

	restarted := make(chan error, 1)
	go func() {
		_, err := server.Start(ctx)
		restarted <- err
	}()
	select {
	case err := <-restarted:
		t.Fatalf("start returned while the retired server drains: %v", err)
	case <-time.After(500 * time.Millisecond):
	}

	close(release)
	require.NoError(t, <-stopped)
	require.NoError(t, <-restarted)
	require.NoError(t, server.Stop(ctx))
}

func TestServerService_RetiredRequestsKeepTheirHostAcrossRestart(t *testing.T) {
	target := pid.PID{Host: "retired", UniqID: "session"}
	inbox := make(chan *relay.Package, 1)
	attached, restartIssued := make(chan error, 1), make(chan struct{})
	delivered := make(chan *relay.Package, 1)
	draining, release := make(chan struct{}), make(chan struct{})
	var server *ServerService
	server = serveHeld(t, &config.ServerConfig{Addr: "127.0.0.1:0"}, holdUntilShutdown(draining, func() {
		_, err := server.Attach(target, inbox)
		attached <- err
		if err != nil {
			return
		}
		<-restartIssued
		if err := server.Send(&relay.Package{Target: target, Messages: []*relay.Message{{Topic: "retired"}}}); err == nil {
			select {
			case pkg := <-inbox:
				delivered <- pkg
			case <-time.After(time.Second):
			}
		}
		server.Detach(target)
	}, release))

	ctx, cancel := context.WithTimeout(contextapi.NewRootContext(), 20*time.Second)
	defer cancel()
	_, resp := startHeld(ctx, t, server)
	defer resp.Body.Close()

	stopped := make(chan error, 1)
	go func() { stopped <- server.Stop(ctx) }()
	<-draining
	require.NoError(t, <-attached)

	restarted := make(chan error, 1)
	go func() {
		_, err := server.Start(ctx)
		restarted <- err
	}()
	select {
	case err := <-restarted:
		require.NoError(t, err)
	case <-time.After(500 * time.Millisecond):
	}
	close(restartIssued)

	select {
	case pkg := <-delivered:
		assert.Equal(t, "retired", pkg.Messages[0].Topic)
	case <-time.After(2 * time.Second):
		t.Fatal("a request of the retired server lost its relay host to the restart")
	}

	close(release)
	require.NoError(t, <-stopped)
	select {
	case err := <-restarted:
		require.NoError(t, err)
	default:
	}
	require.NoError(t, server.Stop(ctx))
}

func TestServerService_StopClosesConnectionsThatOutliveTheDeadline(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	server := serveHeld(t, &config.ServerConfig{Addr: "127.0.0.1:0"}, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-release
	})

	ctx, cancel := context.WithTimeout(contextapi.NewRootContext(), 20*time.Second)
	defer cancel()
	_, resp := startHeld(ctx, t, server)
	defer resp.Body.Close()

	stopCtx, stopCancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer stopCancel()
	err := server.Stop(stopCtx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// The request context bounds the read, so a connection that outlives the stop shows as
	// a read that lasts until that context ends.
	began := time.Now()
	_, err = io.ReadAll(resp.Body)
	assert.Error(t, err, "the connection ends without completing the response")
	assert.Less(t, time.Since(began), 2*time.Second, "a connection outlives the stop that gave up on it")
}
