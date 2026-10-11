// SPDX-License-Identifier: MPL-2.0

package docker

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	execapi "github.com/wippyai/runtime/api/service/exec"
	"go.uber.org/zap"
)

func TestStartAfterExecutorCloseDoesNotContactDaemon(t *testing.T) {
	for _, direct := range []bool{false, true} {
		t.Run(map[bool]string{false: "acquired", true: "direct"}[direct], func(t *testing.T) {
			cli, err := client.New(client.WithHost("http://daemon.invalid"), client.WithAPIVersion("1.44"),
				client.WithHTTPClient(&http.Client{Transport: startupTransport(func(*http.Request) (*http.Response, error) {
					t.Error("closed executor contacted the daemon")
					return nil, context.Canceled
				})}))
			require.NoError(t, err)
			p, executor := acquiredStartupProcess(context.Background(), t, cli, execapi.ProcessOptions{})
			if direct {
				process, err := executor.NewProcess("true", execapi.ProcessOptions{})
				require.NoError(t, err)
				p = process.(*Process)
			}
			require.NoError(t, executor.Close())
			require.ErrorIs(t, p.Start(), context.Canceled)
			require.False(t, p.started)
			require.Empty(t, p.containerID)
		})
	}
}

func TestStartupCancellationCleansUpEveryDaemonStage(t *testing.T) {
	for _, stage := range []string{"attach", "start", "resize"} {
		for _, closeExecutor := range []bool{false, true} {
			owner := map[bool]string{false: "owner", true: "executor"}[closeExecutor]
			t.Run(stage+"/"+owner, func(t *testing.T) {
				entered := make(chan struct{})
				unblock := make(chan struct{})
				removed := make(chan bool, 1)
				stall := func(w http.ResponseWriter, r *http.Request) {
					close(entered)
					select {
					case <-r.Context().Done():
					case <-unblock:
					}
					w.WriteHeader(http.StatusInternalServerError)
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch {
					case strings.HasSuffix(r.URL.Path, "/containers/create"):
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusCreated)
						_, _ = io.WriteString(w, `{"Id":"created"}`)
					case strings.HasSuffix(r.URL.Path, "/containers/created/attach"):
						if stage == "attach" {
							stall(w, r)
							return
						}
						conn, _, err := w.(http.Hijacker).Hijack()
						if !assert.NoError(t, err) {
							return
						}
						defer conn.Close()
						_, err = io.WriteString(conn, "HTTP/1.1 101 UPGRADED\r\nContent-Type: application/vnd.docker.raw-stream\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
						if assert.NoError(t, err) {
							<-unblock
						}
					case strings.HasSuffix(r.URL.Path, "/containers/created/start"):
						if stage == "start" {
							stall(w, r)
						} else {
							w.WriteHeader(http.StatusNoContent)
						}
					case strings.HasSuffix(r.URL.Path, "/containers/created/resize"):
						stall(w, r)
					case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/containers/created"):
						removed <- r.Context().Err() == nil && r.URL.Query().Get("force") == "1"
						w.WriteHeader(http.StatusNoContent)
					default:
						t.Errorf("unexpected daemon request: %s %s", r.Method, r.URL.Path)
					}
				}))
				defer server.Close()
				cli, err := client.New(client.WithHost("tcp://"+strings.TrimPrefix(server.URL, "http://")), client.WithAPIVersion("1.44"))
				require.NoError(t, err)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				opts := execapi.ProcessOptions{}
				if stage == "resize" {
					opts.PTY = &execapi.PTYOptions{}
				}
				p, executor := acquiredStartupProcess(ctx, t, cli, opts)
				done := make(chan error, 1)
				go func() { done <- p.Start(); close(done) }()
				defer func() { cancel(); close(unblock); <-done }()
				select {
				case <-entered:
				case err := <-done:
					t.Fatalf("startup did not reach %s: %v", stage, err)
				case <-time.After(5 * time.Second):
					t.Fatalf("startup never reached %s", stage)
				}
				if closeExecutor {
					require.NoError(t, executor.Close())
				} else {
					cancel()
				}
				select {
				case err := <-done:
					require.ErrorIs(t, err, context.Canceled)
				case <-time.After(5 * time.Second):
					t.Fatalf("startup did not stop at %s", stage)
				}
				select {
				case activeCleanup := <-removed:
					require.True(t, activeCleanup, "container removal must use an independent active context")
				default:
					t.Fatal("Start returned without removing the failed container")
				}
				require.False(t, p.started)
				require.Empty(t, p.containerID)
				require.Nil(t, p.Stdout())
				require.Nil(t, p.Stderr())
			})
		}
	}
}

func TestReleasedExecutorBorrowKeepsConstructedProcessOwner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cli, err := client.New(client.WithHost("http://daemon.invalid"), client.WithAPIVersion("1.44"),
		client.WithHTTPClient(&http.Client{Transport: startupTransport(func(*http.Request) (*http.Response, error) {
			t.Error("canceled process contacted the daemon")
			return nil, context.Canceled
		})}))
	require.NoError(t, err)
	_, executor := acquiredStartupProcess(context.Background(), t, cli, execapi.ProcessOptions{})
	res, err := newExecutorProvider(executor).Acquire(ctx, registry.ID{}, resource.ModeNormal)
	require.NoError(t, err)
	defer res.Release()
	value, err := res.Get()
	require.NoError(t, err)
	_, hasCloser := value.(io.Closer)
	require.True(t, hasCloser, "the existing closer capability must be retained")
	p, err := value.(execapi.ProcessExecutor).NewProcess("true", execapi.ProcessOptions{})
	require.NoError(t, err)
	res.Release()
	_, err = res.Get()
	require.ErrorIs(t, err, resource.ErrReleased)
	cancel()
	require.ErrorIs(t, p.Start(), context.Canceled, "a constructed process retains its owner independently of the resource handle")
}

func BenchmarkDockerExecutorAcquisition(b *testing.B) {
	executor, err := NewDockerExecutor(zap.NewNop(), &execapi.DockerExecutorConfig{Image: "fixture"})
	if err != nil {
		b.Fatal(err)
	}
	defer executor.Close()
	provider := newExecutorProvider(executor)
	b.ReportAllocs()
	for b.Loop() {
		res, err := provider.Acquire(context.Background(), registry.ID{}, resource.ModeNormal)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := res.Get(); err != nil {
			b.Fatal(err)
		}
		res.Release()
	}
}
