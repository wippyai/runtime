// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	depconfig "github.com/wippyai/runtime/boot/deps/config"
	"github.com/wippyai/runtime/boot/deps/lock"
	clilogger "github.com/wippyai/runtime/cmd/internal/logger"
	"go.uber.org/zap"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new lock file",
	Long: `Initialize a new wippy.lock file with default configuration

Creates a new lock file with default directory structure:
  - src: ./src (application source directory)
  - modules: .wippy (modules installation directory)

Directory precedence, highest first: explicit --src-dir and --modules-dir flags,
existing lock file, directories in wippy.yaml in the working directory, defaults.

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
	dirs, err := lockCommandDirectories(cmd, lockFile)
	if err != nil {
		return err
	}

	logger.Info("initializing lock file",
		zap.String("path", lockFile),
		zap.String("src", dirs.Src),
		zap.String("modules", dirs.Modules))

	lockObj, err := lock.New(lockFile)
	if err != nil {
		return NewLoadLockFileError(fmt.Errorf("lock file %s: %w", lockFile, err))
	}

	lockObj.SetDirectories(dirs)

	if err := lockObj.Write(); err != nil {
		return NewWriteLockFileError(fmt.Errorf("lock file %s: %w", lockObj.Path(), err))
	}
	logger.Info("lock file initialized successfully")
	return nil
}

func lockCommandDirectories(cmd *cobra.Command, lockFile string) (lock.Directories, error) {
	dirs := lock.Directories{Modules: ".wippy", Src: "./src"}
	var configured lock.Directories
	if _, err := os.Stat(lockFile); err == nil {
		locked, err := lock.New(lockFile)
		if err != nil {
			return dirs, NewLoadLockFileError(err)
		}
		configured = locked.GetDirectories()
	} else if !os.IsNotExist(err) {
		return dirs, NewLoadLockFileError(err)
	} else if _, err := os.Stat(depconfig.DefaultConfigFile); err == nil {
		manifest, err := depconfig.Load(".")
		if err != nil {
			return dirs, err
		}
		configured = manifest.Directories
	} else if !os.IsNotExist(err) {
		return dirs, fmt.Errorf("read %s: %w", depconfig.DefaultConfigFile, err)
	}
	if configured.Src != "" {
		dirs.Src = configured.Src
	}
	if configured.Modules != "" {
		dirs.Modules = configured.Modules
	}
	if cmd.Flags().Changed("src-dir") {
		dirs.Src, _ = cmd.Flags().GetString("src-dir")
	}
	if cmd.Flags().Changed("modules-dir") {
		dirs.Modules, _ = cmd.Flags().GetString("modules-dir")
	}
	return dirs, nil
}
