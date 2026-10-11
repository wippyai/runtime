// SPDX-License-Identifier: MPL-2.0

package docker

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestCanceledAttachRemovesCreatedContainer(t *testing.T) {
	attached := make(chan struct{})
	removed := make(chan bool, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/create"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"Id":"created"}`)
		case strings.HasSuffix(r.URL.Path, "/containers/created/attach"):
			close(attached)
			<-r.Context().Done()
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/containers/created"):
			removed <- r.Context().Err() == nil && r.URL.Query().Get("force") == "1"
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	cli, err := client.New(client.WithHost("tcp://"+strings.TrimPrefix(server.URL, "http://")), client.WithAPIVersion("1.44"))
	require.NoError(t, err)
	defer cli.Close()
	p := &Process{cli: cli, log: zap.NewNop(), image: "fixture"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- p.StartContext(ctx) }()
	select {
	case <-attached:
	case err := <-done:
		t.Fatalf("startup failed before attach: %v", err)
	}
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.True(t, <-removed, "failed startup must clean up with an active control context")
	require.False(t, p.started)
	require.Empty(t, p.containerID)
}
