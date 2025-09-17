package main

import (
	"context"

	"github.com/ponyruntime/pony/deps"

	regapi "github.com/ponyruntime/pony/api/registry"
)

// DependencyInstaller defines the interface for dependency installation operations
type DependencyInstaller interface {
	InstallDependencies(ctx context.Context) error
	UpdateDependencies(ctx context.Context) error
}

// ModuleStatsProvider defines the interface for providing module statistics
type ModuleStatsProvider interface {
	GetModuleStats() []deps.ModuleStats
}

// LockFileManager defines the interface for lock file operations
type LockFileManager interface {
	LoadLockFile(path string) (*deps.LockFile, error)
	SaveLockFile(lockFile *deps.LockFile, path string) error
	CalculateChanges(oldLock, newLock *deps.LockFile) *deps.LockFileChanges
}

// RegistryLoader defines the interface for loading registry entries
type RegistryLoader interface {
	LoadEntries(ctx context.Context, srcDir string) ([]regapi.Entry, error)
	CreateManager(entries []regapi.Entry) *deps.Manager
}
