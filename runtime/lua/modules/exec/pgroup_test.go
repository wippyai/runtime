// SPDX-License-Identifier: MPL-2.0

//go:build !windows

package exec

import (
	"bufio"
	"context"
	"io"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	rtresource "github.com/wippyai/runtime/api/runtime/resource"
	execapi "github.com/wippyai/runtime/api/service/exec"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/service/exec/native"
	"go.uber.org/zap"
)

// processTreeCommand prints the pid of a grandchild that only a group-wide
// signal can reach, then keeps the direct child alive until it is stopped.
const processTreeCommand = "sh -c 'sleep 300 & echo $!; wait'"

// treeAlive reports whether the pid is still signalable; a reaped process
// answers ESRCH.
func treeAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }

// newTreeProcess builds a real native process in its own process group and
// wraps it in the Lua handle, exactly as executor:exec does.
func newTreeProcess(ctx context.Context, t *testing.T, group bool) (*Process, io.Reader) {
	t.Helper()
	executor := native.NewNativeExecutor(zap.NewNop(), &execapi.NativeExecutorConfig{ProcessGroup: group})
	handle, err := executor.NewProcess(processTreeCommand, execapi.ProcessOptions{})
	require.NoError(t, err)
	return NewProcess(ctx, handle), handle.Stdout()
}

// startTree drives the Lua start binding and returns the grandchild pid the
// child reports.
func startTree(t *testing.T, l *lua.LState, process *Process, stdout io.Reader) int {
	t.Helper()
	value.PushTypedUserData(l, process, processTypeName)
	require.Equal(t, 2, procStart(l))
	l.SetTop(1)

	pids := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			pids <- strings.TrimSpace(scanner.Text())
		}
		close(pids)
	}()
	select {
	case line, ok := <-pids:
		require.True(t, ok, "child produced no output")
		pid, err := strconv.Atoi(line)
		require.NoErrorf(t, err, "expected a pid on the first output line, got %q", line)
		t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
		return pid
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the grandchild pid")
		return 0
	}
}

// close() releases the whole tree a harness spawned, not just the command it
// started: the tool subprocesses are grandchildren of that command.
func TestCloseReleasesTheProcessGroupTree(t *testing.T) {
	ctx, frame := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(frame)
	store := rtresource.NewStore()
	require.NoError(t, rtresource.SetStore(ctx, store))
	defer func() { _ = store.Close() }()

	l := setupState()
	defer l.Close()

	process, stdout := newTreeProcess(ctx, t, true)
	grandchild := startTree(t, l, process, stdout)
	require.True(t, treeAlive(grandchild))

	procClose(l)

	assert.True(t, waitFor(t, 10*time.Second, func() bool { return !treeAlive(grandchild) }),
		"grandchild %d survived close() of a process-group child", grandchild)
}

// The forced close escalates straight to SIGKILL, which must also address the
// group.
func TestForcedCloseKillsTheProcessGroupTree(t *testing.T) {
	ctx, frame := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(frame)
	store := rtresource.NewStore()
	require.NoError(t, rtresource.SetStore(ctx, store))
	defer func() { _ = store.Close() }()

	l := setupState()
	defer l.Close()

	process, stdout := newTreeProcess(ctx, t, true)
	grandchild := startTree(t, l, process, stdout)

	l.Push(lua.LTrue)
	procClose(l)

	assert.True(t, waitFor(t, 10*time.Second, func() bool { return !treeAlive(grandchild) }),
		"grandchild %d survived close(true) of a process-group child", grandchild)
}

// The cleanup that runs when the owning Lua process exits takes the same path,
// so it must release the tree too.
func TestRuntimeCleanupReleasesTheProcessGroupTree(t *testing.T) {
	ctx, frame := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(frame)
	store := rtresource.NewStore()
	require.NoError(t, rtresource.SetStore(ctx, store))

	l := setupState()
	defer l.Close()

	process, stdout := newTreeProcess(ctx, t, true)
	grandchild := startTree(t, l, process, stdout)

	require.NoError(t, store.Close())

	assert.True(t, waitFor(t, 10*time.Second, func() bool { return !treeAlive(grandchild) }),
		"grandchild %d outlived the owning process", grandchild)
}

// Without the option the child shares the runtime's group, so only it is
// signaled. That is the defined behavior for callers that rely on it.
func TestCloseWithoutProcessGroupLeavesTheGrandchild(t *testing.T) {
	ctx, frame := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(frame)
	store := rtresource.NewStore()
	require.NoError(t, rtresource.SetStore(ctx, store))
	defer func() { _ = store.Close() }()

	l := setupState()
	defer l.Close()

	process, stdout := newTreeProcess(ctx, t, false)
	grandchild := startTree(t, l, process, stdout)

	procClose(l)

	assert.True(t, treeAlive(grandchild),
		"the default close must not reach beyond the direct child")
}

// pid() is the evidence a caller records for an attempt.
func TestProcessPidBinding(t *testing.T) {
	ctx, frame := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(frame)
	store := rtresource.NewStore()
	require.NoError(t, rtresource.SetStore(ctx, store))
	defer func() { _ = store.Close() }()

	l := setupState()
	defer l.Close()

	process, stdout := newTreeProcess(ctx, t, true)

	value.PushTypedUserData(l, process, processTypeName)
	require.Equal(t, 2, procPid(l))
	require.Equal(t, lua.LNil, l.Get(-2))
	pidErr, ok := l.Get(-1).(*lua.Error)
	require.True(t, ok)
	assert.Contains(t, pidErr.Error(), "process not started")
	l.SetTop(0)

	grandchild := startTree(t, l, process, stdout)

	require.Equal(t, 2, procPid(l))
	pid, ok := l.Get(-2).(lua.LInteger)
	require.True(t, ok, "pid must be an integer")
	assert.Positive(t, int(pid))
	assert.NotEqual(t, grandchild, int(pid))
	l.SetTop(1)

	procClose(l)
}

func TestParseProcessOptionsProcessGroup(t *testing.T) {
	l := setupState()
	defer l.Close()

	table := l.NewTable()
	table.RawSetString("process_group", lua.LTrue)
	options, err := parseProcessOptions(table)
	require.NoError(t, err)
	require.NotNil(t, options.ProcessGroup)
	assert.True(t, *options.ProcessGroup)
	assert.Equal(t, true, processSecurityMeta(options)["process_group"])

	table.RawSetString("process_group", lua.LFalse)
	options, err = parseProcessOptions(table)
	require.NoError(t, err)
	require.NotNil(t, options.ProcessGroup)
	assert.False(t, *options.ProcessGroup)
	assert.Equal(t, false, processSecurityMeta(options)["process_group"])

	table.RawSetString("process_group", lua.LString("yes"))
	_, err = parseProcessOptions(table)
	require.ErrorContains(t, err, "process_group must be a boolean")

	// An unset option leaves the executor default in force and reports nothing
	// requested to the policy.
	empty, err := parseProcessOptions(l.NewTable())
	require.NoError(t, err)
	assert.Nil(t, empty.ProcessGroup)
	assert.Equal(t, false, processSecurityMeta(empty)["process_group"])
}
