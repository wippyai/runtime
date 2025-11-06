package cmd

import (
	"fmt"

	"github.com/ponyruntime/pony/cmd/runner/app"
	"github.com/ponyruntime/pony/deps"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update dependencies and regenerate lock file",
	Long:  "If the lock file is missing, runs init. Resolves dependencies and calculates a diff. Writes a new lock file and runs install afterwards (unless --lock flag is used).",
	RunE: func(cmd *cobra.Command, args []string) error {
		logger, err := createLogger()
		if err != nil {
			return fmt.Errorf("failed to create logger: %w", err)
		}

		return runUpdate(cmd, args, logger)
	},
}

func runUpdate(cmd *cobra.Command, _ []string, logger *zap.Logger) error {
	lockFile, _ := cmd.Flags().GetString("lock-file")
	lockOnly, _ := cmd.Flags().GetBool("lock")
	folderPath := "."

	if lockOnly {
		logger.Info("Updating dependencies (lock file only, skipping installation)")
	} else {
		logger.Info("Updating dependencies")
	}

	// Check if lock file exists and load old lock file BEFORE update to compare later
	lockPath, err := deps.FindLockFile(folderPath, lockFile)
	var oldLock *deps.LockFile
	if err != nil {
		logger.Info("Lock file not found, running init")

		// Create empty lock file with default directories
		lockFileObj := &deps.LockFile{
			Directories: deps.Directories{
				Modules: deps.DefaultModulesDir,
				Src:     deps.DefaultSrcDir,
			},
			Modules: []deps.LockedModule{},
		}

		// Save the lock file
		if err := lockFileObj.SaveLockFile(lockFile); err != nil {
			logger.Error("failed to initialize lock file", zap.Error(err))
			return fmt.Errorf("failed to initialize lock file: %w", err)
		}

		logger.Info("Lock file initialized",
			zap.String("path", lockFile),
			zap.String("src_dir", deps.DefaultSrcDir),
			zap.String("modules_dir", deps.DefaultModulesDir))
		// oldLock remains nil for new lock file
	} else {
		logger.Debug("Found existing lock file", zap.String("path", lockPath))
		// Load existing lock file to save it for comparison
		if existingLock, err := deps.LoadLockFile(lockPath); err == nil {
			oldLock = existingLock
			logger.Debug("Current lock file state",
				zap.String("src_dir", existingLock.Directories.Src),
				zap.String("modules_dir", existingLock.Directories.Modules),
				zap.Int("current_modules", len(existingLock.Modules)),
				zap.Int("replacements", len(existingLock.Replacements)))

			if len(existingLock.Modules) > 0 {
				logger.Debug("Current modules in lock file:")
				for i, module := range existingLock.Modules {
					logger.Debug(fmt.Sprintf("  %d. %s: %s", i+1, module.Name, module.Version))
				}
			}
		}
	}

	// Update dependencies
	depsManager := app.NewDependencyManager(folderPath, lockFile, logger)
	logger.Info("Starting dependency resolution and update process")

	if err := depsManager.UpdateDependencies(cmd.Context()); err != nil {
		logger.Error("failed to update dependencies", zap.Error(err))
		return fmt.Errorf("failed to update dependencies: %w", err)
	}

	logger.Info("Dependencies updated and lock file regenerated")

	// Load new lock file AFTER update to compare
	newLock, err := deps.LoadLockFile(lockFile)
	if err != nil {
		logger.Warn("Failed to load updated lock file for comparison", zap.Error(err))
	} else {
		changes := deps.CalculateChanges(oldLock, newLock)
		if len(changes.Installed)+len(changes.Updated)+len(changes.Removed) > 0 {
			logger.Info(fmt.Sprintf("Package operations: %d installed, %d updated, %d removed",
				len(changes.Installed), len(changes.Updated), len(changes.Removed)))
			for _, op := range changes.Installed {
				logger.Info(fmt.Sprintf(" - Installing %s: %s", op.Name, op.Version))
			}
			for _, op := range changes.Updated {
				logger.Info(fmt.Sprintf(" - Updating %s: %s → %s", op.Name, op.OldVersion, op.Version))
			}
			for _, op := range changes.Removed {
				logger.Info(fmt.Sprintf(" - Removing %s: %s", op.Name, op.Version))
			}
		} else {
			logger.Debug("Package operations summary",
				zap.Int("installed", len(changes.Installed)),
				zap.Int("updated", len(changes.Updated)),
				zap.Int("removed", len(changes.Removed)))
		}
	}

	// Install dependencies unless --lock flag is set
	if !lockOnly {
		logger.Info("Installing dependencies from updated lock file")

		// Validate replacements before installation
		if err := newLock.ValidateReplacements(lockFile); err != nil {
			logger.Error("invalid replacement paths", zap.Error(err))
			return fmt.Errorf("invalid replacement paths: %w", err)
		}

		if err := depsManager.InstallDependenciesFromLockFile(cmd.Context(), newLock, lockFile); err != nil {
			logger.Error("failed to install dependencies", zap.Error(err))
			return fmt.Errorf("failed to install dependencies: %w", err)
		}

		logger.Info("Dependencies installed successfully")
	} else {
		logger.Info("Skipping installation (--lock flag is set)")
	}

	logger.Info("Update completed successfully")
	return nil
}

func init() {
	rootCmd.AddCommand(updateCmd)

	updateCmd.Flags().StringP("lock-file", "l", "wippy.lock", "path to lock file")
	updateCmd.Flags().Bool("lock", false, "only update lock file without installing dependencies")
}
