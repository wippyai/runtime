// SPDX-License-Identifier: MPL-2.0

package http

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	contextapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
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
