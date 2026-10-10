// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/boot/deps/lock"
	"go.uber.org/zap"
)

const includeHostDependenciesKey = "workspace.include_host_dependencies"

// includeHostDependencies opts a published deployment into host dependency
// discovery. Source-only workspaces keep their existing behavior.
func includeHostDependencies(cfg boot.Config) (bool, error) {
	if cfg == nil {
		return false, nil
	}
	value, present := cfg.Get(includeHostDependenciesKey)
	if !present || value == nil {
		return false, nil
	}
	enabled, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be a boolean, got %T", includeHostDependenciesKey, value)
	}
	return enabled, nil
}

// lockedApplicationRoots keeps the application selected by the deployment.
// Startup uses its exact version; explicit updates may release that constraint.
func lockedApplicationRoots(locked *lock.Lock) ([]dependencyRequest, error) {
	if locked == nil {
		return nil, nil
	}
	var requests []dependencyRequest
	for _, root := range locked.GetRootModules() {
		org, name, ok := strings.Cut(root, "/")
		if !ok || org == "" || name == "" {
			return nil, fmt.Errorf("invalid deployment root %q", root)
		}
		module, _ := locked.GetModule(root)
		requests = append(requests, dependencyRequest{Org: org, Module: name, Constraint: module.Version})
	}
	return requests, nil
}

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
