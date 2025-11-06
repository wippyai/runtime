package minimal_app_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMinimalAppIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Find project root
	projectRoot, err := findProjectRoot()
	if err != nil {
		t.Fatalf("Failed to find project root: %v", err)
	}

	// Build runner binary first
	t.Log("Building runner binary...")
	buildCmd := exec.Command("make", "build-runner-local")
	buildCmd.Dir = projectRoot
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build runner: %v\nOutput: %s", err, output)
	}

	// Run test.sh script
	t.Log("Running integration test script...")
	testDir := filepath.Join(projectRoot, "tests", "minimal_app")
	testScript := filepath.Join(testDir, "test.sh")

	cmd := exec.Command("/bin/bash", testScript)
	cmd.Dir = testDir
	output, err := cmd.CombinedOutput()

	t.Logf("Test script output:\n%s", output)

	if err != nil {
		t.Fatalf("Integration test failed: %v", err)
	}
}

func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// Walk up until we find go.mod or Makefile
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		if _, err := os.Stat(filepath.Join(dir, "Makefile")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
