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
	Long:  "If the lock file is missing, runs init. Resolves dependencies and calculates a diff. Writes a new lock file and runs install afterwards.",
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
	folderPath := "."

	logger.Info("Updating dependencies")

	// Check if lock file exists
	lockPath, err := deps.FindLockFile(folderPath, lockFile)
	if err != nil {
		logger.Info("Lock file not found, running init")

		// Create default init parameters
		srcDir, _ := cmd.Flags().GetString("src-dir")
		modulesDir, _ := cmd.Flags().GetString("modules-dir")

		if srcDir == "" {
			srcDir = "."
		}
		if modulesDir == "" {
			modulesDir = ".wippy"
		}

		// Create empty lock file with directories
		lockFileObj := &deps.LockFile{
			Directories: deps.Directories{
				Modules: modulesDir,
				Src:     srcDir,
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
			zap.String("src_dir", srcDir),
			zap.String("modules_dir", modulesDir))
	} else {
		logger.Debug("Found existing lock file", zap.String("path", lockPath))

		// Load existing lock file to show current state
		if existingLock, err := deps.LoadLockFile(lockPath); err == nil {
			logger.Info("Current lock file state",
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

	// Run install after update
	logger.Info("Running install after update")

	// Load the updated lock file to show what will be installed
	if updatedLock, err := deps.LoadLockFile(lockFile); err == nil {
		logger.Info("Updated lock file contents",
			zap.Int("total_modules", len(updatedLock.Modules)))

		if len(updatedLock.Modules) > 0 {
			logger.Info(fmt.Sprintf("Lock file operations: %d installs, %d updates, 0 removals:", len(updatedLock.Modules), len(updatedLock.Modules)))
			for _, module := range updatedLock.Modules {
				logger.Info(fmt.Sprintf("- %s: %s", module.Name, module.Version))
			}
		}
	}

	if err := depsManager.InstallDependencies(cmd.Context()); err != nil {
		logger.Error("failed to install updated dependencies", zap.Error(err))
		return fmt.Errorf("failed to install updated dependencies: %w", err)
	}

	logger.Info("Update completed successfully")
	return nil
}

func init() {
	rootCmd.AddCommand(updateCmd)

	updateCmd.Flags().StringP("lock-file", "l", "wippy.lock", "path to lock file")
	updateCmd.Flags().StringP("src-dir", "s", ".", "source directory path")
	updateCmd.Flags().StringP("modules-dir", "m", ".wippy", "modules directory path")
}
