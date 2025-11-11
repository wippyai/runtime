package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ponyruntime/pony/cmd/runner/app"
	"github.com/ponyruntime/pony/deps"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var installCmd = &cobra.Command{
	Use:           "install",
	Short:         "Install dependencies from lock file",
	Long:          "Reads dependencies from the lock file and installs them. If the lock file is missing, behaves like update.",
	Args:          cobra.NoArgs,
	SilenceUsage:  true,
	SilenceErrors: false,
	RunE: func(cmd *cobra.Command, args []string) error {
		// CRITICAL: Early check - log to stderr immediately before any logger is created
		fmt.Fprintf(os.Stderr, "DEBUG: install command RunE called (cmd.Use='%s', args=%v)\n", cmd.Use, args)

		// Explicitly check for unexpected arguments on Windows
		if len(args) > 0 {
			fmt.Fprintf(os.Stderr, "ERROR: install command received unexpected arguments: %v\n", args)
			return fmt.Errorf("unexpected arguments: %v (command 'install' does not accept positional arguments)", args)
		}

		// Verify we're actually running the install command, not something else
		if cmd.Use != "install" {
			fmt.Fprintf(os.Stderr, "ERROR: expected 'install' command but got '%s'\n", cmd.Use)
			return fmt.Errorf("internal error: expected 'install' command but got '%s'", cmd.Use)
		}

		logger, err := createLogger()
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: failed to create logger: %v\n", err)
			return fmt.Errorf("failed to create logger: %w", err)
		}

		logger.Info("Executing install command", zap.String("command", cmd.Use), zap.Strings("args", args))

		lockFile, _ := cmd.Flags().GetString("lock-file")
		folderPath := "."

		// Check if lock file exists
		lockPath, err := deps.FindLockFile(folderPath, lockFile)
		if err != nil {
			logger.Info("Lock file not found, falling back to update behavior")
			return runUpdate(cmd, args, logger)
		}

		logger.Debug("Found lock file", zap.String("path", lockPath))

		// Load lock file
		lockFileObj, err := deps.LoadLockFile(lockPath)
		if err != nil {
			logger.Error("failed to load lock file", zap.Error(err))
			os.Exit(1)
		}

		logger.Info("Installing dependencies from lock file")
		logger.Debug("Lock file contents",
			zap.String("src_dir", lockFileObj.Directories.Src),
			zap.String("modules_dir", lockFileObj.Directories.Modules),
			zap.Int("modules_count", len(lockFileObj.Modules)),
			zap.Int("replacements_count", len(lockFileObj.Replacements)))

		// Validate replacements before installation
		if err := lockFileObj.ValidateReplacements(lockPath); err != nil {
			logger.Error("invalid replacement paths", zap.Error(err))
			os.Exit(1)
		}

		// Create dependency manager
		depsManager := app.NewDependencyManager(folderPath, lockFile, logger)

		// Scan current state of installed packages BEFORE installation
		logger.Debug("Scanning currently installed packages")
		oldState := depsManager.ScanInstalledPackages(lockFileObj)

		// Install dependencies
		if err := depsManager.InstallDependenciesFromLockFile(cmd.Context(), lockFileObj, lockPath); err != nil {
			logger.Error("failed to install dependencies", zap.Error(err))
			os.Exit(1)
		}

		// Clean up garbage directories BEFORE scanning new state to get accurate stats
		removedGarbage, err := depsManager.CleanupGarbageDirectories(lockFileObj)
		if err != nil {
			logger.Warn("Failed to cleanup garbage directories", zap.Error(err))
		}

		// Scan new state of installed packages AFTER cleanup
		logger.Debug("Scanning newly installed packages")
		newState := depsManager.ScanInstalledPackages(lockFileObj)

		// Compare the two states to determine what changed
		stats := app.ComparePackageStates(oldState, newState)

		// Build a set of modules that were already accounted for (updated or installed)
		accountedModules := make(map[string]bool)
		for _, op := range stats.Operations {
			if op.Action == deps.ActionUpdated || op.Action == deps.ActionInstalled {
				accountedModules[op.Name] = true
			}
		}

		// Add removed garbage directories to stats (only if not already updated/installed)
		if len(removedGarbage) > 0 {
			for _, dir := range removedGarbage {
				// Only process module directories, skip organization directories
				// Path format: /home/user/project/.wippy/vendor/org/module@hash
				// We need to check if path ends with @hash
				base := filepath.Base(dir)
				if strings.Contains(base, "@") && !strings.HasSuffix(filepath.Dir(dir), "vendor") {
					// Get the last two parts of path: org and module@hash
					// dir is full path like: /path/to/.wippy/vendor/wippy/actor@019a01d3-...
					// Split into parts
					parts := strings.Split(dir, string(filepath.Separator))
					// Find vendor in path and get org, module parts
					for i, part := range parts {
						if part == "vendor" && i+2 < len(parts) {
							org := parts[i+1]
							moduleDirName := parts[i+2]
							moduleName := strings.Split(moduleDirName, "@")[0]
							fullModuleName := org + "/" + moduleName
							if org != "" && moduleName != "" && org != "vendor" && !strings.HasSuffix(org, "vendor") {
								// Only add if this module wasn't already updated or installed
								if !accountedModules[fullModuleName] {
									stats.AddRemoved(fullModuleName, "unknown")
								}
							}
							break
						}
					}
				}
			}
		}

		// Display the results
		if stats.HasOperations() {
			logger.Info(fmt.Sprintf("Package operations: %d installed, %d updated, %d removed",
				stats.Installed, stats.Updated, stats.Removed))

			for _, op := range stats.Operations {
				switch op.Action {
				case deps.ActionInstalled:
					logger.Info(fmt.Sprintf(" - Installing %s: %s", op.Name, op.Version))
				case deps.ActionUpdated:
					logger.Info(fmt.Sprintf(" - Updating %s: %s → %s", op.Name, op.OldVersion, op.Version))
				case deps.ActionRemoved:
					logger.Info(fmt.Sprintf(" - Removing %s: %s", op.Name, op.Version))
				}
			}
		} else {
			logger.Info("All dependencies are up to date")
		}

		logger.Info("Dependencies installed successfully")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(installCmd)

	installCmd.Flags().StringP("lock-file", "l", "wippy.lock", "path to lock file")
}
