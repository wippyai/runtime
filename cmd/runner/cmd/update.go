package cmd

import (
	"fmt"
	"os"

	"github.com/ponyruntime/pony/cmd/runner/app"
	"github.com/ponyruntime/pony/deps"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var updateCmd = &cobra.Command{
	Use:           "update",
	Short:         "Update dependencies and regenerate lock file",
	Long:          "If the lock file is missing, runs init. Resolves dependencies and calculates a diff. Writes a new lock file and runs install afterwards.",
	Args:          cobra.NoArgs,
	SilenceUsage:  true,
	SilenceErrors: false,
	RunE: func(cmd *cobra.Command, args []string) error {
		// CRITICAL: Early check - log to stderr immediately before any logger is created
		fmt.Fprintf(os.Stderr, "DEBUG: update command RunE called (cmd.Use='%s', args=%v)\n", cmd.Use, args)

		// Explicitly check for unexpected arguments on Windows
		if len(args) > 0 {
			fmt.Fprintf(os.Stderr, "ERROR: update command received unexpected arguments: %v\n", args)
			return fmt.Errorf("unexpected arguments: %v (command 'update' does not accept positional arguments)", args)
		}

		// Verify we're actually running the update command, not something else
		if cmd.Use != "update" {
			fmt.Fprintf(os.Stderr, "ERROR: expected 'update' command but got '%s'\n", cmd.Use)
			return fmt.Errorf("internal error: expected 'update' command but got '%s'", cmd.Use)
		}

		logger, err := createLogger()
		if err != nil {
			return fmt.Errorf("failed to create logger: %w", err)
		}

		logger.Info("Executing update command", zap.String("command", cmd.Use), zap.Strings("args", args))

		return runUpdate(cmd, args, logger)
	},
}

func runUpdate(cmd *cobra.Command, _ []string, logger *zap.Logger) error {
	lockFile, _ := cmd.Flags().GetString("lock-file")
	folderPath := "."

	logger.Info("Updating dependencies")

	// Check if lock file exists and load old lock file BEFORE update to compare later
	lockPath, err := deps.FindLockFile(folderPath, lockFile)
	var oldLock *deps.LockFile
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

	logger.Info("Update completed successfully")
	return nil
}

func init() {
	rootCmd.AddCommand(updateCmd)

	updateCmd.Flags().StringP("lock-file", "l", "wippy.lock", "path to lock file")
	updateCmd.Flags().StringP("src-dir", "d", ".", "source directory path")
	updateCmd.Flags().StringP("modules-dir", "m", ".wippy", "modules directory path")
}
