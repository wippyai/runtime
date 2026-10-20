// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/boot/deps/lock"
	"go.uber.org/zap"
)

func writeWorkspaceLockFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, lock.DefaultFilename)
	require.NoError(t, os.WriteFile(path, []byte("directories:\n    modules: .wippy\n    src: ./src\n"), 0o600))
	return path
}

func TestWorkspaceChangeRefusedWhileRuntimeRuns(t *testing.T) {
	lockFile := writeWorkspaceLockFile(t)

	releaseRuntime, err := holdWorkspaceForRuntime(lockFile, zap.NewNop())
	require.NoError(t, err)

	_, err = holdWorkspaceForChange(lockFile, ".wippy")
	require.Error(t, err)
	var apiErr apierror.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, apierror.Conflict, apiErr.Kind())
	assert.Contains(t, err.Error(), "a Wippy runtime is running from this workspace")

	releaseRuntime()
	releaseChange, err := holdWorkspaceForChange(lockFile, ".wippy")
	require.NoError(t, err)
	releaseChange()
}

// Update runs install in the same process; both hold the one exclusive lock.
func TestWorkspaceChangeIsReentrantInOneProcess(t *testing.T) {
	lockFile := writeWorkspaceLockFile(t)

	releaseUpdate, err := holdWorkspaceForChange(lockFile, ".wippy")
	require.NoError(t, err)
	releaseInstall, err := holdWorkspaceForChange(lockFile, "")
	require.NoError(t, err)

	path := lock.RuntimeLockPath(filepath.Dir(lockFile), ".wippy")
	releaseInstall()
	_, err = lock.ExclusiveWorkspace(path)
	require.ErrorIs(t, err, lock.ErrWorkspaceBusy, "the update still holds the workspace")

	releaseUpdate()
	held, err := lock.ExclusiveWorkspace(path)
	require.NoError(t, err)
	require.NoError(t, held.Release())
}

func TestRuntimeWithoutLockFileHoldsNothing(t *testing.T) {
	release, err := holdWorkspaceForRuntime(filepath.Join(t.TempDir(), lock.DefaultFilename), zap.NewNop())
	require.NoError(t, err)
	release()
}
