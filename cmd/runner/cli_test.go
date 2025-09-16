package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

		// Create a context with timeout to prevent hanging
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		// Test with verbose flag in a goroutine to prevent blocking
		errChan := make(chan error, 1)
		go func() {
			flags := []string{"-v"}
			err := runCmd.Execute(ctx, flags, []string{})
			errChan <- err
		}()

		// Wait for either completion or timeout
		select {
		case err := <-errChan:
			if err != nil && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Run command with verbose flag failed: %v", err)
			}
		case <-ctx.Done():
			// This is expected - the command should timeout
			if ctx.Err() != context.DeadlineExceeded {
				t.Fatalf("Unexpected context error: %v", ctx.Err())
			}
		}

		// Verify that verbose mode was enabled
		if !runner.config.Verbose {
			t.Error("Expected verbose mode to be enabled")
		}
	})

	// Test run command with context cancellation
	t.Run("RunCommandWithContextCancellation", func(t *testing.T) {
		runCmd := &RunCommand{runner: runner}

		// Create a context that will be canceled after a short delay
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		// Test with verbose flag in a goroutine
		errChan := make(chan error, 1)
		go func() {
			flags := []string{"-v"}
			err := runCmd.Execute(ctx, flags, []string{})
			errChan <- err
		}()

		// Wait for context cancellation
		select {
		case err := <-errChan:
			// The command should return due to context cancellation
			if err != nil && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Run command with context cancellation failed: %v", err)
			}
		case <-ctx.Done():
			// This is expected - the command should timeout
			if ctx.Err() != context.DeadlineExceeded {
				t.Fatalf("Unexpected context error: %v", ctx.Err())
			}
		}

		// Verify that verbose mode was enabled
		if !runner.config.Verbose {
			t.Error("Expected verbose mode to be enabled")
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
		initCmd := &InitCommand{runner: runner}

		// Create lock file with custom src directory
		flags := []string{"--src-dir", "app", "--modules-dir", ".wippy"}
		err := initCmd.Execute(context.Background(), flags, []string{})
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
		// Create the src directory that was specified in the lock file
		appDir := filepath.Join(tempDir, "app")
		if err := os.MkdirAll(appDir, 0755); err != nil {
			t.Fatalf("Failed to create app directory: %v", err)
		}

		// Create a simple test file in the app directory
		testFile := filepath.Join(appDir, "test.yaml")
		if err := os.WriteFile(testFile, []byte("kind: test\n"), 0600); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

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
