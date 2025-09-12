package cmd

import (
	"fmt"
	"os"

	"github.com/ponyruntime/pony/deps"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new lock file",
	Long:  "Initialize a new lock file with the specified directory structure.",
	RunE: func(cmd *cobra.Command, args []string) error {
		logger, err := createLogger()
		if err != nil {
			return fmt.Errorf("failed to create logger: %w", err)
		}

		lockFile, _ := cmd.Flags().GetString("lock-file")
		srcDir, _ := cmd.Flags().GetString("src-dir")
		modulesDir, _ := cmd.Flags().GetString("modules-dir")

		// Check if lock file already exists
		if _, err := os.Stat(lockFile); err == nil {
			return fmt.Errorf("lock file already exists: %s", lockFile)
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
			return fmt.Errorf("failed to create lock file: %w", err)
		}

		logger.Info("Lock file initialized successfully",
			zap.String("path", lockFile),
			zap.String("src_dir", srcDir),
			zap.String("modules_dir", modulesDir))

		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)

	initCmd.Flags().StringP("lock-file", "l", "wippy.lock", "path to lock file")
	initCmd.Flags().StringP("src-dir", "s", ".", "source directory path")
	initCmd.Flags().StringP("modules-dir", "m", ".wippy", "modules directory path")
}
