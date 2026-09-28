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
	lines := strings.Split(strings.TrimSuffix(string(payload), "\n"), "\n")
	require.Len(t, lines, 3)
	require.Equal(t, "yes", lines[0])
	require.NotEqual(t, os.Getenv("HOME"), lines[1])
	require.Empty(t, lines[2], "home: private does not imply the separate {tmp} policy")
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

func TestDarwinSpawnRejectsSubstitutedSignedHelperBeforeExecution(t *testing.T) {
	installDarwinConfinementHelper(t)
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(t, err)
	marker := filepath.Join(t.TempDir(), "executed")
	substitute := filepath.Join(t.TempDir(), "substitute")
	command := exec.Command("go", "build", "-trimpath", "-ldflags", "-X main.marker="+marker,
		"-o", substitute, "./service/exec/native/internal/confinement/darwin/testdata/substitute")
	command.Dir = repoRoot
	output, err := command.CombinedOutput()
	require.NoErrorf(t, err, "build substitute Darwin helper: %s", output)
	command = exec.Command("codesign", "--force", "--sign", "-", "--options", "hard,kill,runtime", substitute)
	output, err = command.CombinedOutput()
	require.NoErrorf(t, err, "sign substitute Darwin helper: %s", output)

	input, err := os.Open(os.DevNull)
	require.NoError(t, err)
	defer input.Close()
	outputFile, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	require.NoError(t, err)
	defer outputFile.Close()
	policyReader, policyWriter, err := os.Pipe()
	require.NoError(t, err)
	defer policyReader.Close()
	defer policyWriter.Close()
	statusReader, statusWriter, err := os.Pipe()
	require.NoError(t, err)
	defer statusReader.Close()
	defer statusWriter.Close()
	workDir, err := os.Open(t.TempDir())
	require.NoError(t, err)
	defer workDir.Close()

	_, err = confinedarwin.SpawnVerified(substitute, darwinHelperCDHash,
		[6]*os.File{input, outputFile, outputFile, policyReader, statusWriter, workDir})
	require.Error(t, err)
	_, err = os.Stat(marker)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestNativeDarwinConfinementUsesBoundWorkDirAfterRootReplacement(t *testing.T) {
	installDarwinConfinementHelper(t)
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	workDir := filepath.Join(root, "work")
	require.NoError(t, os.MkdirAll(workDir, 0o700))
	target := buildDarwinConfinementTarget(t)
	factory := NewExecutorFactory(zap.NewNop())
	handle, err := factory.CreateExecutor(registry.ID{}, &execapi.NativeExecutorConfig{
		DefaultWorkDir: workDir,
		Confine: &execapi.Confinement{
			WorkDirRoots: []string{root},
			Home:         "private",
		},
	})
	require.NoError(t, err)
	executor := handle.(*Executor)
	t.Cleanup(func() { require.NoError(t, executor.Close()) })
	process, err := executor.NewProcess(darwinPayloadCommand(target, "cwd"), execapi.ProcessOptions{})
	require.NoError(t, err)

	movedRoot := filepath.Join(parent, "bound-root")
	require.NoError(t, os.Rename(root, movedRoot))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "work"), 0o700))
	stdout := process.Stdout()
	require.NoError(t, process.Start())
	payload, err := io.ReadAll(stdout)
	require.NoError(t, err)
	require.NoError(t, process.Wait())
	expectedWorkDir, err := filepath.EvalSymlinks(filepath.Join(movedRoot, "work"))
	require.NoError(t, err)
	require.Equal(t, expectedWorkDir, strings.TrimSpace(string(payload)))
}
