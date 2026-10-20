// SPDX-License-Identifier: MPL-2.0

package lock

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const holdWorkspaceLockEnv = "WIPPY_TEST_HOLD_WORKSPACE_LOCK"

func TestRuntimeLockPath(t *testing.T) {
	root := filepath.Join("/", "app")
	assert.Equal(t, filepath.Join(root, ".wippy", RuntimeLockFilename), RuntimeLockPath(root, ""))
	assert.Equal(t, filepath.Join(root, ".wippy", RuntimeLockFilename), RuntimeLockPath(root, ".wippy"))
	assert.Equal(t, filepath.Join(root, "mods", RuntimeLockFilename), RuntimeLockPath(root, filepath.Join("mods", "vendor")))
	abs := filepath.Join("/", "var", "mods")
	assert.Equal(t, filepath.Join(abs, RuntimeLockFilename), RuntimeLockPath(root, abs))
}

func TestWorkspaceLockSharedRuntimesBlockUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".wippy", RuntimeLockFilename)

	first, err := ShareWorkspace(path, nil)
	require.NoError(t, err)
	second, err := ShareWorkspace(path, func() { t.Error("a shared lock must not wait for another shared lock") })
	require.NoError(t, err)

	_, err = ExclusiveWorkspace(path)
	require.ErrorIs(t, err, ErrWorkspaceBusy)

	require.NoError(t, first.Release())
	_, err = ExclusiveWorkspace(path)
	require.ErrorIs(t, err, ErrWorkspaceBusy, "one runtime still holds the workspace")

	require.NoError(t, second.Release())
	update, err := ExclusiveWorkspace(path)
	require.NoError(t, err)
	require.NoError(t, update.Release())
	require.NoError(t, update.Release())
}

func TestWorkspaceLockRuntimeWaitsForUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), RuntimeLockFilename)
	update, err := ExclusiveWorkspace(path)
	require.NoError(t, err)

	_, err = ExclusiveWorkspace(path)
	require.ErrorIs(t, err, ErrWorkspaceBusy)

	waiting := make(chan struct{})
	acquired := make(chan *WorkspaceLock, 1)
	go func() {
		held, shareErr := ShareWorkspace(path, func() { close(waiting) })
		assert.NoError(t, shareErr)
		acquired <- held
	}()

	select {
	case <-waiting:
	case <-time.After(5 * time.Second):
		t.Fatal("the runtime did not report that it waits for the update")
	}
	select {
	case <-acquired:
		t.Fatal("the runtime took the workspace while the update held it")
	case <-time.After(100 * time.Millisecond):
	}

	require.NoError(t, update.Release())
	select {
	case held := <-acquired:
		require.NoError(t, held.Release())
	case <-time.After(5 * time.Second):
		t.Fatal("the runtime did not take the workspace after the update")
	}
}

// A lock held by a process that dies is released by the operating system.
func TestWorkspaceLockCrashedHolderDoesNotBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), RuntimeLockFilename)
	cmd := exec.Command(os.Args[0], "-test.run=^TestWorkspaceLockHolderProcess$")
	cmd.Env = append(os.Environ(), holdWorkspaceLockEnv+"="+path)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })

	line, err := bufio.NewReader(stdout).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "locked\n", line)

	_, err = ExclusiveWorkspace(path)
	require.ErrorIs(t, err, ErrWorkspaceBusy)

	require.NoError(t, cmd.Process.Kill())
	_ = cmd.Wait()

	update, err := ExclusiveWorkspace(path)
	require.NoError(t, err)
	require.NoError(t, update.Release())
}

func TestWorkspaceLockHolderProcess(t *testing.T) {
	path := os.Getenv(holdWorkspaceLockEnv)
	if path == "" {
		t.Skip("helper process for TestWorkspaceLockCrashedHolderDoesNotBlock")
	}
	held, err := ShareWorkspace(path, nil)
	if err != nil {
		os.Exit(2)
	}
	_ = held
	_, _ = os.Stdout.WriteString("locked\n")
	time.Sleep(time.Minute)
	os.Exit(0)
}
