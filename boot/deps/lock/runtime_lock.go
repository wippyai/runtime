// SPDX-License-Identifier: MPL-2.0

package lock

import (
	"errors"
	"os"
	"path/filepath"
)

// RuntimeLockFilename names the workspace lock file inside the modules
// directory. A running runtime holds a shared lock on it; install and update
// hold an exclusive lock while they change the module trees.
const RuntimeLockFilename = "runtime.lock"

// ErrWorkspaceBusy reports that another process holds a conflicting lock on
// the workspace lock file.
var ErrWorkspaceBusy = errors.New("workspace lock is held by another process")

// RuntimeLockPath returns the workspace lock file for the directory of a lock
// file and its configured modules directory.
func RuntimeLockPath(lockDir, modulesDir string) string {
	if modulesDir == "" {
		modulesDir = ".wippy"
	}
	if filepath.Base(modulesDir) == "vendor" {
		modulesDir = filepath.Dir(modulesDir)
	}
	return filepath.Join(ResolveLockPath(lockDir, modulesDir), RuntimeLockFilename)
}

// WorkspaceLock is a held lock on the workspace lock file. The operating
// system releases it when the holder exits, so a crashed process never blocks
// the workspace.
type WorkspaceLock struct {
	file *os.File
}

// ShareWorkspace takes a shared lock. Any number of runtimes share it. While
// an install or update holds the exclusive lock, it calls waiting once and
// then waits for the lock.
func ShareWorkspace(path string, waiting func()) (*WorkspaceLock, error) {
	file, err := openWorkspaceLock(path)
	if err != nil {
		return nil, err
	}
	err = lockFile(file, false, false)
	if errors.Is(err, ErrWorkspaceBusy) {
		if waiting != nil {
			waiting()
		}
		err = lockFile(file, false, true)
	}
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return &WorkspaceLock{file: file}, nil
}

// ExclusiveWorkspace takes the exclusive lock without waiting. It returns
// ErrWorkspaceBusy while a runtime, an install or an update holds the lock.
func ExclusiveWorkspace(path string) (*WorkspaceLock, error) {
	file, err := openWorkspaceLock(path)
	if err != nil {
		return nil, err
	}
	if err := lockFile(file, true, false); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &WorkspaceLock{file: file}, nil
}

// Release unlocks and closes the lock file.
func (l *WorkspaceLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := errors.Join(unlockFile(l.file), l.file.Close())
	l.file = nil
	return err
}

func openWorkspaceLock(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
}
