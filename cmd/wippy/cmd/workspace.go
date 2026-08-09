// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"fmt"
	"os"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/boot/deps/lock"
	"go.uber.org/zap"
)

func newConfiguredLock(path string, cfg boot.Config, logger *zap.Logger) (*lock.Lock, error) {
	lockObj, err := lock.New(path, lock.WithWorkspaceConfig(cfg))
	if err != nil {
		return nil, err
	}
	warnTrackedLockReplacements(lockObj, logger)
	return lockObj, nil
}

func warnTrackedLockReplacements(lockObj *lock.Lock, logger *zap.Logger) {
	if lockObj == nil || logger == nil || len(lockObj.GetTrackedReplacements()) == 0 {
		return
	}
	if silentLogs {
		_, _ = fmt.Fprintf(os.Stderr, "\nWARNING: DEPRECATED replacements in %s\nMove them to workspace.replacements in a runtime config file; lock-file replacement support will be removed.\n\n", lockObj.Path())
		return
	}
	logger.Warn("DEPRECATED: lock-file replacements are loaded only for compatibility; move them to workspace.replacements in a runtime config file",
		zap.String("lock_file", lockObj.Path()))
}
