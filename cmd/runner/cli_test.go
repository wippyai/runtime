package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
