// SPDX-License-Identifier: MPL-2.0

package app

import (
	"errors"
	"os"
	"path/filepath"
)

// The state directory layout the runner owns.
const (
	lockFilename        = "lock"
	deploymentsDir      = "deployments"
	currentFilename     = "current"
	historyFilename     = "registry.db"
	recoveryDir         = "recovery"
	receiptFilename     = "receipt.json"
	cacheDir            = "cache"
	configFilename      = ".wippy.yaml"
	updatePrefix        = "update-"
	updateStagingPrefix = ".update-"
)

func deploymentsPath(state string) string { return filepath.Join(state, deploymentsDir) }

func currentPath(state string) string { return filepath.Join(state, currentFilename) }

func historyPath(state string) string { return filepath.Join(state, historyFilename) }

func recoveryHistoryPath(state string) string {
	return filepath.Join(state, recoveryDir, historyFilename)
}

func receiptPath(state string) string { return filepath.Join(state, recoveryDir, receiptFilename) }

func cachePath(state string) string { return filepath.Join(state, cacheDir) }

// configFiles returns the runtime configuration files the state carries.
func configFiles(state string) ([]string, error) {
	path := filepath.Join(state, configFilename)
	if _, err := os.Stat(path); err == nil {
		return []string{path}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, NewApplicationStateError("inspect application configuration", path, err)
	}
	return []string{}, nil
}

// lockState takes exclusive ownership of the state directory and returns the
// release. ErrOwned reports the invocation that already holds it.
func lockState(state string) (func() error, error) {
	path := filepath.Join(state, lockFilename)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, NewApplicationStateError("open application state lock", path, err)
	}
	unlock, err := tryLockFile(file)
	if err != nil {
		_ = file.Close()
		if errors.Is(err, errLockBusy) {
			return nil, NewOwnedStateError(err)
		}
		return nil, NewApplicationStateError("take application state lock", path, err)
	}
	return func() error {
		unlockErr := unlock()
		return errors.Join(unlockErr, file.Close())
	}, nil
}

// writeRecord replaces path with data through a synced temporary file, so a
// reader sees either the previous record or the complete new one.
func writeRecord(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return NewApplicationStateError("create state record directory", directory, err)
	}
	pending, err := os.CreateTemp(directory, ".record-")
	if err != nil {
		return NewApplicationStateError("create state record", path, err)
	}
	pendingPath := pending.Name()
	defer func() { _ = os.Remove(pendingPath) }()
	if _, err := pending.Write(data); err != nil {
		_ = pending.Close()
		return NewApplicationStateError("write state record", path, err)
	}
	if err := pending.Sync(); err != nil {
		_ = pending.Close()
		return NewApplicationStateError("sync state record", path, err)
	}
	if err := pending.Close(); err != nil {
		return NewApplicationStateError("close state record", path, err)
	}
	if err := os.Rename(pendingPath, path); err != nil {
		return NewApplicationStateError("place state record", path, err)
	}
	return nil
}
