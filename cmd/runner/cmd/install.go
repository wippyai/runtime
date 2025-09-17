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
	Use:   "install",
	Short: "Install dependencies from lock file",
	Long:  "Reads dependencies from the lock file and installs them. If the lock file is missing, behaves like update.",
	RunE: func(cmd *cobra.Command, args []string) error {
		logger, err := createLogger()
		if err != nil {
			logger.Error("failed to create logger", zap.Error(err))
			os.Exit(1)
		}

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

		// Clean unused packages from modules directory
		if err := cleanUnusedPackages(lockFileObj, folderPath, logger); err != nil {
			logger.Error("failed to clean unused packages", zap.Error(err))
			os.Exit(1)
		}

		// Display package operations from lock file
		if len(lockFileObj.Modules) > 0 {
			logger.Info(fmt.Sprintf("Package operations: %d installs, 0 updates, 0 removals:", len(lockFileObj.Modules)))
			for _, module := range lockFileObj.Modules {
				logger.Info(fmt.Sprintf("- %s: %s", module.Name, module.Version))
			}
		} else {
			logger.Info("No modules to install")
		}

		// Install dependencies
		depsManager := app.NewDependencyManager(folderPath, lockFile, logger)
		if err := depsManager.InstallDependencies(cmd.Context()); err != nil {
			logger.Error("failed to install dependencies", zap.Error(err))
			os.Exit(1)
		}

		logger.Info("Dependencies installed successfully")
		return nil
	},
}

func cleanUnusedPackages(lockFile *deps.LockFile, folderPath string, logger *zap.Logger) error {
	logger.Info("Cleaning unused packages from modules directory")

	// Get the modules directory path
	modulesDir := filepath.Join(folderPath, lockFile.Directories.Modules)

	// Check if modules directory exists
	if _, err := os.Stat(modulesDir); os.IsNotExist(err) {
		logger.Info("Modules directory does not exist, nothing to clean")
		return nil
	}

	// Create a set of expected organizations from lock file
	// Module names are like "wippy/llm", so we need to extract organization names
	expectedOrgs := make(map[string]bool)
	for _, module := range lockFile.Modules {
		// Parse module name to get organization
		if name, err := deps.ParseName(module.Name); err == nil {
			expectedOrgs[name.Organization] = true
			logger.Debug("Expected organization from module",
				zap.String("module", module.Name),
				zap.String("organization", name.Organization))
		} else {
			// Fallback: split by '/' and take first part
			parts := strings.Split(module.Name, "/")
			if len(parts) > 0 {
				expectedOrgs[parts[0]] = true
				logger.Debug("Expected organization from fallback parsing",
					zap.String("module", module.Name),
					zap.String("organization", parts[0]))
			}
		}
	}

	logger.Debug("Expected organizations", zap.Any("organizations", expectedOrgs))

	// Scan the modules directory for installed packages
	entries, err := os.ReadDir(modulesDir)
	if err != nil {
		return fmt.Errorf("failed to read modules directory: %w", err)
	}

	cleanedCount := 0
	// Remove organization directories that are not in the lock file
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Check if this organization is expected
		if !expectedOrgs[entry.Name()] {
			packagePath := filepath.Join(modulesDir, entry.Name())
			logger.Info("Removing unused organization directory", zap.String("org", entry.Name()))
			cleanedCount++

			if err := os.RemoveAll(packagePath); err != nil {
				logger.Warn("Failed to remove unused organization directory",
					zap.String("org", entry.Name()),
					zap.Error(err))
			} else {
				logger.Debug("Successfully removed organization directory",
					zap.String("org", entry.Name()),
					zap.String("path", packagePath))
			}
		} else {
			logger.Debug("Keeping expected organization directory", zap.String("org", entry.Name()))
		}
	}

	if cleanedCount > 0 {
		logger.Info("Cleanup completed", zap.Int("removed_directories", cleanedCount))
	} else {
		logger.Info("No unused packages found to clean")
	}

	return nil
}

func init() {
	rootCmd.AddCommand(installCmd)

	installCmd.Flags().StringP("lock-file", "l", "wippy.lock", "path to lock file")
}
