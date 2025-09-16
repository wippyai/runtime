package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/ponyruntime/pony/moduleloader"
)

// CommonFlags represents common flags used across CLI commands
type CommonFlags struct {
	LockFilePath string
	Verbose      bool
}

// ParseCommonFlags parses common flags from command line arguments
func ParseCommonFlags(commandName string, flags []string) (*CommonFlags, error) {
	flagSet := flag.NewFlagSet(commandName, flag.ExitOnError)
	var lockFilePath string
	var verbose bool

	flagSet.StringVar(&lockFilePath, "lock-file", "wippy.lock", "path to lock file")
	flagSet.BoolVar(&verbose, "v", false, "enable verbose debug logging")
	flagSet.BoolVar(&verbose, "verbose", false, "enable verbose debug logging")

	if err := flagSet.Parse(flags); err != nil {
		return nil, fmt.Errorf("failed to parse flags: %w", err)
	}

	return &CommonFlags{
		LockFilePath: lockFilePath,
		Verbose:      verbose,
	}, nil
}

// UpdateLoggerIfVerbose updates the logger if verbose flag is set
func UpdateLoggerIfVerbose(runner *CLIRunner, verbose bool) error {
	if verbose {
		runner.config.Verbose = true
		logger, err := initMainLogger(true, false)
		if err != nil {
			return fmt.Errorf("failed to initialize verbose logger: %w", err)
		}
		runner.logger = logger
	}
	return nil
}

// UpdateConfigWithLockFile updates the config with the parsed lock file path
func UpdateConfigWithLockFile(runner *CLIRunner, lockFilePath string) {
	if lockFilePath != "wippy.lock" {
		runner.config.LockFile = lockFilePath
	}
}

// LoadLockFileOrFallback loads a lock file or falls back to update behavior
func LoadLockFileOrFallback(ctx context.Context, runner *CLIRunner, flags []string, args []string) error {
	lockPath, err := moduleloader.FindLockFile(runner.config.FolderPath, runner.config.LockFile)
	if err != nil {
		runner.logger.Info("Lock file not found, falling back to update behavior")
		updateCmd := &UpdateCommand{runner: runner}
		return updateCmd.Execute(ctx, flags, args)
	}

	// Load lock file
	lockFile, err := moduleloader.LoadLockFile(lockPath)
	if err != nil {
		return fmt.Errorf("failed to load lock file: %w", err)
	}

	// Validate replacements before installation
	if err := lockFile.ValidateReplacements(lockPath); err != nil {
		return fmt.Errorf("invalid replacement paths: %w", err)
	}

	return nil
}

// InitLockFileIfMissing initializes a lock file if it doesn't exist
func InitLockFileIfMissing(ctx context.Context, runner *CLIRunner, flags []string, args []string) error {
	_, err := moduleloader.FindLockFile(runner.config.FolderPath, runner.config.LockFile)
	if err != nil {
		runner.logger.Info("Lock file not found, running init")
		initCmd := &InitCommand{runner: runner}
		if err := initCmd.Execute(ctx, flags, args); err != nil {
			return fmt.Errorf("failed to initialize lock file: %w", err)
		}
	}
	return nil
}

// BaseCommand provides common functionality for CLI commands
type BaseCommand struct {
	runner *CLIRunner
}

// NewBaseCommand creates a new base command
func NewBaseCommand(runner *CLIRunner) *BaseCommand {
	return &BaseCommand{runner: runner}
}

// ExecuteWithCommonFlags executes a command with common flag parsing
func (bc *BaseCommand) ExecuteWithCommonFlags(
	ctx context.Context,
	commandName string,
	flags []string,
	args []string,
	executeFunc func(ctx context.Context, commonFlags *CommonFlags, args []string) error,
) error {
	// Parse common flags
	commonFlags, err := ParseCommonFlags(commandName, flags)
	if err != nil {
		return err
	}

	// Update logger if verbose
	if err := UpdateLoggerIfVerbose(bc.runner, commonFlags.Verbose); err != nil {
		return err
	}

	// Update config with lock file
	UpdateConfigWithLockFile(bc.runner, commonFlags.LockFilePath)

	// Execute the specific command logic
	return executeFunc(ctx, commonFlags, args)
}
