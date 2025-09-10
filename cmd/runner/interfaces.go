package main

import (
	"context"

	regapi "github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/moduleloader"
)

// DependencyInstaller defines the interface for dependency installation operations
type DependencyInstaller interface {
	InstallDependencies(ctx context.Context) error
	InstallDependenciesSilent(ctx context.Context) (*OperationStats, error)
	UpdateDependencies(ctx context.Context) error
}

// ModuleStatsProvider defines the interface for providing module statistics
type ModuleStatsProvider interface {
	GetModuleStats() []moduleloader.ModuleStats
	DisplayModuleStatistics(stats []moduleloader.ModuleStats)
}

// LockFileManager defines the interface for lock file operations
type LockFileManager interface {
	LoadLockFile(path string) (*moduleloader.LockFile, error)
	SaveLockFile(lockFile *moduleloader.LockFile, path string) error
	CalculateChanges(oldLock, newLock *moduleloader.LockFile) *LockFileChanges
}

// RegistryLoader defines the interface for loading registry entries
type RegistryLoader interface {
	LoadEntries(ctx context.Context, srcDir string) ([]regapi.Entry, error)
	CreateManager(entries []regapi.Entry) *moduleloader.Manager
}
