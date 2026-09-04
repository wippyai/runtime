// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	rtresource "github.com/wippyai/runtime/api/runtime/resource"
	execapi "github.com/wippyai/runtime/api/service/exec"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

// reapProbe records whether the process was waited on, which is the only thing
// that releases a child's entry in the OS process table.
type reapProbe struct {
	waitReleased chan struct{}
	signals      []int
	mu           sync.Mutex
	waitCalled   int
	blockWait    bool
}

func newReapProbe() *reapProbe {
	return &reapProbe{waitReleased: make(chan struct{})}
}

func (r *reapProbe) Start() error { return nil }

func (r *reapProbe) Signal(sig int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.signals = append(r.signals, sig)
	return nil
}

func (r *reapProbe) WriteStdin(_ []byte) error { return nil }

func (r *reapProbe) Wait() error {
	r.mu.Lock()
	r.waitCalled++
	block := r.blockWait
	r.mu.Unlock()

	if block {
		// Stands in for a child that ignores SIGTERM: Wait only returns once
		// something stronger arrives.
		<-r.waitReleased
	}
	return nil
}

func (r *reapProbe) Stdout() io.ReadCloser { return io.NopCloser(strings.NewReader("")) }
func (r *reapProbe) Stderr() io.ReadCloser { return io.NopCloser(strings.NewReader("")) }

func (r *reapProbe) waits() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.waitCalled
}

func (r *reapProbe) sent() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int(nil), r.signals...)
}

// waitFor polls until cond holds or the deadline passes. The reap is detached
// from close(), so the assertion has to allow for it landing shortly after.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return cond()
}

// A signaled child that is never waited on stays a zombie for the lifetime of
// the runtime, and close() is the documented way to stop a process.
func TestReapReleasedWaitsOnGracefulStop(t *testing.T) {
	probe := newReapProbe()

	reapReleased(probe, false)

	if probe.waits() != 1 {
		t.Fatalf("expected the released process to be waited on once, got %d", probe.waits())
	}
}

func TestReapReleasedWaitsOnForcedStop(t *testing.T) {
	probe := newReapProbe()

	reapReleased(probe, true)

	if probe.waits() != 1 {
		t.Fatalf("expected the killed process to be waited on once, got %d", probe.waits())
	}
	if sent := probe.sent(); len(sent) != 0 {
		t.Fatalf("a process the caller already killed must not be signaled again, got %v", sent)
	}
}

// A child that ignores SIGTERM must not leave the reaper parked forever: that
// would trade a leaked process for a leaked goroutine.
func TestReapReleasedEscalatesToKill(t *testing.T) {
	probe := newReapProbe()
	probe.blockWait = true

	done := make(chan struct{})
	go func() {
		defer close(done)
		reapReleasedWithGrace(probe, false, 10*time.Millisecond)
	}()

	// Let the escalation fire, then release the stand-in child as a real one
	// would be released by SIGKILL.
	if !waitFor(t, time.Second, func() bool { return len(probe.sent()) > 0 }) {
		t.Fatal("expected the unresponsive process to be escalated to SIGKILL")
	}

	sent := probe.sent()
	if sent[0] != int(syscall.SIGKILL) {
		t.Fatalf("expected SIGKILL escalation, got signal %d", sent[0])
	}

	close(probe.waitReleased)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reaper did not return after the process was released")
	}
}

type gatedStartProbe struct {
	*reapProbe
	entered chan struct{}
	release chan struct{}
}

func (p *gatedStartProbe) Start() error {
	close(p.entered)
	<-p.release
	return nil
}

func TestRuntimeCleanupWaitsForStartThenReapsChild(t *testing.T) {
	ctx, frame := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(frame)
	store := rtresource.NewStore()
	require.NoError(t, rtresource.SetStore(ctx, store))
	probe := &gatedStartProbe{
		reapProbe: newReapProbe(), entered: make(chan struct{}), release: make(chan struct{}),
	}
	process := NewProcess(ctx, probe)
	l := setupState()
	defer l.Close()
	value.PushTypedUserData(l, process, processTypeName)

	startDone := make(chan struct{})
	go func() {
		procStart(l)
		close(startDone)
	}()
	<-probe.entered
	cleanupDone := make(chan error, 1)
	go func() { cleanupDone <- store.Close() }()
	select {
	case <-cleanupDone:
		t.Fatal("runtime cleanup raced ahead of process startup")
	case <-time.After(20 * time.Millisecond):
	}
	close(probe.release)
	<-startDone
	require.NoError(t, <-cleanupDone)
	require.True(t, waitFor(t, time.Second, func() bool { return probe.waits() == 1 }))
	require.Equal(t, []int{int(syscall.SIGTERM)}, probe.sent())
}

func TestRuntimeCleanupReleasesUnstartedProcessWithoutError(t *testing.T) {
	ctx, frame := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(frame)
	store := rtresource.NewStore()
	require.NoError(t, rtresource.SetStore(ctx, store))
	probe := newReapProbe()
	_ = NewProcess(ctx, probe)

	require.NoError(t, store.Close())
	require.Empty(t, probe.sent())
	require.Zero(t, probe.waits())
}

// The end the whole change exists for: a real child, stopped through the same
// path Lua's close() takes, must not be left as a zombie.
func TestClosedProcessIsNotLeftAsZombie(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not available")
	}

	cmd := exec.CommandContext(t.Context(), "sleep", "30")
	// Pipes are created the same way the native executor creates them, because
	// Wait closes them and that interaction is part of what is being checked.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	defer func() { _ = stdout.Close() }()

	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	pid := cmd.Process.Pid

	handle := &cmdProcess{cmd: cmd, stdout: stdout}

	_ = handle.Signal(int(syscall.SIGTERM))
	go reapReleased(handle, false)

	// A reaped child leaves the process table entirely; an unreaped one lingers
	// in state Z until the parent exits.
	if !waitFor(t, 15*time.Second, func() bool { return !processExists(pid) }) {
		if isZombie(pid) {
			t.Fatalf("process %d was left as a zombie: it was signaled but never reaped", pid)
		}
		t.Fatalf("process %d was still present after close and reap", pid)
	}
}

// cmdProcess adapts a plain *exec.Cmd to the process interface so the reaper can
// be exercised against a real child without standing up an executor.
type cmdProcess struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
}

func (c *cmdProcess) Start() error { return c.cmd.Start() }

func (c *cmdProcess) Signal(sig int) error {
	if c.cmd.Process == nil {
		return nil
	}
	return c.cmd.Process.Signal(syscall.Signal(sig))
}

func (c *cmdProcess) WriteStdin(_ []byte) error { return nil }
func (c *cmdProcess) Wait() error               { return c.cmd.Wait() }
func (c *cmdProcess) Stdout() io.ReadCloser     { return c.stdout }
func (c *cmdProcess) Stderr() io.ReadCloser     { return io.NopCloser(strings.NewReader("")) }

var _ execapi.Process = (*cmdProcess)(nil)

// processExists reports whether the pid is still in the process table at all,
// zombie or otherwise.
func processExists(pid int) bool {
	_, err := os.Stat(procStatPath(pid))
	return err == nil
}

// isZombie reads the process state field from /proc. A reaped child has no entry
// at all; an unreaped one lingers in state Z.
func isZombie(pid int) bool {
	data, err := os.ReadFile(procStatPath(pid))
	if err != nil {
		return false
	}
	// The comm field may contain spaces, so the state follows the final ')'.
	stat := string(data)
	idx := strings.LastIndex(stat, ")")
	if idx == -1 || idx+2 >= len(stat) {
		return false
	}
	return stat[idx+2] == 'Z'
}

func procStatPath(pid int) string {
	return "/proc/" + strconv.Itoa(pid) + "/stat"
}

// The regression this change exists to prevent: close() signaled the child and
// then discarded the handle, after which every method -- wait included -- reports
// "process is closed". Nothing could reap it, so each close() left a zombie.
//
// This drives procClose exactly as the Lua binding does, so it fails against the
// unfixed implementation.
func TestProcessCloseReapsTheChild(t *testing.T) {
	probe := newReapProbe()
	p := &Process{handle: probe, started: true}

	l := setupState()
	defer l.Close()

	value.PushTypedUserData(l, p, processTypeName)
	procClose(l)

	if !p.closed {
		t.Fatal("close should mark the process closed")
	}
	if !waitFor(t, 5*time.Second, func() bool { return probe.waits() == 1 }) {
		t.Fatalf("close must reap the child exactly once, got %d waits", probe.waits())
	}
	if sent := probe.sent(); len(sent) != 1 || sent[0] != int(syscall.SIGTERM) {
		t.Fatalf("expected a single SIGTERM, got %v", sent)
	}
}

// A process that was never started has no OS child to signal or reap. Cleanup
// is a no-op so an unused handle cannot make runtime shutdown fail.
func TestProcessCloseDoesNotReapUnstartedProcess(t *testing.T) {
	probe := newReapProbe()
	p := &Process{handle: probe, started: false}

	l := setupState()
	defer l.Close()

	value.PushTypedUserData(l, p, processTypeName)
	procClose(l)

	if sent := probe.sent(); len(sent) != 0 {
		t.Fatalf("unstarted process must not be signaled, got %v", sent)
	}

	if probe.waits() != 0 {
		t.Fatalf("an unstarted process must not be waited on, got %d waits", probe.waits())
	}
}
