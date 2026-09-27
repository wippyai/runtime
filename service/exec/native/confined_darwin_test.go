// SPDX-License-Identifier: MPL-2.0

//go:build darwin && cgo

package native

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	execapi "github.com/wippyai/runtime/api/service/exec"
	confinedarwin "github.com/wippyai/runtime/service/exec/native/internal/confinement/darwin"
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
	command = exec.Command("codesign", "--force", "--sign", "-", "--options", "hard,kill,runtime", helper)
	output, err = command.CombinedOutput()
	require.NoErrorf(t, err, "sign Darwin confinement helper: %s", output)
	payload, err := os.ReadFile(helper)
	require.NoError(t, err)
	digest := sha256.Sum256(payload)
	codeHash, err := confinedarwin.StaticCDHash(helper)
	require.NoError(t, err)
	darwinHelperPath = helper
	darwinHelperSHA256 = hex.EncodeToString(digest[:])
	darwinHelperCDHash = codeHash
	t.Cleanup(func() {
		darwinHelperPath = ""
		darwinHelperSHA256 = ""
		darwinHelperCDHash = ""
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

func TestNativeDarwinConfinementEnforcesPrivateEnvironment(t *testing.T) {
	installDarwinConfinementHelper(t)
	workspace := t.TempDir()
	target := buildDarwinConfinementTarget(t)

	factory := NewExecutorFactory(zap.NewNop())
	handle, err := factory.CreateExecutor(registry.ID{}, &execapi.NativeExecutorConfig{
		DefaultWorkDir: workspace,
		Confine: &execapi.Confinement{
			WorkDirRoots: []string{workspace},
			Env:          &execapi.ConfinementEnvironment{Set: map[string]string{"WIPPY_PINNED": "yes"}},
			Home:         "private",
		},
	})
	require.NoError(t, err)
	executor := handle.(*Executor)
	t.Cleanup(func() { require.NoError(t, executor.Close()) })
	process, err := executor.NewProcess(darwinPayloadCommand(target, "environment"), execapi.ProcessOptions{})
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
	lines := strings.Split(strings.TrimSpace(string(payload)), "\n")
	require.Len(t, lines, 3)
	require.Equal(t, "yes", lines[0])
	require.NotEqual(t, os.Getenv("HOME"), lines[1])
	require.NotEqual(t, os.Getenv("TMPDIR"), lines[2])
	require.Equal(t, filepath.Dir(lines[1]), filepath.Dir(lines[2]))
	_, err = os.Stat(filepath.Dir(lines[1]))
	require.ErrorIs(t, err, os.ErrNotExist, "private environment is removed after target exit")
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
	stdout := process.Stdout()
	stderr := process.Stderr()
	started := time.Now()
	startErr := process.Start()
	if startErr != nil {
		diagnostics, _ := io.ReadAll(stderr)
		require.NoErrorf(t, startErr, "confined target stderr: %s", diagnostics)
	}
	marker, err := bufio.NewReader(stdout).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "SPAWN_DENIED\n", marker)
	err = process.Wait()
	status := execapi.ClassifyExit(err)
	require.NoError(t, status.Err)
	require.Equal(t, 137, status.Code)
	require.Equal(t, int(syscall.SIGKILL), status.Signal)
	elapsed := time.Since(started)
	require.GreaterOrEqual(t, elapsed, 800*time.Millisecond)
	require.Less(t, elapsed, 10*time.Second)
}
