package cmd

import (
	"context"
	"fmt"

	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/runtime/cmd/internal/entries"
	"go.uber.org/zap"
)

func loadValidatedLock(folderPath, lockFile string) (string, *lock.Lock, error) {
	lockPath, err := lock.Find(folderPath, lockFile)
	if err != nil {
		return "", nil, NewLockFileNotFoundError(err)
	}

	lockObj, err := lock.New(lockPath)
	if err != nil {
		return lockPath, nil, NewLoadLockFileError(fmt.Errorf("lock file %s: %w", lockPath, err))
	}

	if err := lock.Validate(lockObj); err != nil {
		return lockPath, nil, NewInvalidLockFileError(fmt.Errorf("lock file %s: %w", lockObj.Path(), err))
	}

	return lockPath, lockObj, nil
}

// ensureModulesAndLoadEntries is the shared lock-driven entry load flow used by
// run/list, lint, and registry commands.
//
// When installMissing is true, missing lock modules are installed before loading
// entry sources; when false, it stays read-only and only attempts to load entries
// from already present lock paths.
func ensureModulesAndLoadEntries(
	ctx context.Context,
	lockPath string,
	lockObj *lock.Lock,
	logger *zap.Logger,
	installMissing bool,
) ([]regapi.Entry, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	if installMissing {
		if err := entries.EnsureModulesInstalled(ctx, lockPath, logger.Named("modules")); err != nil {
			return nil, NewEnsureModulesInstalledError(err)
		}
	}

	allEntries, err := loadEntriesFromLockPaths(ctx, lockObj, logger)
	if err != nil {
		return nil, NewLoadEntriesError(fmt.Sprintf("lock paths (%s)", lockPath), err)
	}

	return allEntries, nil
}
