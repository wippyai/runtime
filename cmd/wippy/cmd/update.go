package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/client"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/lock"
	transcoder "github.com/wippyai/runtime/system/payload"
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

	updateCmd.Flags().StringP("lock-file", "l", "wippy.lock", "path to lock file")
	updateCmd.Flags().StringP("src-dir", "d", ".", "source directory path")
	updateCmd.Flags().StringP("modules-dir", "m", ".wippy", "modules directory path")
}

func runUpdate(cmd *cobra.Command, args []string) error {
	app, err := InitApp(cmd.Context())
	if err != nil {
		return fmt.Errorf("init app: %w", err)
	}

	logger := app.Logger.Named("update")

	lockFilePath, _ := cmd.Flags().GetString("lock-file")
	srcDir, _ := cmd.Flags().GetString("src-dir")
	modulesDir, _ := cmd.Flags().GetString("modules-dir")

	// Targeted update if modules specified
	if len(args) > 0 {
		return runTargetedUpdate(cmd, lockFilePath, srcDir, modulesDir, args, app)
	}

	// Full update otherwise
	logger.Info("re-resolving all dependencies from source")

	// Load old lock file for comparison
	var oldLockObj *lock.Lock
	if stat, err := os.Stat(lockFilePath); err == nil && !stat.IsDir() {
		oldLockObj, _ = lock.New(lockFilePath)
	}

	// Scan source directory for dependencies
	logger.Info("scanning source directory", zap.String("src_dir", srcDir))

	dirFS := os.DirFS(srcDir)
	entries, err := app.Loader.LoadFS(app.Ctx, dirFS)
	if err != nil {
		return fmt.Errorf("load entries from source: %w", err)
	}

	// Extract root dependencies from entries
	rootDeps := extractRootDependencies(entries)
	logger.Info("found root dependencies", zap.Int("count", len(rootDeps)))

	if len(rootDeps) == 0 {
		logger.Info("no dependencies found, nothing to update")
		return nil
	}

	// Build dependency graph using graph builder
	manifestBridge, err := client.NewManifestBridge(app.RegistryClient, app.Transcoder, logger.Named("manifest"), 100)
	if err != nil {
		return fmt.Errorf("create manifest bridge: %w", err)
	}

	builder := graph.NewBuilder(manifestBridge)

	logger.Info("resolving dependency graph")
	result, err := builder.Build(app.Ctx, graph.BuildInput{
		RootDependencies: rootDeps,
	})
	if err != nil {
		return fmt.Errorf("build dependency graph: %w", err)
	}

	if len(result.Conflicts) > 0 {
		logger.Error("dependency conflicts detected", zap.Int("count", len(result.Conflicts)))
		for _, conflict := range result.Conflicts {
			logger.Error("conflict",
				zap.String("module", conflict.Module.String()),
				zap.String("reason", conflict.Reason.String()),
				zap.String("message", conflict.Message))
		}
		return fmt.Errorf("dependency conflicts detected: %d conflicts", len(result.Conflicts))
	}

	logger.Info("dependency graph resolved",
		zap.Int("total_modules", result.Stats.TotalModules),
		zap.Int("total_levels", result.Stats.TotalLevels))

	// Convert BuildResult to lock file
	newLockObj := convertBuildResultToLock(result, modulesDir, srcDir)

	// Preserve replacements from old lock file
	if oldLockObj != nil {
		replacements := oldLockObj.GetReplacements()
		for _, repl := range replacements {
			newLockObj.SetReplacement(repl)
		}
	}

	// Save lock file
	if err := newLockObj.Write(); err != nil {
		return fmt.Errorf("write lock file: %w", err)
	}

	logger.Info("lock file updated")

	// Compare old and new
	if oldLockObj != nil {
		changes := lock.Diff(oldLockObj, newLockObj)
		logChanges(logger, changes)
	}

	// Run install to download modules
	logger.Info("running install to download modules")
	if err := runInstall(cmd, []string{}); err != nil {
		return fmt.Errorf("install failed: %w", err)
	}

	logger.Info("update completed successfully")
	return nil
}

func extractRootDependencies(entries []regapi.Entry) []graph.DependencyRequest {
	deps := []graph.DependencyRequest{}
	seen := make(map[string]bool)

	for _, entry := range entries {
		if entry.Kind != "ns.dependency" {
			continue
		}

		var depData struct {
			Component string `json:"component"`
			Version   string `json:"version"`
		}

		if err := transcoder.GlobalTranscoder().Unmarshal(entry.Data, &depData); err != nil {
			continue
		}

		if depData.Component == "" {
			continue
		}

		name, err := graph.ParseName(depData.Component)
		if err != nil {
			continue
		}

		key := name.String() + "@" + depData.Version
		if seen[key] {
			continue
		}
		seen[key] = true

		deps = append(deps, graph.DependencyRequest{
			Name:       name,
			Constraint: depData.Version,
		})
	}

	return deps
}

func convertBuildResultToLock(result *graph.BuildResult, modulesDir, srcDir string) *lock.Lock {
	lockObj, _ := lock.New("wippy.lock")

	lockObj.SetDirectories(lock.Directories{
		Modules: modulesDir,
		Src:     srcDir,
	})

	for _, resolved := range result.ResolvedModules {
		lockObj.SetModule(lock.Module{
			Name:    resolved.Name.String(),
			Version: resolved.Version,
			Hash:    resolved.CommitID,
		})
	}

	return lockObj
}

func runTargetedUpdate(cmd *cobra.Command, lockFilePath, srcDir, modulesDir string, targetModules []string, app *AppContext) error {
	logger := app.Logger.Named("update")
	logger.Info("updating specific modules", zap.Strings("modules", targetModules))

	// Load current lock file
	lockObj, err := lock.New(lockFilePath)
	if err != nil {
		return fmt.Errorf("load lock file: %w", err)
	}

	oldLockObj, _ := lock.New(lockFilePath)

	// Scan source to get constraints
	dirFS := os.DirFS(srcDir)
	entries, err := app.Loader.LoadFS(app.Ctx, dirFS)
	if err != nil {
		return fmt.Errorf("load entries from source: %w", err)
	}

	// Extract source constraints
	rootDeps := extractRootDependencies(entries)
	sourceConstraints := make(map[string]string)
	for _, dep := range rootDeps {
		sourceConstraints[dep.Name.String()] = dep.Constraint
	}

	// Build frozen constraints from lock file (all modules except targets)
	targetSet := make(map[string]bool)
	for _, name := range targetModules {
		targetSet[name] = true
	}

	frozenDeps := []graph.DependencyRequest{}
	for _, mod := range lockObj.GetModules() {
		if !targetSet[mod.Name] {
			// Fixed constraint for locked modules
			name, err := graph.ParseName(mod.Name)
			if err != nil {
				continue
			}
			frozenDeps = append(frozenDeps, graph.DependencyRequest{
				Name:       name,
				Constraint: "=" + mod.Version, // Exact version constraint
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

		name, err := graph.ParseName(moduleName)
		if err != nil {
			return fmt.Errorf("parse module name %s: %w", moduleName, err)
		}

		frozenDeps = append(frozenDeps, graph.DependencyRequest{
			Name:       name,
			Constraint: constraint,
		})
	}

	// Setup registry client
	manifestBridge, err := client.NewManifestBridge(app.RegistryClient, app.Transcoder, logger.Named("manifest"), 100)
	if err != nil {
		return fmt.Errorf("create manifest bridge: %w", err)
	}

	builder := graph.NewBuilder(manifestBridge)

	logger.Info("resolving with frozen dependencies")
	result, err := builder.Build(app.Ctx, graph.BuildInput{
		RootDependencies: frozenDeps,
	})
	if err != nil {
		return fmt.Errorf("build dependency graph: %w", err)
	}

	if len(result.Conflicts) > 0 {
		logger.Error("conflicts detected", zap.Int("count", len(result.Conflicts)))
		for _, conflict := range result.Conflicts {
			logger.Error("conflict",
				zap.String("module", conflict.Module.String()),
				zap.String("reason", conflict.Reason.String()),
				zap.String("message", conflict.Message))
		}
		return fmt.Errorf("update not possible: %d conflicts detected", len(result.Conflicts))
	}

	// Build new lock file
	newLockObj := convertBuildResultToLock(result, modulesDir, srcDir)

	// Preserve replacements
	for _, repl := range lockObj.GetReplacements() {
		newLockObj.SetReplacement(repl)
	}

	// Detect changes
	changes := lock.Diff(oldLockObj, newLockObj)

	// Check if any non-target modules would be updated
	nonTargetUpdates := []lock.ModuleChange{}
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
			if _, err := fmt.Scanln(&response); err != nil && response == "" {
				response = "Y"
			}
			if response != "" && response != "Y" && response != "y" {
				logger.Info("update cancelled by user")
				return nil
			}
		}
	}

	// Save lock file
	if err := newLockObj.Write(); err != nil {
		return fmt.Errorf("write lock file: %w", err)
	}

	logger.Info("lock file updated")
	logChanges(logger, changes)

	// Run install
	logger.Info("running install to download modules")
	if err := runInstall(cmd, []string{}); err != nil {
		return fmt.Errorf("install failed: %w", err)
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
