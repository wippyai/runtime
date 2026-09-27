// SPDX-License-Identifier: MPL-2.0

//go:build windows

package native

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	execapi "github.com/wippyai/runtime/api/service/exec"
	"go.uber.org/zap"
	"golang.org/x/sys/windows"
)

func TestWindowsConfinedPayload(t *testing.T) {
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
	case "basic":
		fmt.Printf("%s\n%s\n", os.Getenv("WIPPY_PINNED"), os.Getenv("USERPROFILE"))
	case "tree":
		command := exec.Command(os.Args[0], "-test.run=^TestWindowsConfinedPayload$", "--", "grandchild")
		command.Env = os.Environ()
		if err := command.Start(); err != nil {
			os.Exit(91)
		}
		fmt.Println(command.Process.Pid)
		for {
			time.Sleep(time.Hour)
		}
	case "grandchild":
		for {
			time.Sleep(time.Hour)
		}
	default:
		os.Exit(92)
	}
}

func newWindowsConfinedExecutor(t *testing.T, workDir string) *Executor {
	t.Helper()
	factory := NewExecutorFactory(zap.NewNop())
	handle, err := factory.CreateExecutor(registry.ID{}, &execapi.NativeExecutorConfig{
		DefaultWorkDir: workDir,
		Confine: &execapi.Confinement{
			WorkDirRoots: []string{workDir},
			Env:          &execapi.ConfinementEnvironment{Set: map[string]string{"WIPPY_PINNED": "yes"}},
			Home:         "private",
			Limits:       &execapi.ConfinementLimits{MemoryMiB: 512, WallSec: 20},
			Tree:         &execapi.ConfinementTree{KillOnOwnerExit: true},
		},
	})
	require.NoError(t, err)
	executor, ok := handle.(*Executor)
	require.True(t, ok)
	t.Cleanup(func() { require.NoError(t, executor.Close()) })
	return executor
}

func windowsPayloadCommand(t *testing.T, mode string) string {
	t.Helper()
	executable, err := os.Executable()
	require.NoError(t, err)
	return strconv.Quote(executable) + " -test.run=^TestWindowsConfinedPayload$ -- " + mode
}

func TestNativeWindowsConfinementRunsInsideJob(t *testing.T) {
	workDir := t.TempDir()
	executor := newWindowsConfinedExecutor(t, workDir)
	process, err := executor.NewProcess(windowsPayloadCommand(t, "basic"), execapi.ProcessOptions{})
	require.NoError(t, err)
	stdout := process.Stdout()
	require.NoError(t, process.Start())
	payload, err := io.ReadAll(stdout)
	require.NoError(t, err)
	require.NoError(t, process.Wait())
	lines := strings.Split(strings.TrimSpace(string(payload)), "\n")
	require.GreaterOrEqual(t, len(lines), 2)
	require.Equal(t, "yes", lines[0])
	require.NotEmpty(t, lines[1])
	require.NotEqual(t, os.Getenv("USERPROFILE"), lines[1])
	_, err = os.Stat(lines[1])
	require.ErrorIs(t, err, os.ErrNotExist, "private home is removed after the job is empty")
}

func TestNativeWindowsConfinementStopKillsDescendants(t *testing.T) {
	workDir := t.TempDir()
	executor := newWindowsConfinedExecutor(t, workDir)
	process, err := executor.NewProcess(windowsPayloadCommand(t, "tree"), execapi.ProcessOptions{})
	require.NoError(t, err)
	stdout := process.Stdout()
	require.NoError(t, process.Start())
	line, err := bufio.NewReader(stdout).ReadString('\n')
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	require.NoError(t, err)

	process.(*ProcessExecutor).Stop()
	_ = process.Wait()
	requireWindowsProcessGone(t, uint32(pid))
}

func requireWindowsProcessGone(t *testing.T, pid uint32) {
	t.Helper()
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return
	}
	defer windows.CloseHandle(handle)
	status, err := windows.WaitForSingleObject(handle, uint32((5*time.Second)/time.Millisecond))
	require.NoError(t, err)
	require.Equal(t, uint32(windows.WAIT_OBJECT_0), status)
}
