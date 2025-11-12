package minimal_app_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	testDirName    = "minimal_app"
	testScriptName = "test.sh"
	makeTarget     = "build-runner-local"
)

func TestMinimalAppIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	projectRoot, err := findProjectRoot()
	require.NoError(t, err, "Failed to find project root")

	buildCmd := exec.Command("make", makeTarget)
	buildCmd.Dir = projectRoot
	output, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "Failed to build runner: %s", string(output))

	testDir := filepath.Join(projectRoot, "tests", testDirName)
	testScript := filepath.Join(testDir, testScriptName)

	cmd := exec.Command("/bin/bash", testScript)
	cmd.Dir = testDir
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "Integration test failed: %s", string(output))
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
