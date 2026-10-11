// SPDX-License-Identifier: MPL-2.0

package docker

import (
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	execapi "github.com/wippyai/runtime/api/service/exec"
)

func TestCanceledAttachRemovesCreatedContainer(t *testing.T) {
	attached := make(chan struct{})
	removed := make(chan bool, 1)
	unblock := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/create"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"Id":"created"}`)
		case strings.HasSuffix(r.URL.Path, "/containers/created/attach"):
			close(attached)
			select {
			case <-r.Context().Done():
			case <-unblock:
			}
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p, _ := acquiredStartupProcess(ctx, t, cli, execapi.ProcessOptions{})
	done := make(chan error, 1)
	go func() { done <- p.Start(); close(done) }()
	defer func() { close(unblock); <-done }()
	select {
	case <-attached:
	case err := <-done:
		t.Fatalf("startup failed before attach: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("Start did not cancel the attach handshake")
	}
	require.True(t, <-removed, "failed startup must clean up with an active control context")
	require.False(t, p.started)
	require.Empty(t, p.containerID)
}

func TestStartReportsStartupOutcome(t *testing.T) {
	for _, stage := range []string{"start", "resize", "success"} {
		t.Run(stage, func(t *testing.T) {
			entered := make(chan struct{})
			reply := make(chan struct{}, 1)
			defer close(reply)
			output := make(chan struct{})
			removed := make(chan bool, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/containers/create"):
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					_, _ = io.WriteString(w, `{"Id":"created"}`)
				case strings.HasSuffix(r.URL.Path, "/containers/created/attach"):
					conn, _, err := w.(http.Hijacker).Hijack()
					if !assert.NoError(t, err) {
						return
					}
					defer conn.Close()
					_, err = io.WriteString(conn, "HTTP/1.1 101 UPGRADED\r\nContent-Type: application/vnd.docker.raw-stream\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
					if !assert.NoError(t, err) {
						return
					}
					<-output
					if stage == "success" {
						body := []byte("still attached")
						frame := make([]byte, 8+len(body))
						frame[0] = 1
						binary.BigEndian.PutUint32(frame[4:8], uint32(len(body)))
						copy(frame[8:], body)
						_, _ = conn.Write(frame)
					}
				case strings.HasSuffix(r.URL.Path, "/containers/created/start"):
					close(entered)
					<-reply
					if stage == "start" {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusInternalServerError)
						_, _ = io.WriteString(w, `{"message":"daemon refused start"}`)
					} else {
						w.WriteHeader(http.StatusNoContent)
					}
				case strings.HasSuffix(r.URL.Path, "/containers/created/resize"):
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = io.WriteString(w, `{"message":"daemon refused resize"}`)
				case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/containers/created"):
					removed <- r.Context().Err() == nil && r.URL.Query().Get("force") == "1"
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
			}))
			defer server.Close()
			defer close(output)
			cli, err := client.New(client.WithHost("tcp://"+strings.TrimPrefix(server.URL, "http://")), client.WithAPIVersion("1.44"))
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			opts := execapi.ProcessOptions{}
			if stage == "resize" {
				opts.PTY = &execapi.PTYOptions{}
			}
			p, _ := acquiredStartupProcess(ctx, t, cli, opts)
			done := make(chan error, 1)
			go func() { done <- p.Start(); close(done) }()
			defer func() { cancel(); <-done }()
			select {
			case <-entered:
			case err := <-done:
				t.Fatalf("startup failed before start request: %v", err)
			}
			select {
			case err := <-done:
				t.Fatalf("Start returned before daemon startup outcome: %v", err)
			default:
			}
			reply <- struct{}{}
			err = <-done
			if stage != "success" {
				require.ErrorContains(t, err, "daemon refused "+stage)
				require.True(t, <-removed)
				require.False(t, p.started)
				require.Empty(t, p.containerID)
				require.Nil(t, p.Stdout())
				require.Nil(t, p.Stderr())
				return
			}
			require.NoError(t, err)
			require.True(t, p.started)
			require.Nil(t, p.startupCtx, "completed startup must not retain its acquiring frame")
			require.Nil(t, p.lifecycleCtx, "startup shutdown watcher must not outlive startup")
			// Startup cancellation is no longer allowed to close a successful stream.
			cancel()
			output <- struct{}{}
			body, err := io.ReadAll(p.Stdout())
			require.NoError(t, err)
			require.Equal(t, "still attached", string(body))
			require.NoError(t, p.Stdout().Close())
			require.NoError(t, p.Stderr().Close())
			require.ErrorIs(t, p.Start(), ErrContainerAlreadyStart)
		})
	}
}
