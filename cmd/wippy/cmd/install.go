package cmd

import (
	"fmt"
	"os"

	"github.com/ponyruntime/pony/cmd/runner/app"
	"github.com/ponyruntime/pony/deps"
	"github.com/ponyruntime/pony/deps/lock"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install dependencies from lock file",
	Long: `Install dependencies from wippy.lock file

Downloads and installs all modules specified in the lock file.
If the lock file is missing, runs 'wippy init' followed by 'wippy update'.

Modules are installed to the vendor directory specified in the lock file.`,
	RunE: runInstall,
}

func init() {
	rootCmd.AddCommand(installCmd)

	installCmd.Flags().StringP("lock-file", "l", "wippy.lock", "path to lock file")
}

func runInstall(cmd *cobra.Command, args []string) error {
	logger, err := CreateLogger()
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}
	defer logger.Sync()

	lockFile, _ := cmd.Flags().GetString("lock-file")
	folderPath := "."

	lockPath, err := deps.FindLockFile(folderPath, lockFile)
	if err != nil {
		logger.Info("lock file not found, running init and update")

		if err := runInit(cmd, args); err != nil {
			return fmt.Errorf("init failed: %w", err)
		}

		return runUpdate(cmd, args)
	}

	logger.Info("installing dependencies", zap.String("lock_file", lockPath))

	lockFileObj, err := lock.New(lockPath)
	if err != nil {
		return fmt.Errorf("load lock file: %w", err)
	}

	modules := lockFileObj.GetModules()
	if len(modules) == 0 {
		logger.Info("no modules to install")
		return nil
	}

	logger.Info("modules to install", zap.Int("count", len(modules)))

	oldLockFile, err := deps.LoadLockFile(lockPath)
	if err != nil {
		return fmt.Errorf("load old format lock file: %w", err)
	}

	depsManager := app.NewDependencyManager(folderPath, lockFile, logger)

	oldState := depsManager.ScanInstalledPackages(oldLockFile)

	if err := depsManager.InstallDependenciesFromLockFile(cmd.Context(), oldLockFile, lockPath); err != nil {
		logger.Error("failed to install dependencies", zap.Error(err))
		os.Exit(1)
	}

	removedGarbage, err := depsManager.CleanupGarbageDirectories(oldLockFile)
	if err != nil {
		logger.Warn("failed to cleanup garbage directories", zap.Error(err))
	}

	newState := depsManager.ScanInstalledPackages(oldLockFile)
	stats := app.ComparePackageStates(oldState, newState)

	if len(removedGarbage) > 0 {
		for _, dir := range removedGarbage {
			logger.Debug("removed garbage", zap.String("dir", dir))
		}
	}

	if stats.HasOperations() {
		logger.Info(fmt.Sprintf("package operations: %d installed, %d updated, %d removed",
			stats.Installed, stats.Updated, stats.Removed))

		for _, op := range stats.Operations {
			switch op.Action {
			case deps.ActionInstalled:
				logger.Info(fmt.Sprintf(" - installing %s: %s", op.Name, op.Version))
			case deps.ActionUpdated:
				logger.Info(fmt.Sprintf(" - updating %s: %s → %s", op.Name, op.OldVersion, op.Version))
			case deps.ActionRemoved:
				logger.Info(fmt.Sprintf(" - removing %s: %s", op.Name, op.Version))
			}
		}
	} else {
		logger.Info("all dependencies are up to date")
	}

	logger.Info("installation complete")
	return nil
}
