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
	Long:  fmt.Sprintf("Initialize a new lock file with default directory structure (src: '%s', modules: '%s').", deps.DefaultSrcDir, deps.DefaultModulesDir),
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger, err := createLogger()
		if err != nil {
			return fmt.Errorf("failed to create logger: %w", err)
		}

		lockFile, _ := cmd.Flags().GetString("lock-file")

		// Check if lock file already exists
		if _, err := os.Stat(lockFile); err == nil {
			return fmt.Errorf("lock file already exists: %s", lockFile)
		}

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
			return fmt.Errorf("failed to create lock file: %w", err)
		}

		logger.Info("Lock file initialized successfully",
			zap.String("path", lockFile),
			zap.String("src_dir", deps.DefaultSrcDir),
			zap.String("modules_dir", deps.DefaultModulesDir))

		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)

	initCmd.Flags().StringP("lock-file", "l", "wippy.lock", "path to lock file")
}
