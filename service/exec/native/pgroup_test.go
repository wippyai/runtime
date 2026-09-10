// SPDX-License-Identifier: MPL-2.0

//go:build !windows

package native

import (
	"bufio"
	"io"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/service/exec"
	"go.uber.org/zap"
)

// treeCommand prints the pid of a grandchild that outlives a signal sent to the
// direct child alone, then keeps the child alive until it is stopped.
const treeCommand = "sh -c 'sleep 300 & echo $!; wait'"

// readPID takes the first line the child writes and reads it as a pid.
func readPID(t *testing.T, reader io.Reader) int {
	t.Helper()
	lines := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(reader)
		if scanner.Scan() {
			lines <- strings.TrimSpace(scanner.Text())
		}
		close(lines)
	}()
	select {
	case line, ok := <-lines:
		require.True(t, ok, "child produced no output")
		pid, err := strconv.Atoi(line)
		require.NoErrorf(t, err, "expected a pid on the first output line, got %q", line)
		return pid
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the grandchild pid")
		return 0
	}
}

// alive reports whether the pid can still be signaled. A pid that has been
// reaped answers ESRCH.
func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func waitGone(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return !alive(pid)
}

// A signal sent to a process started with its own process group reaches every
// descendant that has not left the group, which is the whole point of the
// option: a harness spawns its tools as grandchildren.
func TestProcessGroupSignalReachesGrandchild(t *testing.T) {
	enabled := true
	executor := NewNativeExecutor(zap.NewNop(), &exec.NativeExecutorConfig{})
	process, err := executor.NewProcess(treeCommand, exec.ProcessOptions{ProcessGroup: &enabled})
	require.NoError(t, err)

	stdout := process.Stdout()
	require.NoError(t, process.Start())
	grandchild := readPID(t, stdout)
	require.True(t, alive(grandchild), "grandchild should be running")

	require.NoError(t, process.Signal(int(syscall.SIGKILL)))
	_ = process.Wait()

	assert.True(t, waitGone(grandchild, 5*time.Second),
		"grandchild %d survived a signal sent to the process group", grandchild)
}

// The default keeps the child in the runtime's own group, where a group signal
// would reach the runtime itself. Only the direct child is signaled.
func TestWithoutProcessGroupOnlyTheDirectChildIsSignaled(t *testing.T) {
	executor := NewNativeExecutor(zap.NewNop(), &exec.NativeExecutorConfig{})
	process, err := executor.NewProcess(treeCommand, exec.ProcessOptions{})
	require.NoError(t, err)

	stdout := process.Stdout()
	require.NoError(t, process.Start())
	grandchild := readPID(t, stdout)
	t.Cleanup(func() { _ = syscall.Kill(grandchild, syscall.SIGKILL) })

	require.NoError(t, process.Signal(int(syscall.SIGKILL)))
	_ = process.Wait()

	assert.True(t, alive(grandchild),
		"a signal to the direct child must not reach the grandchild by default")
}

// close(force) escalation and the runtime's owner-exit cleanup both go through
// Signal, so SIGTERM must address the group as well.
func TestProcessGroupTerminationReachesGrandchild(t *testing.T) {
	enabled := true
	executor := NewNativeExecutor(zap.NewNop(), &exec.NativeExecutorConfig{})
	process, err := executor.NewProcess(
		// The trap is set after the grandchild starts: a disposition of SIG_IGN
		// is inherited, so an earlier trap would make the grandchild ignore TERM
		// as well and the test would prove nothing.
		"sh -c 'sleep 300 & echo $!; trap \"\" TERM; wait'",
		exec.ProcessOptions{ProcessGroup: &enabled},
	)
	require.NoError(t, err)

	stdout := process.Stdout()
	require.NoError(t, process.Start())
	grandchild := readPID(t, stdout)
	t.Cleanup(func() { _ = syscall.Kill(grandchild, syscall.SIGKILL) })

	// The child ignores TERM; the grandchild does not, so the group delivery is
	// what is observed here.
	require.NoError(t, process.Signal(int(syscall.SIGTERM)))
	assert.True(t, waitGone(grandchild, 5*time.Second),
		"grandchild %d survived SIGTERM sent to the process group", grandchild)

	// The child's own wait returns once the grandchild is gone, so it exits too.
	_ = process.Wait()
}

// A PTY child is already a session leader of its own group. Requesting a
// process group there must not add a conflicting setpgid that fails the start.
func TestProcessGroupWithPTYStartsAndSignalsTheSession(t *testing.T) {
	enabled := true
	executor := NewNativeExecutor(zap.NewNop(), &exec.NativeExecutorConfig{})
	process, err := executor.NewProcess(treeCommand, exec.ProcessOptions{
		PTY:          &exec.PTYOptions{Width: 80, Height: 24},
		ProcessGroup: &enabled,
	})
	require.NoError(t, err)

	// A PTY child has no pipes: the master exists only once the process starts,
	// and the caller that acquires it owns its close.
	require.NoError(t, process.Start())
	stdout := process.Stdout()
	require.NotNil(t, stdout)
	t.Cleanup(func() { _ = stdout.Close() })

	grandchild := readPID(t, stdout)
	t.Cleanup(func() { _ = syscall.Kill(grandchild, syscall.SIGKILL) })

	require.NoError(t, process.Signal(int(syscall.SIGKILL)))
	_ = process.Wait()

	assert.True(t, waitGone(grandchild, 5*time.Second),
		"grandchild %d survived a signal sent to the PTY session group", grandchild)
}

// The executor default applies when the command says nothing, and an explicit
// per-command value wins in both directions.
func TestProcessGroupExecutorDefaultAndPerCommandOverride(t *testing.T) {
	enabled, disabled := true, false
	tests := []struct {
		option      *bool
		name        string
		configured  bool
		wantEnabled bool
	}{
		{name: "default off", configured: false, option: nil, wantEnabled: false},
		{name: "default on", configured: true, option: nil, wantEnabled: true},
		{name: "command enables", configured: false, option: &enabled, wantEnabled: true},
		{name: "command disables", configured: true, option: &disabled, wantEnabled: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := NewNativeExecutor(zap.NewNop(), &exec.NativeExecutorConfig{ProcessGroup: test.configured})
			process, err := executor.NewProcess("true", exec.ProcessOptions{ProcessGroup: test.option})
			require.NoError(t, err)

			native, ok := process.(*ProcessExecutor)
			require.True(t, ok)
			assert.Equal(t, test.wantEnabled, native.processGroup)
			if test.wantEnabled {
				require.NotNil(t, native.cmd.SysProcAttr)
				assert.True(t, native.cmd.SysProcAttr.Setpgid)
			} else if native.cmd.SysProcAttr != nil {
				assert.False(t, native.cmd.SysProcAttr.Setpgid)
			}
		})
	}
}

// The pid is the evidence a caller records for an attempt, so it is readable
// only once there is a child to identify.
func TestProcessPidRequiresAStartedChild(t *testing.T) {
	executor := NewNativeExecutor(zap.NewNop(), &exec.NativeExecutorConfig{})
	process, err := executor.NewProcess("sh -c 'exit 0'", exec.ProcessOptions{})
	require.NoError(t, err)

	identity, ok := process.(exec.ProcessIdentity)
	require.True(t, ok, "native processes expose their pid")

	_, err = identity.Pid()
	require.ErrorIs(t, err, ErrProcessNotStarted)

	require.NoError(t, process.Start())
	pid, err := identity.Pid()
	require.NoError(t, err)
	assert.Positive(t, pid)
	assert.Equal(t, process.(*ProcessExecutor).cmd.Process.Pid, pid)

	require.NoError(t, process.Wait())
}
