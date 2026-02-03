package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wippyai/runtime/boot/deps/lock"
	clilogger "github.com/wippyai/runtime/cmd/internal/logger"
	"go.uber.org/zap"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new lock file",
	Long: `Initialize a new wippy.lock file with default configuration

Creates a new lock file with default directory structure:
  - src: . (application source directory)
  - modules: .wippy (modules installation directory)

The lock file tracks installed dependencies and their versions.`,
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)

	initCmd.Flags().StringP("src-dir", "d", "./src", "source directory path")
	initCmd.Flags().String("modules-dir", ".wippy", "modules directory path")
	initCmd.Flags().StringP("lock-file", "l", defaultLockFile, "path to lock file")
}

func runInit(cmd *cobra.Command, _ []string) error {
	logger, err := clilogger.CreateLogger(clilogger.Config{
		Verbose:      verbose,
		VeryVerbose:  veryVerbose,
		Console:      console,
		Silent:       silentLogs,
		AppStartTime: appStartTime,
	})
	if err != nil {
		return NewCreateLoggerError(err)
	}
	defer func() {
		_ = logger.Sync() // Ignore sync errors (typically closed stdout/stderr)
	}()

	lockFile, _ := cmd.Flags().GetString("lock-file")
	srcDir, _ := cmd.Flags().GetString("src-dir")
	modulesDir, _ := cmd.Flags().GetString("modules-dir")

	logger.Info("initializing lock file",
		zap.String("path", lockFile),
		zap.String("src", srcDir),
		zap.String("modules", modulesDir))

	lockObj, err := lock.New(lockFile)
	if err != nil {
		return NewLoadLockFileError(fmt.Errorf("lock file %s: %w", lockFile, err))
	}

	lockObj.SetDirectories(lock.Directories{
		Modules: modulesDir,
		Src:     srcDir,
	})

	if err := lockObj.Write(); err != nil {
		return NewWriteLockFileError(err)
	}

	logger.Info("lock file initialized successfully")
	return nil
}
