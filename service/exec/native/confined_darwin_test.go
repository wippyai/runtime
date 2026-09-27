// SPDX-License-Identifier: MPL-2.0

//go:build darwin && cgo

package native

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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

func TestDarwinConfinedPayload(t *testing.T) {
	separator := -1
	for index, arg := range os.Args {
		if arg == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}
	switch os.Args[separator+1] {
	case "filesystem":
		if separator+4 >= len(os.Args) {
			os.Exit(90)
		}
		allowed, denied, output := os.Args[separator+2], os.Args[separator+3], os.Args[separator+4]
		payload, err := os.ReadFile(allowed)
		if err != nil {
			os.Exit(91)
		}
		if _, err := os.ReadFile(denied); err == nil {
			os.Exit(92)
		}
		if err := os.WriteFile(output, []byte("written"), 0o600); err != nil {
			os.Exit(93)
		}
		fmt.Printf("%s:%s", os.Getenv("WIPPY_PINNED"), payload)
	case "spawn-denied":
		child := exec.Command(os.Args[0], "-test.run=^TestDarwinConfinedPayload$", "--", "sleep")
		if err := child.Start(); err == nil {
			_ = child.Process.Kill()
			os.Exit(94)
		}
		time.Sleep(time.Hour)
	case "sleep":
		time.Sleep(time.Hour)
	default:
		os.Exit(95)
	}
}

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

func darwinPayloadCommand(t *testing.T, arguments ...string) string {
	t.Helper()
	executable, err := os.Executable()
	require.NoError(t, err)
	parts := []string{strconv.Quote(executable), "-test.run=^TestDarwinConfinedPayload$", "--"}
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
	executable, err := os.Executable()
	require.NoError(t, err)
	executableRoot := filepath.Dir(executable)

	factory := NewExecutorFactory(zap.NewNop())
	handle, err := factory.CreateExecutor(registry.ID{}, &execapi.NativeExecutorConfig{
		DefaultWorkDir: workspace,
		Confine: &execapi.Confinement{
			WorkDirRoots: []string{workspace},
			FS: &execapi.ConfinementFS{
				Read: []string{workspace, executableRoot}, Write: []string{workspace}, Exec: []string{executableRoot},
			},
			Env: &execapi.ConfinementEnvironment{Set: map[string]string{"WIPPY_PINNED": "yes"}},
		},
	})
	require.NoError(t, err)
	executor := handle.(*Executor)
	t.Cleanup(func() { require.NoError(t, executor.Close()) })
	process, err := executor.NewProcess(darwinPayloadCommand(t, "filesystem", allowed, denied, output), execapi.ProcessOptions{})
	require.NoError(t, err)
	stdout := process.Stdout()
	require.NoError(t, process.Start())
	payload, err := io.ReadAll(stdout)
	require.NoError(t, err)
	require.NoError(t, process.Wait())
	require.Equal(t, "yes:allowed", string(payload))
	written, err := os.ReadFile(output)
	require.NoError(t, err)
	require.Equal(t, "written", string(written))
}

func TestNativeDarwinConfinementWallUsesSingleProcessDomain(t *testing.T) {
	installDarwinConfinementHelper(t)
	workspace := t.TempDir()
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
	process, err := executor.NewProcess(darwinPayloadCommand(t, "spawn-denied"), execapi.ProcessOptions{})
	require.NoError(t, err)
	started := time.Now()
	require.NoError(t, process.Start())
	err = process.Wait()
	require.Error(t, err)
	require.Less(t, time.Since(started), 10*time.Second)
}
