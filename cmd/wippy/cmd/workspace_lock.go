// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/wippyai/runtime/boot/deps/lock"
	"go.uber.org/zap"
)

// An update runs install in the same process, and install without a lock file
// runs update. One process holds the exclusive workspace lock once.
var workspaceChange = struct {
	sync.Mutex
	held map[string]*workspaceHold
}{held: make(map[string]*workspaceHold)}

type workspaceHold struct {
	lock *lock.WorkspaceLock
	refs int
}

// holdWorkspaceForChange takes the exclusive workspace lock for an install or
// update. It refuses while a runtime runs from the workspace, because the
// change replaces module trees that the runtime has open.
func holdWorkspaceForChange(lockFilePath, modulesDir string) (func(), error) {
	path, err := filepath.Abs(lock.RuntimeLockPath(filepath.Dir(lockFilePath), modulesDir))
	if err != nil {
		return nil, NewWorkspaceLockError(err)
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
func holdWorkspaceForRuntime(lockFilePath string, logger *zap.Logger) (func(), error) {
	if _, err := os.Stat(lockFilePath); err != nil {
		return func() {}, nil
	}
	lockObj, err := lock.New(lockFilePath)
	if err != nil {
		return nil, NewLoadLockFileError(err)
	}
	path := lock.RuntimeLockPath(filepath.Dir(lockObj.Path()), lockObj.GetDirectories().Modules)
	held, err := lock.ShareWorkspace(path, func() {
		logger.Info("waiting for an install or update in this workspace to finish", zap.String("lock", path))
	})
	if err != nil {
		return nil, NewWorkspaceLockError(err)
	}
	return func() { _ = held.Release() }, nil
}
