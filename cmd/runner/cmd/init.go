package cmd

import (
	"fmt"
	"os"

	"github.com/ponyruntime/pony/deps"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var initCmd = &cobra.Command{
	Use:           "init",
	Short:         "Initialize a new lock file",
	Long:          "Initialize a new lock file with the specified directory structure.",
	Args:          cobra.NoArgs,
	SilenceUsage:  true,
	SilenceErrors: false,
	RunE: func(cmd *cobra.Command, args []string) error {
		// CRITICAL: Early check - log to stderr immediately before any logger is created
		fmt.Fprintf(os.Stderr, "DEBUG: init command RunE called (cmd.Use='%s', args=%v)\n", cmd.Use, args)

		// Explicitly check for unexpected arguments on Windows
		if len(args) > 0 {
			fmt.Fprintf(os.Stderr, "ERROR: init command received unexpected arguments: %v\n", args)
			return fmt.Errorf("unexpected arguments: %v (command 'init' does not accept positional arguments)", args)
		}

		// Verify we're actually running the init command, not something else
		if cmd.Use != "init" {
			fmt.Fprintf(os.Stderr, "ERROR: expected 'init' command but got '%s'\n", cmd.Use)
			return fmt.Errorf("internal error: expected 'init' command but got '%s'", cmd.Use)
		}

		logger, err := createLogger()
		if err != nil {
			return fmt.Errorf("failed to create logger: %w", err)
		}

		logger.Info("Executing init command", zap.String("command", cmd.Use), zap.Strings("args", args))

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
	initCmd.Flags().StringP("src-dir", "d", ".", "source directory path")
	initCmd.Flags().StringP("modules-dir", "m", ".wippy", "modules directory path")
}
