package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/ponyruntime/pony/moduleloader"
	"go.uber.org/zap"
)

// CLICommand represents a command that can be executed
type CLICommand interface {
	Execute(ctx context.Context, flags []string, args []string) error
	Help() string
}

// CLIRunner handles CLI command structure
type CLIRunner struct {
	config *Config
	logger *zap.Logger
	cmds   map[string]CLICommand
}

// NewCLIRunner creates a new CLI runner
func NewCLIRunner(config *Config, logger *zap.Logger) *CLIRunner {
	runner := &CLIRunner{
		config: config,
		logger: logger,
		cmds:   make(map[string]CLICommand),
	}

	// Register commands
	runner.cmds["init"] = &InitCommand{runner: runner}
	runner.cmds["install"] = &InstallCommand{runner: runner}
	runner.cmds["update"] = &UpdateCommand{runner: runner}
	runner.cmds["run"] = &RunCommand{runner: runner}
	runner.cmds["replace"] = &ReplaceCommand{runner: runner}

	return runner
}

// Run executes the CLI with the new command format
func (cr *CLIRunner) Run(ctx context.Context) error {
	// Check if we have any arguments
	if len(os.Args) < 2 {
		return cr.showHelp()
	}

	// Parse the command
	command := os.Args[1]

	// Check if it's a help command
	if command == "help" || command == "--help" || command == "-h" {
		return cr.showHelp()
	}

	// Check if the command exists
	cmd, exists := cr.cmds[command]
	if !exists {
		return fmt.Errorf("unknown command: %s. Use 'help' to see available commands", command)
	}

	// Update config with current directory
	cr.config.FolderPath = "."

	// Extract flags and arguments from the remaining arguments
	// os.Args[2:] contains everything after the command
	remainingArgs := os.Args[2:]

	// Find the first non-flag argument (subcommand)
	var flags, args []string
	var foundSubcommand bool

	for _, arg := range remainingArgs {
		switch {
		case strings.HasPrefix(arg, "-"):
			// This is a flag, add it to flags
			flags = append(flags, arg)
		case !foundSubcommand:
			// This is the first subcommand
			foundSubcommand = true
			args = append(args, arg)
		default:
			// This is an additional argument after the subcommand
			args = append(args, arg)
		}
	}

	// Execute the command with flags and arguments
	return cmd.Execute(ctx, flags, args)
}

// showHelp displays help information
func (cr *CLIRunner) showHelp() error {
	fmt.Println("Wippy - Dependency management and execution tool")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  wippy <command> [options] [--] <arguments>")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  init     - Initialize a new lock file")
	fmt.Println("  install  - Install dependencies from lock file")
	fmt.Println("  update   - Update dependencies and regenerate lock file")
	fmt.Println("  run      - Run Wippy using paths from lock file")
	fmt.Println("  replace  - Manage module replacements")
	fmt.Println("  help     - Show this help message")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  --lock-file <path>  - Path to lock file (default: wippy.lock)")
	fmt.Println("  -v, --verbose       - Enable verbose debug logging")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  wippy init --lock-file=\"./wippy.lock\" --src-dir=\".\" --modules-dir=\".wippy\" --")
	fmt.Println("  wippy install --lock-file=\"./wippy.lock\" --")
	fmt.Println("  wippy update --lock-file=\"./wippy.lock\" --")
	fmt.Println("  wippy run --lock-file=\"./wippy.lock\" --")
	fmt.Println("  wippy update -v --lock-file=\"./wippy.lock\" --")
	fmt.Println()
	fmt.Println("Use 'wippy <command> --help' for command-specific help")
	return nil
}

// InitCommand handles the init command
type InitCommand struct {
	runner *CLIRunner
}

func (ic *InitCommand) Execute(_ context.Context, flags []string, _ []string) error {
	// Parse init-specific flags
	flagSet := flag.NewFlagSet("init", flag.ExitOnError)
	var srcDir, modulesDir, lockFilePath string
	var verbose bool
	flagSet.StringVar(&srcDir, "src-dir", ".", "source directory path")
	flagSet.StringVar(&modulesDir, "modules-dir", ".wippy", "modules directory path")
	flagSet.StringVar(&lockFilePath, "lock-file", "wippy.lock", "path to lock file")
	flagSet.BoolVar(&verbose, "v", false, "enable verbose debug logging")
	flagSet.BoolVar(&verbose, "verbose", false, "enable verbose debug logging")

	// Parse flags (before --)
	if err := flagSet.Parse(flags); err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	// Update logger if verbose flag is set
	if verbose {
		ic.runner.config.Verbose = true
		// Reinitialize logger with verbose settings
		logger, err := initMainLogger(true, false)
		if err != nil {
			return fmt.Errorf("failed to initialize verbose logger: %w", err)
		}
		ic.runner.logger = logger
	}

	// Update config with parsed lock file
	ic.runner.config.LockFile = lockFilePath

	// Check if lock file already exists
	lockPath := ic.runner.config.LockFile

	if _, err := os.Stat(lockPath); err == nil {
		return fmt.Errorf("lock file already exists: %s", lockPath)
	}

	// Create empty lock file with directories
	lockFile := &moduleloader.LockFile{
		Directories: moduleloader.Directories{
			Modules: modulesDir,
			Src:     srcDir,
		},
		Modules: []moduleloader.LockedModule{},
	}

	// Save the lock file
	if err := lockFile.SaveLockFile(lockPath); err != nil {
		return fmt.Errorf("failed to create lock file: %w", err)
	}

	ic.runner.logger.Info("Lock file initialized successfully",
		zap.String("path", lockPath),
		zap.String("src_dir", srcDir),
		zap.String("modules_dir", modulesDir))

	return nil
}

func (ic *InitCommand) Help() string {
	return `init - Initialize a new lock file

Usage: wippy init --lock-file=<path> --src-dir=<path> --modules-dir=<path> --

Options:
  --lock-file <path>     Path to lock file (default: wippy.lock)
  --src-dir <path>       Source directory path (default: .)
  --modules-dir <path>   Modules directory path (default: .wippy)

Creates a new lock file with the specified directory structure.
Paths in the lock file are relative to the lock file's location.`
}

// InstallCommand handles the install command
type InstallCommand struct {
	runner *CLIRunner
}

func (ic *InstallCommand) Execute(ctx context.Context, flags []string, args []string) error {
	baseCmd := NewBaseCommand(ic.runner)
	return baseCmd.ExecuteWithCommonFlags(ctx, "install", flags, args, ic.executeInstall)
}

func (ic *InstallCommand) executeInstall(ctx context.Context, commonFlags *CommonFlags, args []string) error {
	// Check if lock file exists and load it
	lockPath, err := moduleloader.FindLockFile(ic.runner.config.FolderPath, ic.runner.config.LockFile)
	if err != nil {
		ic.runner.logger.Info("Lock file not found, falling back to update behavior")
		updateCmd := &UpdateCommand{runner: ic.runner}
		return updateCmd.Execute(ctx, []string{"--lock-file=" + commonFlags.LockFilePath}, args)
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

	// Install dependencies
	depsManager := NewDependencyManager(ic.runner.config, ic.runner.logger)
	return depsManager.InstallDependencies(ctx)
}

func (ic *InstallCommand) Help() string {
	return `install - Install dependencies from lock file

Usage: wippy install --lock-file=<path> --

Options:
  --lock-file <path>  Path to lock file (default: wippy.lock)

Reads dependencies from the lock file and installs them.
If the lock file is missing, behaves like update.
Before installation, cleans .wippy from packages not present in the lock file.`
}

// UpdateCommand handles the update command
type UpdateCommand struct {
	runner *CLIRunner
}

func (ic *UpdateCommand) Execute(ctx context.Context, flags []string, args []string) error {
	baseCmd := NewBaseCommand(ic.runner)
	return baseCmd.ExecuteWithCommonFlags(ctx, "update", flags, args, ic.executeUpdate)
}

func (ic *UpdateCommand) executeUpdate(ctx context.Context, commonFlags *CommonFlags, args []string) error {
	// Check if lock file exists, init if missing
	flags := []string{"--lock-file=" + commonFlags.LockFilePath}
	if commonFlags.Verbose {
		flags = append(flags, "-v")
	}
	if err := InitLockFileIfMissing(ctx, ic.runner, flags, args); err != nil {
		return err
	}

	// Update dependencies
	depsManager := NewDependencyManager(ic.runner.config, ic.runner.logger)
	stats := NewModuleOperationStats(commonFlags.Verbose)
	if err := depsManager.UpdateDependenciesWithRemovedModules(ctx, stats); err != nil {
		return fmt.Errorf("failed to update dependencies: %w", err)
	}

	return nil
}

func (ic *UpdateCommand) Help() string {
	return `update - Update dependencies and regenerate lock file

Usage: wippy update --lock-file=<path> --

Options:
  --lock-file <path>  Path to lock file (default: wippy.lock)

If the lock file is missing, runs init.
Resolves dependencies and calculates a diff.
Writes a new lock file and runs install afterwards.`
}

// RunCommand handles the run command
type RunCommand struct {
	runner *CLIRunner
}

func (ic *RunCommand) Execute(_ context.Context, flags []string, args []string) error {
	// Parse common flags
	flagSet := flag.NewFlagSet("run", flag.ExitOnError)
	var lockFilePath string
	var verbose bool
	flagSet.StringVar(&lockFilePath, "lock-file", "wippy.lock", "path to lock file")
	flagSet.BoolVar(&verbose, "v", false, "enable verbose debug logging")
	flagSet.BoolVar(&verbose, "verbose", false, "enable verbose debug logging")
	if err := flagSet.Parse(flags); err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	// Update logger if verbose flag is set
	if verbose {
		ic.runner.config.Verbose = true
		// Reinitialize logger with verbose settings
		logger, err := initMainLogger(true, false)
		if err != nil {
			return fmt.Errorf("failed to initialize verbose logger: %w", err)
		}
		ic.runner.logger = logger
	}

	// Update config with parsed lock file
	if lockFilePath != "wippy.lock" {
		ic.runner.config.LockFile = lockFilePath
	}

	// Check if lock file exists
	lockPath, err := moduleloader.FindLockFile(ic.runner.config.FolderPath, ic.runner.config.LockFile)
	if err != nil {
		return fmt.Errorf("lock file not found: %s", ic.runner.config.LockFile)
	}

	// Load lock file
	lockFile, err := moduleloader.LoadLockFile(lockPath)
	if err != nil {
		return fmt.Errorf("failed to load lock file: %w", err)
	}

	// If no additional arguments, start Wippy application (like previous start behavior)
	if len(args) == 0 {
		ic.runner.logger.Info("Starting Wippy application with lock file paths",
			zap.String("src_dir", lockFile.Directories.Src),
			zap.String("modules_dir", lockFile.Directories.Modules))

		// Resolve the full lock file path first, then get its directory
		// The lock file path could be relative to current directory or absolute
		fullLockPath := lockPath
		if !filepath.IsAbs(fullLockPath) {
			// If relative, resolve it relative to current working directory
			fullLockPath = filepath.Join(".", fullLockPath)
			fullLockPath, err = filepath.Abs(fullLockPath)
			if err != nil {
				return fmt.Errorf("failed to resolve absolute lock file path: %w", err)
			}
		}
		lockDir := filepath.Dir(fullLockPath)

		// Resolve paths relative to lock file location
		// The directories in the lock file are relative to where the lock file is located
		appDir := filepath.Join(lockDir, lockFile.Directories.Src)
		modulesDir := filepath.Join(lockDir, lockFile.Directories.Modules)

		// Ensure paths end with trailing slash if they're directories
		if !strings.HasSuffix(appDir, string(os.PathSeparator)) {
			appDir += string(os.PathSeparator)
		}
		if !strings.HasSuffix(modulesDir, string(os.PathSeparator)) {
			modulesDir += string(os.PathSeparator)
		}

		// Update config with the resolved source directory
		ic.runner.config.FolderPath = appDir

		// Log the resolved paths for debugging
		ic.runner.logger.Info("Resolved paths from lock file",
			zap.String("lock_file_dir", lockDir),
			zap.String("source_dir", appDir),
			zap.String("modules_dir", modulesDir))

		// Debug: check current working directory
		currentDir, err := os.Getwd()
		if err != nil {
			ic.runner.logger.Error("failed to get current working directory", zap.Error(err))
		} else {
			ic.runner.logger.Info("Current working directory", zap.String("cwd", currentDir))
		}

		// Don't change working directory - use absolute paths instead
		// This prevents the application from looking for the lock file in the wrong location

		// Resolve all paths to absolute paths
		absLockPath, err := filepath.Abs(fullLockPath)
		if err != nil {
			return fmt.Errorf("failed to resolve absolute lock file path: %w", err)
		}

		absModulesDir, err := filepath.Abs(modulesDir)
		if err != nil {
			return fmt.Errorf("failed to resolve absolute modules directory path: %w", err)
		}

		// Create Modules Directory if it does not exist
		if _, err := os.Stat(absModulesDir); os.IsNotExist(err) {
			if err := os.MkdirAll(absModulesDir, 0o755); err != nil {
				return fmt.Errorf("failed to create modules directory: %w", err)
			}
			ic.runner.logger.Info("Created modules directory", zap.String("modules_dir", absModulesDir))
		} else if err != nil {
			return fmt.Errorf("failed to stat modules directory: %w", err)
		}

		absLockDir, err := filepath.Abs(lockDir)
		if err != nil {
			return fmt.Errorf("failed to resolve absolute lock file directory path: %w", err)
		}

		// Create and start the application with absolute paths
		app, err := NewApp(ic.runner.config, ic.runner.logger, absLockPath, absModulesDir, absLockDir)
		if err != nil {
			return fmt.Errorf("failed to create application: %w", err)
		}

		// Initialize the application
		if err := app.Initialize(); err != nil {
			return fmt.Errorf("failed to initialize application: %w", err)
		}

		// Configure services
		app.services = createServiceHandlers(app)
		runtime.GC()

		// Start the application
		if err := app.Start(ic.runner.config.FolderPath, ic.runner.config.UseEmbed); err != nil {
			return fmt.Errorf("failed to start application: %w", err)
		}

		ic.runner.logger.Info("Wippy application started successfully")

		// Handle shutdown signals
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		// Wait for first shutdown signal
		sig := <-sigChan
		ic.runner.logger.Info("received shutdown signal, starting graceful shutdown", zap.String("signal", sig.String()))

		// Handle second signal for force shutdown
		go func() {
			sig := <-sigChan
			ic.runner.logger.Warn("received second shutdown signal, forcing immediate shutdown", zap.String("signal", sig.String()))
			close(app.forceShutdown)
		}()

		// Graceful shutdown
		if err := app.Stop(); err != nil {
			ic.runner.logger.Error("error during shutdown", zap.Error(err))
			return fmt.Errorf("error during shutdown: %w", err)
		}

		if app.shuttingDown {
			ic.runner.logger.Info("graceful shutdown completed")
		} else {
			ic.runner.logger.Info("force shutdown completed")
		}

		return nil
	}

	ic.runner.logger.Info("Running command with lock file paths",
		zap.String("src_dir", lockFile.Directories.Src),
		zap.String("modules_dir", lockFile.Directories.Modules))

	return nil
}

func (ic *RunCommand) Help() string {
	return `run - Run Wippy using paths from lock file

Usage: wippy run --lock-file=<path> -- [command]

Options:
  --lock-file <path>  Path to lock file (default: wippy.lock)

Runs Wippy using paths from the lock file.
When executed without a command, outputs a list of available commands.`
}

// ReplaceCommand handles the replace command for managing module replacements
type ReplaceCommand struct {
	runner *CLIRunner
}

func (ic *ReplaceCommand) Execute(_ context.Context, flags []string, args []string) error {
	// Parse common flags
	flagSet := flag.NewFlagSet("replace", flag.ExitOnError)
	var lockFilePath string
	flagSet.StringVar(&lockFilePath, "lock-file", "wippy.lock", "path to lock file")

	// Parse flags first
	if err := flagSet.Parse(flags); err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	// Update config with parsed lock file
	if lockFilePath != "wippy.lock" {
		ic.runner.config.LockFile = lockFilePath
	}

	// Debug logging
	ic.runner.logger.Info("ReplaceCommand Execute",
		zap.String("lockFilePath", lockFilePath),
		zap.String("configLockFile", ic.runner.config.LockFile),
		zap.String("folderPath", ic.runner.config.FolderPath),
		zap.Strings("parsedFlags", flags))

	// Check if lock file exists
	lockPath, err := moduleloader.FindLockFile(ic.runner.config.FolderPath, ic.runner.config.LockFile)
	if err != nil {
		return fmt.Errorf("lock file not found: %s", ic.runner.config.LockFile)
	}

	ic.runner.logger.Info("Found lock file", zap.String("lockPath", lockPath))

	// Load lock file
	lockFile, err := moduleloader.LoadLockFile(lockPath)
	if err != nil {
		return fmt.Errorf("failed to load lock file: %w", err)
	}

	ic.runner.logger.Info("Loaded lock file",
		zap.Int("modulesCount", len(lockFile.Modules)),
		zap.Int("replacementsCount", len(lockFile.Replacements)))

	// Parse replacement arguments
	if len(args) < 2 {
		return ic.showReplacements(lockFile)
	}

	// Parse the replacement command
	switch {
	case len(args) >= 3 && args[0] == "add":
		// Add replacement: replace add <module> <path>
		moduleName := args[1]
		customPath := args[2]

		// Validate that the module exists in the lock file
		moduleExists := false
		for _, module := range lockFile.Modules {
			if module.Name == moduleName {
				moduleExists = true
				break
			}
		}

		if !moduleExists {
			return fmt.Errorf("module %s not found in lock file", moduleName)
		}

		// Check if replacement already exists
		for _, replacement := range lockFile.Replacements {
			if replacement.From == moduleName {
				return fmt.Errorf("replacement for module %s already exists", moduleName)
			}
		}

		// Add the replacement
		lockFile.Replacements = append(lockFile.Replacements, moduleloader.Replacement{
			From: moduleName,
			To:   customPath,
		})

		// Validate the replacement path
		if err := lockFile.ValidateReplacements(lockPath); err != nil {
			return fmt.Errorf("invalid replacement path: %w", err)
		}

		// Save the updated lock file
		if err := lockFile.SaveLockFile(lockPath); err != nil {
			return fmt.Errorf("failed to save lock file: %w", err)
		}

		ic.runner.logger.Info("Replacement added successfully",
			zap.String("module", moduleName),
			zap.String("path", customPath))
	case len(args) >= 2 && args[0] == "remove":
		// Remove replacement: replace remove <module>
		moduleName := args[1]

		// Find and remove the replacement
		found := false
		for i, replacement := range lockFile.Replacements {
			if replacement.From == moduleName {
				lockFile.Replacements = append(lockFile.Replacements[:i], lockFile.Replacements[i+1:]...)
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf("no replacement found for module %s", moduleName)
		}

		// Save the updated lock file
		if err := lockFile.SaveLockFile(lockPath); err != nil {
			return fmt.Errorf("failed to save lock file: %w", err)
		}

		ic.runner.logger.Info("Replacement removed successfully",
			zap.String("module", moduleName))
	case len(args) >= 1 && args[0] == "list":
		// List replacements
		return ic.showReplacements(lockFile)
	default:
		return fmt.Errorf("invalid replace command. Use 'replace add <module> <path>', 'replace remove <module>', or 'replace list'")
	}

	return nil
}

func (ic *ReplaceCommand) showReplacements(lockFile *moduleloader.LockFile) error {
	if len(lockFile.Replacements) == 0 {
		fmt.Println("No module replacements configured.")
		return nil
	}

	fmt.Println("Module replacements:")
	for _, replacement := range lockFile.Replacements {
		fmt.Printf("  %s -> %s\n", replacement.From, replacement.To)
	}
	return nil
}

func (ic *ReplaceCommand) Help() string {
	return `replace - Manage module replacements

Usage: wippy replace --lock-file=<path> -- <command> [args]

Commands:
  add <module> <path>    - Add a replacement for a module
  remove <module>        - Remove a replacement for a module
  list                   - List all replacements

Options:
  --lock-file <path>     Path to lock file (default: wippy.lock)

Examples:
  wippy replace --lock-file="./wippy.lock" -- add wippy/llm ./local/llm
  wippy replace --lock-file="./wippy.lock" -- remove wippy/llm
  wippy replace --lock-file="./wippy.lock" -- list

Module replacements allow you to use custom paths instead of downloading
modules from the registry, similar to Go's replace directive.`
}
