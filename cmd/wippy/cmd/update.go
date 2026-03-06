// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	bootauth "github.com/wippyai/runtime/boot/deps/auth"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
	appinit "github.com/wippyai/runtime/cmd/internal/app"
	"go.uber.org/zap"
)

var updateCmd = &cobra.Command{
	Use:   "update [module...]",
	Short: "Update dependencies and regenerate lock file",
	Long: `Update dependencies and regenerate wippy.lock file

Without arguments, scans source directory and re-resolves the entire dependency graph,
updating all modules to their latest compatible versions.

With module arguments, updates only the specified modules to their highest version
compatible with other locked dependencies. New transitive dependencies are auto-added.
If updating would require changing other locked modules, shows impact and asks for confirmation.

Examples:
  wippy update                    # Re-resolve all dependencies from source
  wippy update acme/http          # Update only acme/http
  wippy update acme/http demo/sql # Update multiple specific modules`,
	RunE: runUpdate,
}

func init() {
	rootCmd.AddCommand(updateCmd)

	updateCmd.Flags().StringP("lock-file", "l", defaultLockFile, "path to lock file")
	updateCmd.Flags().StringP("src-dir", "d", "./src", "source directory path")
	updateCmd.Flags().String("modules-dir", ".wippy", "modules directory path")
	updateCmd.Flags().String("registry", "", "registry URL (default: from credentials)")
}

func runUpdate(cmd *cobra.Command, args []string) error {
	app, err := appinit.Init(cmd.Context(), verbose, veryVerbose, console, silentLogs, appStartTime)
	if err != nil {
		return NewInitAppError(err)
	}

	logger := app.Logger.Named("update")

	lockFilePath, _ := cmd.Flags().GetString("lock-file")
	registryURL, _ := cmd.Flags().GetString("registry")

	// Get flag values and check if explicitly set
	srcDir, _ := cmd.Flags().GetString("src-dir")
	modulesDir, _ := cmd.Flags().GetString("modules-dir")
	srcDirChanged := cmd.Flags().Changed("src-dir")
	modulesDirChanged := cmd.Flags().Changed("modules-dir")

	// Load existing lock file to get current directories
	var existingDirs *lock.Directories
	if stat, err := os.Stat(lockFilePath); err == nil && !stat.IsDir() {
		if existingLock, err := lock.New(lockFilePath); err == nil {
			dirs := existingLock.GetDirectories()
			existingDirs = &dirs
		}
	}

	// Use existing directories unless flags explicitly override
	if existingDirs != nil {
		if !srcDirChanged && existingDirs.Src != "" {
			srcDir = existingDirs.Src
		}
		if !modulesDirChanged && existingDirs.Modules != "" {
			modulesDir = existingDirs.Modules
		}
	}

	// Get auth credentials
	projectDir, _ := os.Getwd()
	authCfg := bootauth.NewConfig(projectDir)
	store := bootauth.NewStore(authCfg)

	if registryURL == "" {
		registryURL = store.DefaultRegistry()
	}

	cred, _ := store.Get(registryURL)
	var token string
	if cred != nil {
		token = cred.Token
	}

	// Create hub client
	hubClient, err := hub.NewClient(hub.Options{
		BaseURL: registryURL,
		Token:   token,
	})
	if err != nil {
		return NewCreateHubClientError(fmt.Errorf("registry %s: %w", registryURL, err))
	}

	// Targeted update if modules specified
	if len(args) > 0 {
		return runTargetedUpdate(cmd, lockFilePath, srcDir, modulesDir, args, app, hubClient)
	}

	// Full update otherwise
	logger.Info("re-resolving all dependencies from source")

	// Load old lock file for comparison
	var oldLockObj *lock.Lock
	if stat, err := os.Stat(lockFilePath); err == nil && !stat.IsDir() {
		oldLockObj, _ = lock.New(lockFilePath)
		if oldLockObj != nil {
			if err := lock.Validate(oldLockObj); err != nil {
				return NewInvalidExistingLockFileError(fmt.Errorf("lock file %s: %w", lockFilePath, err))
			}
		}
	}

	// Scan source directory for dependencies
	logger.Info("scanning source directory", zap.String("src_dir", srcDir))

	dirFS := os.DirFS(srcDir)
	entries, err := app.Loader.LoadFS(app.Ctx, dirFS)
	if err != nil {
		return NewLoadEntriesFromSourceError(fmt.Errorf("source dir %s: %w", srcDir, err))
	}

	// Extract root dependencies from entries
	rootDeps := extractRootDependencies(entries, app.Transcoder)
	logger.Info("found root dependencies", zap.Int("count", len(rootDeps)))

	resolvedModules := make([]hub.ResolvedModule, 0)
	if len(rootDeps) == 0 {
		logger.Info("no root dependencies found in source, pruning lock modules")
	} else {
		// Convert to hub dependency specs
		hubDeps := make([]hub.DependencySpec, 0, len(rootDeps))
		for _, dep := range rootDeps {
			hubDeps = append(hubDeps, hub.DependencySpec{
				Org:        dep.Org,
				Name:       dep.Module,
				Constraint: dep.Constraint,
			})
		}

		logger.Info("resolving dependency graph")
		result, err := hub.Resolve(app.Ctx, hubClient, hubDeps, nil)
		if err != nil {
			return NewBuildDependencyGraphError(err)
		}

		if len(result.Errors) > 0 {
			logger.Error("dependency resolution errors", zap.Int("count", len(result.Errors)))
			for _, resErr := range result.Errors {
				logger.Error("error", zap.String("module", resErr.Org+"/"+resErr.Name), zap.String("reason", resErr.Message))
			}
			return newResolutionConflictsError("dependency conflicts detected", result.Errors)
		}

		logger.Info("dependency graph resolved", zap.Int("total_modules", len(result.Modules)))
		resolvedModules = result.Modules
	}

	// Convert resolved modules to lock file
	newLockObj, err := convertResolvedToLock(lockFilePath, resolvedModules, modulesDir, srcDir)
	if err != nil {
		return NewLoadLockFileError(err)
	}

	// Preserve only replacements that still map to present modules.
	if oldLockObj != nil {
		preserveReplacementsForPresentModules(newLockObj, oldLockObj.GetReplacements())
	}

	// Save lock file
	if err := newLockObj.Write(); err != nil {
		return NewWriteLockFileError(fmt.Errorf("lock file %s: %w", newLockObj.Path(), err))
	}

	logger.Info("lock file updated")

	// Compare old and new
	var changes *lock.Changes
	if oldLockObj != nil {
		changes = lock.Diff(oldLockObj, newLockObj)
		logChanges(logger, changes)
		pruneStaleVendorArtifacts(newLockObj, changes, logger)
	}

	if len(resolvedModules) > 0 {
		// Run install to download modules
		logger.Info("running install to download modules")
		if err := runInstall(cmd, []string{}); err != nil {
			return NewInstallFailedError(err)
		}
	} else {
		logger.Info("no modules to install after update")
	}

	logger.Info("update completed successfully")
	return nil
}

type dependencyRequest struct {
	Org        string
	Module     string
	Constraint string
}

func extractRootDependencies(entries []regapi.Entry, dtt payload.Transcoder) []dependencyRequest {
	deps := make([]dependencyRequest, 0, len(entries))
	seen := make(map[string]bool)

	for _, entry := range entries {
		if entry.Kind != "ns.dependency" {
			continue
		}

		var depData struct {
			Component string `json:"component"`
			Version   string `json:"version"`
		}

		if err := dtt.Unmarshal(entry.Data, &depData); err != nil {
			continue
		}

		if depData.Component == "" {
			continue
		}

		parts := strings.SplitN(depData.Component, "/", 2)
		if len(parts) != 2 {
			continue
		}

		key := depData.Component + "@" + depData.Version
		if seen[key] {
			continue
		}
		seen[key] = true

		deps = append(deps, dependencyRequest{
			Org:        parts[0],
			Module:     parts[1],
			Constraint: depData.Version,
		})
	}

	return deps
}

func convertResolvedToLock(lockFilePath string, modules []hub.ResolvedModule, modulesDir, srcDir string) (*lock.Lock, error) {
	lockObj, err := lock.New(lockFilePath)
	if err != nil {
		return nil, fmt.Errorf("lock file %s: %w", lockFilePath, err)
	}

	lockObj.SetDirectories(lock.Directories{
		Modules: modulesDir,
		Src:     srcDir,
	})

	for _, m := range modules {
		lockObj.SetModule(lock.Module{
			Name:    fmt.Sprintf("%s/%s", m.Org, m.Name),
			Version: m.Version,
			Hash:    m.Digest,
		})
	}

	return lockObj, nil
}

func runTargetedUpdate(cmd *cobra.Command, lockFilePath, srcDir, modulesDir string, targetModules []string, app *appinit.Context, hubClient *hub.Client) error {
	logger := app.Logger.Named("update")
	logger.Info("updating specific modules", zap.Strings("modules", targetModules))

	// Load current lock file
	lockObj, err := lock.New(lockFilePath)
	if err != nil {
		return NewLoadLockFileError(fmt.Errorf("lock file %s: %w", lockFilePath, err))
	}

	if err := lock.Validate(lockObj); err != nil {
		return NewInvalidLockFileError(fmt.Errorf("lock file %s: %w", lockObj.Path(), err))
	}

	oldLockObj, _ := lock.New(lockFilePath)

	// Scan source to get constraints
	dirFS := os.DirFS(srcDir)
	entries, err := app.Loader.LoadFS(app.Ctx, dirFS)
	if err != nil {
		return NewLoadEntriesFromSourceError(fmt.Errorf("source dir %s: %w", srcDir, err))
	}

	// Extract source constraints
	rootDeps := extractRootDependencies(entries, app.Transcoder)
	sourceConstraints := make(map[string]string)
	for _, dep := range rootDeps {
		key := fmt.Sprintf("%s/%s", dep.Org, dep.Module)
		sourceConstraints[key] = dep.Constraint
	}

	// Build frozen constraints from lock file (all modules except targets)
	targetSet := make(map[string]bool)
	for _, name := range targetModules {
		targetSet[name] = true
	}

	modules := lockObj.GetModules()
	hubDeps := make([]hub.DependencySpec, 0, len(modules)+len(targetModules))

	for _, mod := range modules {
		parts := strings.SplitN(mod.Name, "/", 2)
		if len(parts) != 2 {
			continue
		}

		if !targetSet[mod.Name] {
			hubDeps = append(hubDeps, hub.DependencySpec{
				Org:        parts[0],
				Name:       parts[1],
				Constraint: "=" + mod.Version,
			})
		}
	}

	// Add target modules with source constraints
	for _, moduleName := range targetModules {
		constraint, ok := sourceConstraints[moduleName]
		if !ok {
			logger.Warn("module not found in source dependencies", zap.String("module", moduleName))
			continue
		}

		parts := strings.SplitN(moduleName, "/", 2)
		if len(parts) != 2 {
			return NewParseModuleNameError(moduleName, fmt.Errorf("invalid format, expected org/module"))
		}

		hubDeps = append(hubDeps, hub.DependencySpec{
			Org:        parts[0],
			Name:       parts[1],
			Constraint: constraint,
		})
	}

	logger.Info("resolving with frozen dependencies")
	result, err := hub.Resolve(app.Ctx, hubClient, hubDeps, nil)
	if err != nil {
		return NewBuildDependencyGraphError(err)
	}

	if len(result.Errors) > 0 {
		logger.Error("resolution errors", zap.Int("count", len(result.Errors)))
		for _, resErr := range result.Errors {
			logger.Error("error", zap.String("module", resErr.Org+"/"+resErr.Name), zap.String("reason", resErr.Message))
		}
		return newResolutionConflictsError("update conflicts detected", result.Errors)
	}

	// Build new lock file
	newLockObj, err := convertResolvedToLock(lockFilePath, result.Modules, modulesDir, srcDir)
	if err != nil {
		return NewLoadLockFileError(err)
	}

	// Preserve only replacements that still map to present modules.
	preserveReplacementsForPresentModules(newLockObj, lockObj.GetReplacements())

	// Detect changes
	changes := lock.Diff(oldLockObj, newLockObj)

	// Check if any non-target modules would be updated
	var nonTargetUpdates []lock.ModuleChange
	for _, change := range changes.Updated {
		if !targetSet[change.Name] {
			nonTargetUpdates = append(nonTargetUpdates, change)
		}
	}

	// Show impact if non-target modules would be updated
	if len(nonTargetUpdates) > 0 || len(changes.Installed) > 0 {
		logger.Warn("updating target modules would affect other dependencies")

		if len(changes.Installed) > 0 {
			logger.Info("new dependencies to be added", zap.Int("count", len(changes.Installed)))
			for _, mod := range changes.Installed {
				logger.Info("+ new", zap.String("module", mod.Name), zap.String("version", mod.Version))
			}
		}

		if len(nonTargetUpdates) > 0 {
			logger.Warn("other modules would also be updated", zap.Int("count", len(nonTargetUpdates)))
			for _, change := range nonTargetUpdates {
				logger.Warn("~ required update",
					zap.String("module", change.Name),
					zap.String("from", change.OldVersion),
					zap.String("to", change.NewVersion))
			}

			// Prompt user for confirmation
			fmt.Printf("\nProceed with update? [Y/n] ")
			var response string
			if _, err := fmt.Scanln(&response); err != nil || response == "" {
				response = "Y"
			}
			if response != "" && response != "Y" && response != "y" {
				logger.Info("update canceled by user")
				return nil
			}
		}
	}

	// Save lock file
	if err := newLockObj.Write(); err != nil {
		return NewWriteLockFileError(fmt.Errorf("lock file %s: %w", newLockObj.Path(), err))
	}

	logger.Info("lock file updated")
	logChanges(logger, changes)
	pruneStaleVendorArtifacts(newLockObj, changes, logger)

	// Run install
	logger.Info("running install to download modules")
	if err := runInstall(cmd, []string{}); err != nil {
		return NewInstallFailedError(err)
	}

	logger.Info("update completed successfully")
	return nil
}

func logChanges(logger *zap.Logger, changes *lock.Changes) {
	if len(changes.Installed)+len(changes.Updated)+len(changes.Removed) > 0 {
		logger.Info("changes detected",
			zap.Int("installed", len(changes.Installed)),
			zap.Int("updated", len(changes.Updated)),
			zap.Int("removed", len(changes.Removed)))

		for _, mod := range changes.Installed {
			logger.Info("+ installing", zap.String("module", mod.Name), zap.String("version", mod.Version))
		}
		for _, mod := range changes.Updated {
			logger.Info("~ updating", zap.String("module", mod.Name),
				zap.String("old", mod.OldVersion), zap.String("new", mod.NewVersion))
		}
		for _, mod := range changes.Removed {
			logger.Info("- removing", zap.String("module", mod.Name), zap.String("version", mod.Version))
		}
	} else {
		logger.Info("no changes detected")
	}
}

func preserveReplacementsForPresentModules(lockObj *lock.Lock, replacements []lock.Replacement) {
	if lockObj == nil || len(replacements) == 0 {
		return
	}

	modules := lockObj.GetModules()
	present := make(map[string]struct{}, len(modules))
	for _, mod := range modules {
		present[mod.Name] = struct{}{}
	}

	for _, repl := range replacements {
		if _, ok := present[repl.From]; ok {
			lockObj.SetReplacement(repl)
		}
	}
}

func pruneStaleVendorArtifacts(lockObj *lock.Lock, changes *lock.Changes, logger *zap.Logger) {
	if lockObj == nil || changes == nil {
		return
	}
	if len(changes.Removed) == 0 && len(changes.Updated) == 0 {
		return
	}

	lockDir := filepath.Dir(lockObj.Path())
	vendorDir := filepath.Join(lockDir, lockObj.GetVendorPath())

	for _, removed := range changes.Removed {
		pruneModuleArtifacts(vendorDir, removed.Name, removed.Version, true, logger)
	}
	for _, updated := range changes.Updated {
		pruneModuleArtifacts(vendorDir, updated.Name, updated.OldVersion, true, logger)
	}
}

func pruneModuleArtifacts(vendorDir, moduleName, moduleVersion string, removeCurrentDir bool, logger *zap.Logger) {
	name, err := graph.ParseName(moduleName)
	if err != nil {
		if logger != nil {
			logger.Debug("skipping stale module cleanup for invalid module name",
				zap.String("module", moduleName),
				zap.Error(err))
		}
		return
	}

	paths := []string{
		filepath.Join(vendorDir, lock.WappPath(name, moduleVersion)),
		filepath.Join(vendorDir, lock.LegacyModulePath(name, moduleVersion)),
	}
	if removeCurrentDir {
		paths = append(paths, filepath.Join(vendorDir, lock.ModulePath(name)))
	}

	for _, path := range paths {
		if err := os.RemoveAll(path); err != nil {
			if logger != nil {
				logger.Warn("failed to prune stale module artifact",
					zap.String("path", path),
					zap.Error(err))
			}
		}
	}
}

func newResolutionConflictsError(prefix string, errs []hub.ResolutionError) apierror.Error {
	if len(errs) == 0 {
		return apierror.New(apierror.Invalid, prefix+" (0)").WithRetryable(apierror.False)
	}
	details := make([]string, 0, len(errs))
	for _, resErr := range errs {
		details = append(details, formatResolutionError(resErr))
	}
	msg := fmt.Sprintf("%s (%d): %s", prefix, len(errs), strings.Join(details, "; "))
	return apierror.New(apierror.Invalid, msg).WithRetryable(apierror.False)
}

func formatResolutionError(resErr hub.ResolutionError) string {
	module := strings.Trim(resErr.Org+"/"+resErr.Name, "/")
	if module == "" {
		module = "unknown-module"
	}
	if resErr.Constraint != "" {
		module = module + "@" + resErr.Constraint
	}
	if resErr.Message != "" {
		return module + ": " + resErr.Message
	}
	return module
}
