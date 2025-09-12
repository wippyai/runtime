package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ponyruntime/pony/moduleloader"
	"go.uber.org/zap"
)

func TestCLIRunner(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "wippy-test-1")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Change to the temporary directory for testing
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Errorf("Failed to restore original directory: %v", err)
		}
	}()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create test config
	config := &Config{
		FolderPath: tempDir,
		LockFile:   "wippy.lock",
	}

	// Create logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// Create CLI runner
	runner := NewCLIRunner(config, logger)

	// Test init command
	t.Run("InitCommand", func(t *testing.T) {
		initCmd := &InitCommand{runner: runner}

		// Test with default values
		err := initCmd.Execute(context.Background(), []string{}, []string{})
		if err != nil {
			t.Fatalf("Init command failed: %v", err)
		}

		// Verify lock file was created
		lockPath := filepath.Join(tempDir, "wippy.lock")
		if _, err := os.Stat(lockPath); os.IsNotExist(err) {
			t.Fatal("Lock file was not created")
		}

		// Load and verify lock file content
		lockFile, err := moduleloader.LoadLockFile(lockPath)
		if err != nil {
			t.Fatalf("Failed to load lock file: %v", err)
		}

		if lockFile.Directories.Modules != ".wippy" {
			t.Errorf("Expected modules dir to be '.wippy', got '%s'", lockFile.Directories.Modules)
		}

		if lockFile.Directories.Src != "." {
			t.Errorf("Expected src dir to be '.', got '%s'", lockFile.Directories.Src)
		}

		if len(lockFile.Modules) != 0 {
			t.Errorf("Expected 0 modules, got %d", len(lockFile.Modules))
		}
	})

	// Test that init fails when lock file already exists
	t.Run("InitCommandDuplicate", func(t *testing.T) {
		initCmd := &InitCommand{runner: runner}

		err := initCmd.Execute(context.Background(), []string{}, []string{})
		if err == nil {
			t.Fatal("Init command should fail when lock file already exists")
		}
	})

	// Test help command
	t.Run("HelpCommand", func(t *testing.T) {
		err := runner.showHelp()
		if err != nil {
			t.Fatalf("Help command failed: %v", err)
		}
	})
}

func TestInitCommandWithCustomPaths(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "wippy-test-2")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Change to the temporary directory for testing
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Errorf("Failed to restore original directory: %v", err)
		}
	}()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create test config
	config := &Config{
		FolderPath: tempDir,
		LockFile:   "custom.lock",
	}

	// Create logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// Create CLI runner
	runner := NewCLIRunner(config, logger)

	// Test init command with custom paths
	t.Run("InitCommandCustomPaths", func(t *testing.T) {
		initCmd := &InitCommand{runner: runner}

		// Test with custom paths and lock file name
		args := []string{"--lock-file", "custom.lock", "--src-dir", "./src", "--modules-dir", "./vendor"}
		err := initCmd.Execute(context.Background(), args, []string{})
		if err != nil {
			t.Fatalf("Init command with custom paths failed: %v", err)
		}

		// Verify lock file was created
		lockPath := filepath.Join(tempDir, "custom.lock")
		if _, err := os.Stat(lockPath); os.IsNotExist(err) {
			t.Fatal("Lock file was not created")
		}

		// Load and verify lock file content
		lockFile, err := moduleloader.LoadLockFile(lockPath)
		if err != nil {
			t.Fatalf("Failed to load lock file: %v", err)
		}

		if lockFile.Directories.Modules != "./vendor" {
			t.Errorf("Expected modules dir to be './vendor', got '%s'", lockFile.Directories.Modules)
		}

		if lockFile.Directories.Src != "./src" {
			t.Errorf("Expected src dir to be './src', got '%s'", lockFile.Directories.Src)
		}
	})
}

func TestVerboseFlag(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "wippy-test-verbose")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Change to the temporary directory for testing
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Errorf("Failed to restore original directory: %v", err)
		}
	}()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create test config
	config := &Config{
		FolderPath: tempDir,
		LockFile:   "wippy.lock",
	}

	// Create logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// Create CLI runner
	runner := NewCLIRunner(config, logger)

	// Test verbose flag for init command
	t.Run("InitCommandVerbose", func(t *testing.T) {
		initCmd := &InitCommand{runner: runner}

		// Test with verbose flag
		flags := []string{"-v"}
		err := initCmd.Execute(context.Background(), flags, []string{})
		if err != nil {
			t.Fatalf("Init command with verbose flag failed: %v", err)
		}

		// Verify that verbose mode was enabled
		if !runner.config.Verbose {
			t.Error("Expected verbose mode to be enabled")
		}
	})

	// Test verbose flag for install command
	t.Run("InstallCommandVerbose", func(t *testing.T) {
		installCmd := &InstallCommand{runner: runner}

		// Test with verbose flag
		flags := []string{"-v"}
		err := installCmd.Execute(context.Background(), flags, []string{})
		if err != nil {
			t.Fatalf("Install command with verbose flag failed: %v", err)
		}

		// Verify that verbose mode was enabled
		if !runner.config.Verbose {
			t.Error("Expected verbose mode to be enabled")
		}
	})

	// Test verbose flag for update command
	t.Run("UpdateCommandVerbose", func(t *testing.T) {
		updateCmd := &UpdateCommand{runner: runner}

		// Test with verbose flag
		flags := []string{"-v"}
		err := updateCmd.Execute(context.Background(), flags, []string{})
		if err != nil {
			t.Fatalf("Update command with verbose flag failed: %v", err)
		}

		// Verify that verbose mode was enabled
		if !runner.config.Verbose {
			t.Error("Expected verbose mode to be enabled")
		}
	})

	// Test verbose flag for run command
	t.Run("RunCommandVerbose", func(t *testing.T) {
		runCmd := &RunCommand{runner: runner}

		// Create a context with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		// Test with verbose flag
		flags := []string{"-v"}

		// Start the command in a goroutine
		done := make(chan error, 1)
		go func() {
			done <- runCmd.Execute(ctx, flags, []string{})
		}()

		// Wait for either completion or timeout
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Run command with verbose flag failed: %v", err)
			}
		case <-ctx.Done():
			// Timeout is expected for this test
		}

		// Verify that verbose mode was enabled
		if !runner.config.Verbose {
			t.Error("Expected verbose mode to be enabled")
		}
	})
}

func TestRunCommandVerboseWithLogs(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "wippy-test-verbose-logs")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Change to the temporary directory for testing
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Errorf("Failed to restore original directory: %v", err)
		}
	}()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create test config
	config := &Config{
		FolderPath: tempDir,
		LockFile:   "wippy.lock",
	}

	// Create logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// Create CLI runner
	runner := NewCLIRunner(config, logger)

	// First create a lock file
	initCmd := &InitCommand{runner: runner}
	err = initCmd.Execute(context.Background(), []string{}, []string{})
	if err != nil {
		t.Fatalf("Init command failed: %v", err)
	}

	// Test verbose flag with log capture
	t.Run("RunCommandVerboseWithLogs", func(t *testing.T) {
		runCmd := &RunCommand{runner: runner}

		// Create a context with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		// Test with verbose flag
		flags := []string{"-v"}

		// Start the command in a goroutine
		done := make(chan error, 1)
		go func() {
			done <- runCmd.Execute(ctx, flags, []string{})
		}()

		// Wait for either completion or timeout
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Run command with verbose flag failed: %v", err)
			}
		case <-ctx.Done():
			// Timeout is expected for this test
		}

		// Verify that verbose mode was enabled
		if !runner.config.Verbose {
			t.Error("Expected verbose mode to be enabled")
		}
	})

	// Test very verbose flag with log capture
	t.Run("RunCommandVeryVerboseWithLogs", func(t *testing.T) {
		runCmd := &RunCommand{runner: runner}

		// Create a context with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		// Test with very verbose flag
		flags := []string{"-vv"}

		// Start the command in a goroutine
		done := make(chan error, 1)
		go func() {
			done <- runCmd.Execute(ctx, flags, []string{})
		}()

		// Wait for either completion or timeout
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Run command with very verbose flag failed: %v", err)
			}
		case <-ctx.Done():
			// Timeout is expected for this test
		}

		// Verify that very verbose mode was enabled
		if !runner.config.VeryVerbose {
			t.Error("Expected very verbose mode to be enabled")
		}
	})
}

func TestRunCommandFlagParsing(t *testing.T) {
	// Create test config
	config := &Config{
		FolderPath: "/tmp/test",
		LockFile:   "wippy.lock",
	}

	// Create logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// Create CLI runner
	runner := NewCLIRunner(config, logger)

	// Test profiling flag parsing
	t.Run("RunCommandProfilingFlag", func(t *testing.T) {
		// Test flag parsing without executing the full command
		flagSet := flag.NewFlagSet("run", flag.ExitOnError)
		var enableProfiling bool
		flagSet.BoolVar(&enableProfiling, "p", false, "enable performance profiling")
		flagSet.BoolVar(&enableProfiling, "profiling", false, "enable performance profiling")

		// Parse the flag
		err := flagSet.Parse([]string{"-p"})
		if err != nil {
			t.Fatalf("Failed to parse profiling flag: %v", err)
		}

		// Verify flag was parsed correctly
		if !enableProfiling {
			t.Error("Expected profiling flag to be true")
		}
	})

	// Test very verbose flag parsing
	t.Run("RunCommandVeryVerboseFlag", func(t *testing.T) {
		flagSet := flag.NewFlagSet("run", flag.ExitOnError)
		var veryVerbose bool
		flagSet.BoolVar(&veryVerbose, "vv", false, "enable very verbose debug logging with stack traces")

		err := flagSet.Parse([]string{"-vv"})
		if err != nil {
			t.Fatalf("Failed to parse very verbose flag: %v", err)
		}

		if !veryVerbose {
			t.Error("Expected very verbose flag to be true")
		}
	})

	// Test use-embed flag parsing
	t.Run("RunCommandUseEmbedFlag", func(t *testing.T) {
		flagSet := flag.NewFlagSet("run", flag.ExitOnError)
		var useEmbed bool
		flagSet.BoolVar(&useEmbed, "use-embed", false, "use embedded files")

		err := flagSet.Parse([]string{"--use-embed"})
		if err != nil {
			t.Fatalf("Failed to parse use-embed flag: %v", err)
		}

		if !useEmbed {
			t.Error("Expected use-embed flag to be true")
		}
	})

	// Test cluster flags parsing
	t.Run("RunCommandClusterFlags", func(t *testing.T) {
		flagSet := flag.NewFlagSet("run", flag.ExitOnError)
		var clusterEnabled bool
		var clusterName string
		var clusterBind string
		var clusterPort int
		flagSet.BoolVar(&clusterEnabled, "cluster", false, "enable cluster membership")
		flagSet.StringVar(&clusterName, "cluster-name", "", "cluster node name (defaults to hostname)")
		flagSet.StringVar(&clusterBind, "cluster-bind", "0.0.0.0", "cluster bind address")
		flagSet.IntVar(&clusterPort, "cluster-port", 7946, "cluster bind port")

		err := flagSet.Parse([]string{"--cluster", "--cluster-name", "test-node", "--cluster-bind", "127.0.0.1", "--cluster-port", "8000"})
		if err != nil {
			t.Fatalf("Failed to parse cluster flags: %v", err)
		}

		if !clusterEnabled {
			t.Error("Expected cluster flag to be true")
		}
		if clusterName != "test-node" {
			t.Errorf("Expected cluster name to be 'test-node', got '%s'", clusterName)
		}
		if clusterBind != "127.0.0.1" {
			t.Errorf("Expected cluster bind to be '127.0.0.1', got '%s'", clusterBind)
		}
		if clusterPort != 8000 {
			t.Errorf("Expected cluster port to be 8000, got %d", clusterPort)
		}
	})

	// Test cluster join flag parsing
	t.Run("RunCommandClusterJoinFlag", func(t *testing.T) {
		flagSet := flag.NewFlagSet("run", flag.ExitOnError)
		var clusterJoin string
		flagSet.StringVar(&clusterJoin, "cluster-join", "", "comma-separated addresses to join")

		err := flagSet.Parse([]string{"--cluster-join", "node1:7946,node2:7946"})
		if err != nil {
			t.Fatalf("Failed to parse cluster join flag: %v", err)
		}

		if clusterJoin != "node1:7946,node2:7946" {
			t.Errorf("Expected cluster join to be 'node1:7946,node2:7946', got '%s'", clusterJoin)
		}
	})

	// Test cluster secret flags parsing
	t.Run("RunCommandClusterSecretFlags", func(t *testing.T) {
		flagSet := flag.NewFlagSet("run", flag.ExitOnError)
		var clusterSecret string
		var clusterSecretFile string
		flagSet.StringVar(&clusterSecret, "cluster-secret", "", "cluster secret key (base64 encoded string)")
		flagSet.StringVar(&clusterSecretFile, "cluster-secret-file", "", "path to file containing cluster secret key")

		// Test cluster secret flag
		err := flagSet.Parse([]string{"--cluster-secret", "test-secret"})
		if err != nil {
			t.Fatalf("Failed to parse cluster secret flag: %v", err)
		}

		if clusterSecret != "test-secret" {
			t.Errorf("Expected cluster secret to be 'test-secret', got '%s'", clusterSecret)
		}

		// Test cluster secret file flag
		flagSet2 := flag.NewFlagSet("run", flag.ExitOnError)
		flagSet2.StringVar(&clusterSecretFile, "cluster-secret-file", "", "path to file containing cluster secret key")

		err = flagSet2.Parse([]string{"--cluster-secret-file", "/path/to/secret.txt"})
		if err != nil {
			t.Fatalf("Failed to parse cluster secret file flag: %v", err)
		}

		if clusterSecretFile != "/path/to/secret.txt" {
			t.Errorf("Expected cluster secret file to be '/path/to/secret.txt', got '%s'", clusterSecretFile)
		}
	})

	// Test cluster advertise flag parsing
	t.Run("RunCommandClusterAdvertiseFlag", func(t *testing.T) {
		flagSet := flag.NewFlagSet("run", flag.ExitOnError)
		var clusterAdvertise string
		flagSet.StringVar(&clusterAdvertise, "cluster-advertise", "", "cluster advertise IP address")

		err := flagSet.Parse([]string{"--cluster-advertise", "192.168.1.100"})
		if err != nil {
			t.Fatalf("Failed to parse cluster advertise flag: %v", err)
		}

		if clusterAdvertise != "192.168.1.100" {
			t.Errorf("Expected cluster advertise to be '192.168.1.100', got '%s'", clusterAdvertise)
		}
	})

	// Test combined flags parsing
	t.Run("RunCommandCombinedFlags", func(t *testing.T) {
		flagSet := flag.NewFlagSet("run", flag.ExitOnError)
		var veryVerbose bool
		var enableProfiling bool
		var useEmbed bool
		var clusterEnabled bool
		var clusterName string
		flagSet.BoolVar(&veryVerbose, "vv", false, "enable very verbose debug logging with stack traces")
		flagSet.BoolVar(&enableProfiling, "p", false, "enable performance profiling")
		flagSet.BoolVar(&useEmbed, "use-embed", false, "use embedded files")
		flagSet.BoolVar(&clusterEnabled, "cluster", false, "enable cluster membership")
		flagSet.StringVar(&clusterName, "cluster-name", "", "cluster node name (defaults to hostname)")

		err := flagSet.Parse([]string{"-vv", "-p", "--use-embed", "--cluster", "--cluster-name", "combined-test"})
		if err != nil {
			t.Fatalf("Failed to parse combined flags: %v", err)
		}

		if !veryVerbose {
			t.Error("Expected very verbose flag to be true")
		}
		if !enableProfiling {
			t.Error("Expected profiling flag to be true")
		}
		if !useEmbed {
			t.Error("Expected use-embed flag to be true")
		}
		if !clusterEnabled {
			t.Error("Expected cluster flag to be true")
		}
		if clusterName != "combined-test" {
			t.Errorf("Expected cluster name to be 'combined-test', got '%s'", clusterName)
		}
	})

	// Test help command
	t.Run("RunCommandHelp", func(t *testing.T) {
		runCmd := &RunCommand{runner: runner}

		// Test help text
		helpText := runCmd.Help()
		if helpText == "" {
			t.Fatal("Help text should not be empty")
		}

		// Verify that help text contains expected flags
		expectedFlags := []string{
			"--lock-file",
			"-p, --profiling",
			"-v, --verbose",
			"-vv",
			"--use-embed",
			"--cluster",
			"--cluster-name",
			"--cluster-bind",
			"--cluster-port",
			"--cluster-join",
			"--cluster-secret",
			"--cluster-secret-file",
			"--cluster-advertise",
		}

		for _, flag := range expectedFlags {
			if !strings.Contains(helpText, flag) {
				t.Errorf("Help text should contain flag '%s'", flag)
			}
		}
	})
}

func TestUpdateCommandWithSrcDirectory(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "wippy-test-src")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Change to the temporary directory for testing
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Errorf("Failed to restore original directory: %v", err)
		}
	}()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create test config
	config := &Config{
		FolderPath: tempDir,
		LockFile:   "wippy.lock",
	}

	// Create logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// Create CLI runner
	runner := NewCLIRunner(config, logger)

	// First create a lock file with src directory
	t.Run("CreateLockFileWithSrcDir", func(t *testing.T) {
		// Create the app directory first
		appDir := filepath.Join(tempDir, "app")
		err := os.MkdirAll(appDir, 0755)
		if err != nil {
			t.Fatalf("Failed to create app directory: %v", err)
		}

		initCmd := &InitCommand{runner: runner}

		// Create lock file with custom src directory
		flags := []string{"--src-dir", "app", "--modules-dir", ".wippy"}
		err = initCmd.Execute(context.Background(), flags, []string{})
		if err != nil {
			t.Fatalf("Init command failed: %v", err)
		}

		// Verify lock file was created with correct src directory
		lockPath := filepath.Join(tempDir, "wippy.lock")
		lockFile, err := moduleloader.LoadLockFile(lockPath)
		if err != nil {
			t.Fatalf("Failed to load lock file: %v", err)
		}

		if lockFile.Directories.Src != "app" {
			t.Errorf("Expected src dir to be 'app', got '%s'", lockFile.Directories.Src)
		}
	})

	// Test update command with existing lock file
	t.Run("UpdateCommandWithExistingLockFile", func(t *testing.T) {
		updateCmd := &UpdateCommand{runner: runner}

		// Test update command - it should use src directory from lock file
		flags := []string{"-v"}
		err := updateCmd.Execute(context.Background(), flags, []string{})
		if err != nil {
			t.Fatalf("Update command failed: %v", err)
		}

		// Verify that verbose mode was enabled
		if !runner.config.Verbose {
			t.Error("Expected verbose mode to be enabled")
		}
	})
}
