//go:build integration

// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"context"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	execapi "github.com/wippyai/runtime/api/service/exec"
	ttyapi "github.com/wippyai/runtime/api/tty"
	dockerexec "github.com/wippyai/runtime/service/exec/docker"
	"go.uber.org/zap"
)

const dockerAttachProofTimeout = 10 * time.Second

// observedDockerPTY instruments only writes; all execution behavior is native.
type observedDockerPTY struct {
	execapi.PTYProcess
	entered  chan struct{}
	returned chan struct{}
}

func (p *observedDockerPTY) WriteStdin(data []byte) error {
	close(p.entered)
	defer close(p.returned)
	return p.PTYProcess.WriteStdin(data)
}
func TestNativeDockerInputCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cli, err := client.New(client.FromEnv)
	require.NoError(t, err)
	defer cli.Close()
	setupCtx, setupCancel := context.WithTimeout(ctx, dockerAttachProofTimeout)
	defer setupCancel()
	image, err := cli.ImageInspect(setupCtx, "alpine:latest")
	require.NoError(t, err, "fixture requires local image; no downloads")
	executor, err := dockerexec.NewDockerExecutor(zap.NewNop(), &execapi.DockerExecutorConfig{Image: image.ID, AutoRemove: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = executor.Close() })
	child, err := executor.NewProcess("sh -c 'stty -echo -icanon; echo ready; sleep 60'", execapi.ProcessOptions{PTY: &execapi.PTYOptions{Width: 20, Height: 4}})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = child.Signal(int(syscall.SIGKILL))
		if c, ok := child.(execapi.WaitCanceler); ok {
			c.CancelWait()
		}
	})
	process := &observedDockerPTY{PTYProcess: child.(execapi.PTYProcess), entered: make(chan struct{}), returned: make(chan struct{})}
	surface := &testSurface{}
	bridge, err := New(process, surface, 20, 4)
	require.NoError(t, err)
	events := make(chan ttyapi.Event, 1)
	done := make(chan error, 1)
	go func() { done <- bridge.Run(ctx, events) }()
	require.Eventually(t, func() bool {
		surface.mu.Lock()
		defer surface.mu.Unlock()
		return strings.Contains(strings.Join(surface.rows, "\n"), "ready")
	}, dockerAttachProofTimeout, 20*time.Millisecond)
	events <- ttyapi.Event{Type: "paste", Paste: strings.Repeat("x", 16*1024*1024)}
	select {
	case <-process.entered:
	case <-time.After(time.Second):
		t.Fatal("input not dispatched")
	}
	select {
	case <-process.returned:
		t.Fatal("fixture did not block input")
	case <-time.After(200 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		_ = child.Signal(int(syscall.SIGKILL))
		select {
		case <-done:
		case <-time.After(dockerAttachProofTimeout):
		}
		t.Fatal("native Docker input blocked terminal cancellation")
	}
}
