package cmd

import (
	"fmt"

	"github.com/ponyruntime/pony/cmd/runner/app"
	"github.com/ponyruntime/pony/deps"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var updateCmd = &cobra.Command{
	Use:   "update [packages...]",
	Short: "Update dependencies and regenerate lock file",
	Long: `Update dependencies and regenerate wippy.lock file

Updates all dependencies by default, or only specified packages if provided.
After updating, automatically installs the resolved dependencies.

If the lock file is missing, runs 'wippy init' first.

Examples:
  wippy update                    # Update all dependencies
  wippy update acme/http          # Update only acme/http
  wippy update acme/http demo/sql # Update multiple packages`,
	RunE: runUpdate,
}

func init() {
	rootCmd.AddCommand(updateCmd)

	updateCmd.Flags().StringP("lock-file", "l", "wippy.lock", "path to lock file")
	updateCmd.Flags().StringP("src-dir", "d", ".", "source directory path")
	updateCmd.Flags().StringP("modules-dir", "m", ".wippy", "modules directory path")
}

func runUpdate(cmd *cobra.Command, args []string) error {
	logger, err := CreateLogger()
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}
	defer logger.Sync()

	lockFile, _ := cmd.Flags().GetString("lock-file")
	folderPath := "."

	targetPackages := args
	if len(targetPackages) > 0 {
		logger.Info("updating specific packages", zap.Strings("packages", targetPackages))
	} else {
		logger.Info("updating all dependencies")
	}

	lockPath, err := deps.FindLockFile(folderPath, lockFile)
	var oldLock *deps.LockFile

	if err != nil {
		logger.Info("lock file not found, running init")

		if err := runInit(cmd, []string{}); err != nil {
			return fmt.Errorf("init failed: %w", err)
		}

		lockPath = lockFile
	} else {
		logger.Debug("found existing lock file", zap.String("path", lockPath))

		if existingLock, err := deps.LoadLockFile(lockPath); err == nil {
			oldLock = existingLock
			logger.Debug("current lock file state",
				zap.String("src_dir", existingLock.Directories.Src),
				zap.String("modules_dir", existingLock.Directories.Modules),
				zap.Int("current_modules", len(existingLock.Modules)),
				zap.Int("replacements", len(existingLock.Replacements)))
		}
	}

	depsManager := app.NewDependencyManager(folderPath, lockFile, logger)
	logger.Info("starting dependency resolution and update process")

	if err := depsManager.UpdateDependencies(cmd.Context()); err != nil {
		logger.Error("failed to update dependencies", zap.Error(err))
		return fmt.Errorf("failed to update dependencies: %w", err)
	}

	logger.Info("dependencies updated and lock file regenerated")

	newLock, err := deps.LoadLockFile(lockFile)
	if err != nil {
		logger.Warn("failed to load updated lock file for comparison", zap.Error(err))
	} else {
		changes := deps.CalculateChanges(oldLock, newLock)

		filteredChanges := changes
		if len(targetPackages) > 0 {
			filteredChanges = filterOldFormatChanges(changes, targetPackages)
		}

		if len(filteredChanges.Installed)+len(filteredChanges.Updated)+len(filteredChanges.Removed) > 0 {
			logger.Info(fmt.Sprintf("package operations: %d installed, %d updated, %d removed",
				len(filteredChanges.Installed), len(filteredChanges.Updated), len(filteredChanges.Removed)))

			for _, op := range filteredChanges.Installed {
				logger.Info(fmt.Sprintf(" - installing %s: %s", op.Name, op.Version))
			}
			for _, op := range filteredChanges.Updated {
				logger.Info(fmt.Sprintf(" - updating %s: %s → %s", op.Name, op.OldVersion, op.Version))
			}
			for _, op := range filteredChanges.Removed {
				logger.Info(fmt.Sprintf(" - removing %s: %s", op.Name, op.Version))
			}
		} else {
			logger.Info("no changes detected")
		}
	}

	logger.Info("running install to apply changes")
	if err := runInstall(cmd, []string{}); err != nil {
		return fmt.Errorf("install failed: %w", err)
	}

	logger.Info("update completed successfully")
	return nil
}

func filterOldFormatChanges(changes *deps.LockFileChanges, targetPackages []string) *deps.LockFileChanges {
	if len(targetPackages) == 0 {
		return changes
	}

	targets := make(map[string]bool)
	for _, pkg := range targetPackages {
		targets[pkg] = true
	}

	filtered := &deps.LockFileChanges{
		Installed: []deps.ModuleOperation{},
		Updated:   []deps.ModuleOperation{},
		Removed:   []deps.ModuleOperation{},
	}

	for _, op := range changes.Installed {
		if targets[op.Name] {
			filtered.Installed = append(filtered.Installed, op)
		}
	}

	for _, op := range changes.Updated {
		if targets[op.Name] {
			filtered.Updated = append(filtered.Updated, op)
		}
	}

	for _, op := range changes.Removed {
		if targets[op.Name] {
			filtered.Removed = append(filtered.Removed, op)
		}
	}

	return filtered
}
