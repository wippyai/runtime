// SPDX-License-Identifier: MPL-2.0

//go:build darwin && cgo

package native

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	execapi "github.com/wippyai/runtime/api/service/exec"
	"go.uber.org/zap"
)

func installDarwinConfinementHelper(t *testing.T) {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(t, err)
	helper := filepath.Join(t.TempDir(), "confine-darwin")
	command := exec.Command("go", "build", "-trimpath", "-o", helper, "./service/exec/native/cmd/confine-darwin")
	command.Dir = repoRoot
	output, err := command.CombinedOutput()
	require.NoErrorf(t, err, "build Darwin confinement helper: %s", output)
	payload, err := os.ReadFile(helper)
	require.NoError(t, err)
	digest := sha256.Sum256(payload)
	darwinHelperPath = helper
	darwinHelperSHA256 = hex.EncodeToString(digest[:])
	t.Cleanup(func() {
		darwinHelperPath = ""
		darwinHelperSHA256 = ""
	})
}

func buildDarwinConfinementTarget(t *testing.T) string {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(t, err)
	target := filepath.Join(t.TempDir(), "confine-target")
	command := exec.Command("go", "build", "-trimpath", "-o", target,
		"./service/exec/native/internal/confinement/darwin/testdata/target")
	command.Dir = repoRoot
	output, err := command.CombinedOutput()
	require.NoErrorf(t, err, "build Darwin confinement target: %s", output)
	return target
}

func darwinPayloadCommand(target string, arguments ...string) string {
	parts := []string{strconv.Quote(target)}
	for _, argument := range arguments {
		parts = append(parts, strconv.Quote(argument))
	}
	return strings.Join(parts, " ")
}

func TestNativeDarwinConfinementEnforcesFilesystem(t *testing.T) {
	installDarwinConfinementHelper(t)
	workspace := t.TempDir()
	deniedRoot := t.TempDir()
	allowed := filepath.Join(workspace, "input")
	denied := filepath.Join(deniedRoot, "secret")
	output := filepath.Join(workspace, "output")
	require.NoError(t, os.WriteFile(allowed, []byte("allowed"), 0o600))
	require.NoError(t, os.WriteFile(denied, []byte("denied"), 0o600))
	target := buildDarwinConfinementTarget(t)
	executableRoot := filepath.Dir(target)
	loaderRoots := existingDarwinDirectories(
		"/usr/lib", "/System/Library/Frameworks", "/System/Library/PrivateFrameworks",
		"/System/Cryptexes/App", "/System/Cryptexes/OS", "/Library/Apple/System/Library/Frameworks",
	)
	readRoots := append([]string{workspace, executableRoot}, loaderRoots...)
	execRoots := append([]string{executableRoot}, loaderRoots...)

	factory := NewExecutorFactory(zap.NewNop())
	handle, err := factory.CreateExecutor(registry.ID{}, &execapi.NativeExecutorConfig{
		DefaultWorkDir: workspace,
		Confine: &execapi.Confinement{
			WorkDirRoots: []string{workspace},
			FS: &execapi.ConfinementFS{
				Read: readRoots, Write: []string{workspace}, Exec: execRoots,
			},
			Env: &execapi.ConfinementEnvironment{Set: map[string]string{"WIPPY_PINNED": "yes"}},
		},
	})
	require.NoError(t, err)
	executor := handle.(*Executor)
	t.Cleanup(func() { require.NoError(t, executor.Close()) })
	process, err := executor.NewProcess(darwinPayloadCommand(target, "filesystem", allowed, denied, output), execapi.ProcessOptions{})
	require.NoError(t, err)
	stdout := process.Stdout()
	stderr := process.Stderr()
	startErr := process.Start()
	if startErr != nil {
		diagnostics, _ := io.ReadAll(stderr)
		require.NoErrorf(t, startErr, "confined target stderr: %s", diagnostics)
	}
	payload, err := io.ReadAll(stdout)
	require.NoError(t, err)
	waitErr := process.Wait()
	diagnostics, _ := io.ReadAll(stderr)
	require.NoErrorf(t, waitErr, "confined target stderr: %s", diagnostics)
	require.Equal(t, "yes:allowed", string(payload))
	written, err := os.ReadFile(output)
	require.NoError(t, err)
	require.Equal(t, "written", string(written))
}

func existingDarwinDirectories(paths ...string) []string {
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			result = append(result, path)
		}
	}
	return result
}

func TestNativeDarwinConfinementWallUsesSingleProcessDomain(t *testing.T) {
	installDarwinConfinementHelper(t)
	workspace := t.TempDir()
	target := buildDarwinConfinementTarget(t)
	factory := NewExecutorFactory(zap.NewNop())
	handle, err := factory.CreateExecutor(registry.ID{}, &execapi.NativeExecutorConfig{
		DefaultWorkDir: workspace,
		Confine: &execapi.Confinement{
			WorkDirRoots: []string{workspace}, Limits: &execapi.ConfinementLimits{WallSec: 1},
			Tree: &execapi.ConfinementTree{KillOnOwnerExit: true},
		},
	})
	require.NoError(t, err)
	executor := handle.(*Executor)
	t.Cleanup(func() { require.NoError(t, executor.Close()) })
	process, err := executor.NewProcess(darwinPayloadCommand(target, "spawn-denied"), execapi.ProcessOptions{})
	require.NoError(t, err)
	stderr := process.Stderr()
	started := time.Now()
	startErr := process.Start()
	if startErr != nil {
		diagnostics, _ := io.ReadAll(stderr)
		require.NoErrorf(t, startErr, "confined target stderr: %s", diagnostics)
	}
	err = process.Wait()
	require.Error(t, err)
	require.Less(t, time.Since(started), 10*time.Second)
}
