// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	bootapi "github.com/wippyai/runtime/api/boot"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/boot/deps/lock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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

	releaseRuntime, err := holdWorkspaceForRuntime(workspaceConfig(lockFile), zap.NewNop())
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
	release, err := holdWorkspaceForRuntime(workspaceConfig(filepath.Join(t.TempDir(), lock.DefaultFilename)), zap.NewNop())
	require.NoError(t, err)
	release()
}

func TestConfiguredLockProtectsLoadedWorkspace(t *testing.T) {
	root := t.TempDir()
	custom := filepath.Join(root, "deployment.lock")
	require.NoError(t, os.WriteFile(custom, []byte("directories:\n    modules: .wippy\n    src: ./src\n"), 0o600))
	cfg := bootapi.NewConfig(bootapi.WithSection("registry", map[string]any{"dependency_lock_path": custom}))
	// The guard and entry loader must select the same configured deployment.
	actual, err := lock.Find(".", cfg.GetString("registry.dependency_lock_path", ""))
	require.NoError(t, err)
	require.Equal(t, actual, runtimeLockFile(cfg))
	releaseRuntime, err := holdWorkspaceForRuntime(cfg, zap.NewNop())
	require.NoError(t, err)
	defer releaseRuntime()
	releaseUpdate, err := holdWorkspaceForChange(actual, ".wippy")
	if releaseUpdate != nil {
		defer releaseUpdate()
	}
	require.Error(t, err, "the runtime's selected deployment must block destructive CLI updates")
}

func TestChangingModulesDirectoryCannotBypassRuntime(t *testing.T) {
	path := writeWorkspaceLockFile(t)
	releaseRuntime, err := holdWorkspaceForRuntime(workspaceConfig(path), zap.NewNop())
	require.NoError(t, err)
	defer releaseRuntime()
	releaseUpdate, err := holdWorkspaceForChange(path, "other-modules")
	if releaseUpdate != nil {
		defer releaseUpdate()
	}
	require.Error(t, err, "update must not rewrite a live deployment's lock by choosing a different destination")
}

func workspaceConfig(path string) bootapi.Config {
	return bootapi.NewConfig(bootapi.WithSection("registry", map[string]any{"dependency_lock_path": path}))
}

func requireWorkspaceAvailable(t *testing.T, path string) {
	t.Helper()
	held, err := lock.ExclusiveWorkspace(path)
	require.NoError(t, err)
	require.NoError(t, held.Release())
}

func TestWorkspaceChangeRetainsBothTreesThroughNestedInstall(t *testing.T) {
	path := writeWorkspaceLockFile(t)
	releaseUpdate, err := holdWorkspaceForChange(path, "new-modules")
	require.NoError(t, err)
	t.Cleanup(releaseUpdate)
	current, err := lock.New(path)
	require.NoError(t, err)
	current.SetDirectories(lock.Directories{Modules: "new-modules", Src: "./src"})
	require.NoError(t, current.Write())
	releaseInstall, err := holdWorkspaceForChange(path, "")
	require.NoError(t, err)
	releaseInstall()

	paths := []string{
		deploymentGuardPath(path),
		lock.RuntimeLockPath(filepath.Dir(path), ".wippy"),
		lock.RuntimeLockPath(filepath.Dir(path), "new-modules"),
	}
	for _, guarded := range paths {
		held, err := lock.ExclusiveWorkspace(guarded)
		if held != nil {
			_ = held.Release()
		}
		require.ErrorIs(t, err, lock.ErrWorkspaceBusy, guarded)
	}
	releaseUpdate()
	for _, guarded := range paths {
		requireWorkspaceAvailable(t, guarded)
	}
}

func TestWorkspaceChangeReleasesPartialAcquisition(t *testing.T) {
	path := writeWorkspaceLockFile(t)
	destination := lock.RuntimeLockPath(filepath.Dir(path), "new-modules")
	busy, err := lock.ShareWorkspace(destination, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = busy.Release() })
	release, err := holdWorkspaceForChange(path, "new-modules")
	if release != nil {
		t.Cleanup(release)
	}
	require.Error(t, err)
	requireWorkspaceAvailable(t, deploymentGuardPath(path))
	requireWorkspaceAvailable(t, lock.RuntimeLockPath(filepath.Dir(path), ".wippy"))
}

func TestWorkspaceGuardsReleasedOnInvalidLock(t *testing.T) {
	path := writeWorkspaceLockFile(t)
	require.NoError(t, os.WriteFile(path, []byte("[invalid yaml"), 0o600))
	for _, acquire := range []func() (func(), error){
		func() (func(), error) { return holdWorkspaceForChange(path, "") },
		func() (func(), error) { return holdWorkspaceForRuntime(workspaceConfig(path), zap.NewNop()) },
	} {
		release, err := acquire()
		if release != nil {
			t.Cleanup(release)
		}
		require.Error(t, err)
		requireWorkspaceAvailable(t, deploymentGuardPath(path))
	}
}

func TestDifferentDeploymentsSharingModulesCannotChangeRunningTree(t *testing.T) {
	path := writeWorkspaceLockFile(t)
	other := filepath.Join(filepath.Dir(path), "other.lock")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(other, data, 0o600))
	releaseRuntime, err := holdWorkspaceForRuntime(workspaceConfig(path), zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(releaseRuntime)
	release, err := holdWorkspaceForChange(other, "")
	if release != nil {
		t.Cleanup(release)
	}
	require.Error(t, err)
	requireWorkspaceAvailable(t, deploymentGuardPath(other))
}

func TestRuntimeWaitsForUpdateAndReadsNewModulesDirectory(t *testing.T) {
	path := writeWorkspaceLockFile(t)
	releaseUpdate, err := holdWorkspaceForChange(path, "new-modules")
	require.NoError(t, err)
	t.Cleanup(releaseUpdate)
	waiting := make(chan struct{})
	var once sync.Once
	logger := zap.NewExample(zap.Hooks(func(entry zapcore.Entry) error {
		if entry.Message == "waiting for an install or update in this workspace to finish" {
			once.Do(func() { close(waiting) })
		}
		return nil
	}))
	finish := make(chan struct{})
	done := make(chan struct{})
	t.Cleanup(func() {
		releaseUpdate()
		close(finish)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("runtime guard did not release")
		}
	})
	started := make(chan error, 1)
	go func() {
		defer close(done)
		release, err := holdWorkspaceForRuntime(workspaceConfig(path), logger)
		if release != nil {
			defer release()
		}
		started <- err
		<-finish
	}()
	select {
	case <-waiting:
	case <-time.After(5 * time.Second):
		t.Fatal("runtime did not wait for update")
	}
	current, err := lock.New(path)
	require.NoError(t, err)
	current.SetDirectories(lock.Directories{Modules: "new-modules", Src: "./src"})
	require.NoError(t, current.Write())
	releaseUpdate()
	select {
	case err := <-started:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("runtime did not start after update released the workspace")
	}
	requireWorkspaceAvailable(t, lock.RuntimeLockPath(filepath.Dir(path), ".wippy"))
	held, err := lock.ExclusiveWorkspace(lock.RuntimeLockPath(filepath.Dir(path), "new-modules"))
	if held != nil {
		_ = held.Release()
	}
	require.ErrorIs(t, err, lock.ErrWorkspaceBusy)
}

func TestRuntimeLockFileSelection(t *testing.T) {
	assert.Equal(t, defaultLockFile, runtimeLockFile(nil))
	assert.Equal(t, defaultLockFile, runtimeLockFile(workspaceConfig("")))
	assert.Equal(t, "custom.lock", runtimeLockFile(workspaceConfig("custom.lock")))
}

func symlinkWorkspacePath(t *testing.T, target, alias string) {
	t.Helper()
	if err := os.Symlink(target, alias); err != nil {
		if runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
			t.Skipf("symlink privilege unavailable: %v", err)
		}
		require.NoError(t, err)
	}
}

func TestWorkspaceChangeHandlesModulesDirectoryAlias(t *testing.T) {
	path := writeWorkspaceLockFile(t)
	root := filepath.Dir(path)
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".wippy"), 0o755))
	symlinkWorkspacePath(t, filepath.Join(root, ".wippy"), filepath.Join(root, "alias"))
	release, err := holdWorkspaceForChange(path, "alias")
	require.NoError(t, err)
	release()
	requireWorkspaceAvailable(t, deploymentGuardPath(path))
	requireWorkspaceAvailable(t, lock.RuntimeLockPath(root, ".wippy"))
}

func TestSymlinkedDeploymentCannotBypassRuntime(t *testing.T) {
	path := writeWorkspaceLockFile(t)
	alias := filepath.Join(filepath.Dir(path), "alias.lock")
	symlinkWorkspacePath(t, path, alias)
	releaseRuntime, err := holdWorkspaceForRuntime(workspaceConfig(alias), zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(releaseRuntime)
	release, err := holdWorkspaceForChange(path, "new-modules")
	if release != nil {
		t.Cleanup(release)
	}
	require.Error(t, err)
}

func TestRuntimeGuardReportsStatErrors(t *testing.T) {
	// A NUL is invalid on every supported platform. A file used as a parent
	// reports ENOTDIR on Unix but a missing path on Windows.
	path := filepath.Join(t.TempDir(), "invalid\x00.lock")
	release, err := holdWorkspaceForRuntime(workspaceConfig(path), zap.NewNop())
	if release != nil {
		t.Cleanup(release)
	}
	require.Error(t, err, "a stat error must not silently disable protection")
}

func TestRuntimeReleasesDeploymentGuardWhenModulesLockFails(t *testing.T) {
	path := writeWorkspaceLockFile(t)
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(path), ".wippy"), []byte("file"), 0o600))
	release, err := holdWorkspaceForRuntime(workspaceConfig(path), zap.NewNop())
	if release != nil {
		t.Cleanup(release)
	}
	require.Error(t, err)
	requireWorkspaceAvailable(t, deploymentGuardPath(path))
}

func TestEmptyModulesDirectoryUsesDefaultGuard(t *testing.T) {
	path := writeWorkspaceLockFile(t)
	require.NoError(t, os.WriteFile(path, []byte("directories: {src: ./src, modules: ''}\n"), 0o600))
	other := filepath.Join(filepath.Dir(path), "other.lock")
	require.NoError(t, os.WriteFile(other, []byte("directories: {src: ./src, modules: ''}\n"), 0o600))
	releaseRuntime, err := holdWorkspaceForRuntime(workspaceConfig(path), zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(releaseRuntime)
	release, err := holdWorkspaceForChange(other, "")
	if release != nil {
		t.Cleanup(release)
	}
	require.Error(t, err)
	requireWorkspaceAvailable(t, deploymentGuardPath(other))
}
