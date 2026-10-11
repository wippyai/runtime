// SPDX-License-Identifier: MPL-2.0

package docker

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	execapi "github.com/wippyai/runtime/api/service/exec"
	"go.uber.org/zap"
)

type startupTransport func(*http.Request) (*http.Response, error)

func (f startupTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Exercise the same acquisition and construction path as exec.get(): the
// provider receives the owner's context before NewProcess creates the handle.
func acquiredStartupProcess(ctx context.Context, t *testing.T, cli *client.Client, opts execapi.ProcessOptions) (*Process, *Executor) {
	t.Helper()
	executor, err := NewDockerExecutor(zap.NewNop(), &execapi.DockerExecutorConfig{Image: "fixture"})
	require.NoError(t, err)
	require.NoError(t, executor.cli.Close())
	executor.cli = cli
	t.Cleanup(func() { _ = executor.Close() })
	res, err := newExecutorProvider(executor).Acquire(ctx, registry.ID{}, resource.ModeNormal)
	require.NoError(t, err)
	t.Cleanup(res.Release)
	value, err := res.Get()
	require.NoError(t, err)
	proc, err := value.(execapi.ProcessExecutor).NewProcess("true", opts)
	require.NoError(t, err)
	if pty, ok := proc.(*ptyProcess); ok {
		return pty.Process, executor
	}
	return proc.(*Process), executor
}

func TestStartHasNoImplicitDeadline(t *testing.T) {
	for _, ownerDeadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "unbounded owner", true: "owner budget"}[ownerDeadline], func(t *testing.T) {
			ctx := context.Background()
			if ownerDeadline {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(time.Hour))
				defer cancel()
			}
			refusal := errors.New("daemon refused create")
			cli, err := client.New(client.WithHost("http://daemon.invalid"), client.WithAPIVersion("1.44"), client.WithHTTPClient(&http.Client{Transport: startupTransport(func(r *http.Request) (*http.Response, error) {
				deadline, bounded := r.Context().Deadline()
				expected, ownerBounded := ctx.Deadline()
				require.Equal(t, ownerBounded, bounded, "startup must use its owner's budget")
				require.Equal(t, expected, deadline)
				return nil, refusal
			})}))
			require.NoError(t, err)
			p, _ := acquiredStartupProcess(ctx, t, cli, execapi.ProcessOptions{})
			require.ErrorContains(t, p.Start(), refusal.Error())
			require.False(t, p.started)
		})
	}
}

func TestStartCanceledByLifecycle(t *testing.T) {
	for _, pty := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "PTY"}[pty], func(t *testing.T) {
			entered := make(chan struct{})
			unblock := make(chan struct{})
			cli, err := client.New(client.WithHost("http://daemon.invalid"), client.WithAPIVersion("1.44"), client.WithHTTPClient(&http.Client{Transport: startupTransport(func(r *http.Request) (*http.Response, error) {
				close(entered)
				select {
				case <-r.Context().Done():
					return nil, r.Context().Err()
				case <-unblock:
					return nil, errors.New("test released stalled daemon")
				}
			})}))
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			opts := execapi.ProcessOptions{}
			if pty {
				opts.PTY = &execapi.PTYOptions{}
			}
			p, _ := acquiredStartupProcess(ctx, t, cli, opts)
			result := make(chan error, 1)
			go func() { result <- p.Start(); close(result) }()
			defer func() { close(unblock); <-result }()
			<-entered
			cancel()
			select {
			case err := <-result:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(time.Second):
				t.Fatal("Start did not observe owner lifecycle cancellation")
			}
			require.False(t, p.started)
			require.Empty(t, p.containerID)
		})
	}
}

func TestStartCanceledByExecutorClose(t *testing.T) {
	for _, direct := range []bool{false, true} {
		t.Run(map[bool]string{false: "acquired", true: "direct"}[direct], func(t *testing.T) {
			entered := make(chan struct{})
			unblock := make(chan struct{})
			cli, err := client.New(client.WithHost("http://daemon.invalid"), client.WithAPIVersion("1.44"), client.WithHTTPClient(&http.Client{Transport: startupTransport(func(r *http.Request) (*http.Response, error) {
				close(entered)
				select {
				case <-r.Context().Done():
					return nil, r.Context().Err()
				case <-unblock:
					return nil, errors.New("test released stalled daemon")
				}
			})}))
			require.NoError(t, err)
			p, executor := acquiredStartupProcess(context.Background(), t, cli, execapi.ProcessOptions{})
			if direct {
				process, err := executor.NewProcess("true", execapi.ProcessOptions{})
				require.NoError(t, err)
				p = process.(*Process)
			}
			result := make(chan error, 1)
			go func() { result <- p.Start(); close(result) }()
			defer func() { close(unblock); <-result }()
			<-entered
			require.NoError(t, executor.Close())
			select {
			case err := <-result:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(time.Second):
				t.Fatal("Start did not observe executor closure")
			}
			require.False(t, p.started)
		})
	}
}

func TestAcquisitionsKeepStartupOwnersIndependent(t *testing.T) {
	type ownerKey struct{}
	key := ownerKey{}
	refusal := errors.New("daemon refused create")
	cli, err := client.New(client.WithHost("http://daemon.invalid"), client.WithAPIVersion("1.44"), client.WithHTTPClient(&http.Client{Transport: startupTransport(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "second", r.Context().Value(key), "first owner's cancelled startup must never reach the daemon")
		return nil, refusal
	})}))
	require.NoError(t, err)
	_, executor := acquiredStartupProcess(context.Background(), t, cli, execapi.ProcessOptions{})
	provider := newExecutorProvider(executor)
	firstCtx, cancelFirst := context.WithCancel(context.WithValue(context.Background(), key, "first"))
	defer cancelFirst()
	first, err := provider.Acquire(firstCtx, registry.ID{}, resource.ModeNormal)
	require.NoError(t, err)
	defer first.Release()
	second, err := provider.Acquire(context.WithValue(context.Background(), key, "second"), registry.ID{}, resource.ModeNormal)
	require.NoError(t, err)
	defer second.Release()
	// Construct both handles only after both acquisitions, detecting accidental
	// mutation of a shared executor's owner context.
	firstValue, err := first.Get()
	require.NoError(t, err)
	firstProcess, err := firstValue.(execapi.ProcessExecutor).NewProcess("true", execapi.ProcessOptions{})
	require.NoError(t, err)
	secondValue, err := second.Get()
	require.NoError(t, err)
	secondProcess, err := secondValue.(execapi.ProcessExecutor).NewProcess("true", execapi.ProcessOptions{})
	require.NoError(t, err)
	cancelFirst()
	require.ErrorIs(t, firstProcess.Start(), context.Canceled)
	require.ErrorContains(t, secondProcess.Start(), refusal.Error())
}
