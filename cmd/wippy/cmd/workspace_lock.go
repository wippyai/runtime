// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/boot/deps/lock"
	"go.uber.org/zap"
)

// An update runs install in the same process, and install without a lock file
// runs update. One process holds the exclusive workspace lock once.
var workspaceChange = struct {
	held map[string]*workspaceHold
	sync.Mutex
}{held: make(map[string]*workspaceHold)}

type workspaceHold struct {
	lock *lock.WorkspaceLock
	refs int
}

// holdWorkspaceForChange takes the exclusive workspace lock for an install or
// update. It refuses while a runtime runs from the workspace, because the
// change replaces module trees that the runtime has open.
func holdWorkspaceForChange(lockFilePath, modulesDir string) (func(), error) {
	// Protect the deployment before reading its current modules directory. This
	// identity stays fixed even when update changes directories in the lock file.
	releaseDeployment, err := holdWorkspaceChangePath(deploymentGuardPath(lockFilePath))
	if err != nil {
		return nil, err
	}
	releases := []func(){releaseDeployment}
	release := sync.OnceFunc(func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	})
	if _, err := os.Stat(lockFilePath); err != nil && !os.IsNotExist(err) {
		release()
		return nil, NewLoadLockFileError(err)
	}
	current, err := lock.New(lockFilePath)
	if err != nil {
		release()
		return nil, NewLoadLockFileError(err)
	}
	// Keep both the old tree and the destination protected through nested
	// update -> install, including after the deployment lock file is rewritten.
	dirs := []string{current.GetDirectories().Modules}
	if modulesDir != "" && modulesDir != dirs[0] {
		dirs = append(dirs, modulesDir)
	}
	for _, dir := range dirs {
		held, err := holdWorkspaceChangePath(lock.RuntimeLockPath(filepath.Dir(lockFilePath), dir))
		if err != nil {
			release()
			return nil, err
		}
		releases = append(releases, held)
	}
	return release, nil
}

func holdWorkspaceChangePath(lockPath string) (func(), error) {
	path, err := filepath.Abs(lockPath)
	if err != nil {
		return nil, NewWorkspaceLockError(err)
	}

	// Symlink aliases of one modules directory share the same in-process hold.
	if parent, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		path = filepath.Join(parent, filepath.Base(path))
	}
	workspaceChange.Lock()
	defer workspaceChange.Unlock()
	if hold, ok := workspaceChange.held[path]; ok {
		hold.refs++
		return func() { releaseWorkspaceChange(path) }, nil
	}
	held, err := lock.ExclusiveWorkspace(path)
	if errors.Is(err, lock.ErrWorkspaceBusy) {
		return nil, NewWorkspaceInUseError(path)
	}
	if err != nil {
		return nil, NewWorkspaceLockError(err)
	}
	workspaceChange.held[path] = &workspaceHold{lock: held, refs: 1}
	return func() { releaseWorkspaceChange(path) }, nil
}

// deploymentGuardPath follows a symlinked lock file without changing the base
// directory used to resolve its relative module paths.
func deploymentGuardPath(lockFilePath string) string {
	if resolved, err := filepath.EvalSymlinks(lockFilePath); err == nil {
		lockFilePath = resolved
	}
	return lockFilePath + ".runtime.lock"
}

func runtimeLockFile(cfg boot.Config) string {
	if cfg != nil {
		if selected := cfg.GetString("registry.dependency_lock_path", ""); selected != "" {
			return selected
		}
	}
	return defaultLockFile
}

func releaseWorkspaceChange(path string) {
	workspaceChange.Lock()
	defer workspaceChange.Unlock()
	hold, ok := workspaceChange.held[path]
	if !ok {
		return
	}
	hold.refs--
	if hold.refs > 0 {
		return
	}
	delete(workspaceChange.held, path)
	_ = hold.lock.Release()
}

// holdWorkspaceForRuntime takes the shared workspace lock for a runtime that
// starts from a lock file. It waits while an install or update holds the
// workspace. A runtime without a lock file holds nothing.
func holdWorkspaceForRuntime(cfg boot.Config, logger *zap.Logger) (func(), error) {
	lockFilePath := runtimeLockFile(cfg)
	if _, err := os.Stat(lockFilePath); err != nil {
		if os.IsNotExist(err) {
			return func() {}, nil
		}
		return nil, NewLoadLockFileError(err)
	}
	waiting := func() {
		logger.Info("waiting for an install or update in this workspace to finish", zap.String("lock_file", lockFilePath))
	}
	deployment, err := lock.ShareWorkspace(deploymentGuardPath(lockFilePath), waiting)
	if err != nil {
		return nil, NewWorkspaceLockError(err)
	}
	// Read after acquiring the deployment guard: an update we waited for may
	// have selected a different modules directory.
	lockObj, err := lock.New(lockFilePath)
	if err != nil {
		_ = deployment.Release()
		return nil, NewLoadLockFileError(err)
	}
	path := lock.RuntimeLockPath(filepath.Dir(lockObj.Path()), lockObj.GetDirectories().Modules)
	held, err := lock.ShareWorkspace(path, waiting)
	if err != nil {
		_ = deployment.Release()
		return nil, NewWorkspaceLockError(err)
	}
	return func() {
		_ = held.Release()
		_ = deployment.Release()
	}, nil
}
