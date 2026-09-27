// SPDX-License-Identifier: MPL-2.0

//go:build windows

package native

import (
	"bufio"
	"errors"
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
	confinewindows "github.com/wippyai/runtime/service/exec/native/internal/confinement/windows"
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
		if separator+3 >= len(os.Args) {
			os.Exit(93)
		}
		targetPID, err := strconv.Atoi(os.Args[separator+2])
		if err != nil {
			os.Exit(94)
		}
		sentinel, err := strconv.ParseUint(os.Args[separator+3], 10, 64)
		if err != nil {
			os.Exit(89)
		}
		isContainer, err := confinewindows.CurrentProcessIsLPAC()
		if err != nil || !isContainer {
			os.Exit(95)
		}
		for index, access := range []uint32{
			windows.PROCESS_DUP_HANDLE,
			windows.PROCESS_VM_WRITE,
			windows.PROCESS_VM_OPERATION,
			windows.PROCESS_CREATE_PROCESS,
			windows.PROCESS_CREATE_THREAD,
		} {
			handle, openErr := windows.OpenProcess(access, false, uint32(targetPID))
			if openErr == nil {
				_ = windows.CloseHandle(handle)
				os.Exit(80 + index)
			}
			if !errors.Is(openErr, windows.ERROR_ACCESS_DENIED) {
				os.Exit(70 + index)
			}
		}
		status, handleErr := windows.WaitForSingleObject(windows.Handle(sentinel), 0)
		if status != windows.WAIT_FAILED {
			os.Exit(88)
		} else if !errors.Is(handleErr, windows.ERROR_INVALID_HANDLE) {
			os.Exit(87)
		}
		fmt.Printf("%s\n%s\nlpac\ntarget_denied\nhandle_denied\n", os.Getenv("WIPPY_PINNED"), os.Getenv("USERPROFILE"))
	case "hold":
		isContainer, err := confinewindows.CurrentProcessIsLPAC()
		if err != nil || !isContainer {
			os.Exit(86)
		}
		fmt.Println(os.Getpid())
		if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
			os.Exit(85)
		}
		if err := os.WriteFile("holder-still-authorized", []byte("ok"), 0o600); err != nil {
			os.Exit(84)
		}
		fmt.Println("WROTE")
		for {
			time.Sleep(time.Hour)
		}
	case "environment":
		isContainer, err := confinewindows.CurrentProcessIsLPAC()
		if err != nil || !isContainer {
			os.Exit(83)
		}
		fmt.Printf("lpac\n%s\n%s\n%s\n%s\nsentinel=%s\n", os.Getenv("WIPPY_PINNED"),
			os.Getenv("LOCALAPPDATA"), os.Getenv("TEMP"), os.Getenv("TMP"),
			os.Getenv("WIPPY_HOST_SENTINEL"))
	case "tree":
		command := exec.Command(os.Args[0], "-test.run=^TestWindowsConfinedPayload$", "--", "grandchild")
		command.Env = os.Environ()
		if err := command.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
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

func TestNativeWindowsConfinementRejectsPlatformManagedEnvironment(t *testing.T) {
	workDir := t.TempDir()
	for _, environment := range []*execapi.ConfinementEnvironment{
		{Set: map[string]string{"LocalAppData": workDir}},
		{Allow: []string{"temp"}},
	} {
		factory := NewExecutorFactory(zap.NewNop())
		_, err := factory.CreateExecutor(registry.ID{}, &execapi.NativeExecutorConfig{
			DefaultWorkDir: workDir,
			Confine: &execapi.Confinement{
				WorkDirRoots: []string{workDir}, Env: environment,
			},
		})
		require.ErrorIs(t, err, execapi.ErrInvalidConfinement)
	}
}

func windowsPayloadCommand(t *testing.T, mode string, arguments ...string) string {
	t.Helper()
	executable, err := os.Executable()
	require.NoError(t, err)
	parts := []string{strconv.Quote(executable), "-test.run=^TestWindowsConfinedPayload$", "--", mode}
	for _, argument := range arguments {
		parts = append(parts, strconv.Quote(argument))
	}
	return strings.Join(parts, " ")
}

func TestNativeWindowsConfinementRunsInsideJob(t *testing.T) {
	workDir := t.TempDir()
	executor := newWindowsConfinedExecutor(t, workDir)
	sentinel := newInheritableWindowsSentinel(t)
	process, err := executor.NewProcess(windowsPayloadCommand(t, "basic", strconv.Itoa(os.Getpid()),
		strconv.FormatUint(uint64(sentinel), 10)), execapi.ProcessOptions{})
	require.NoError(t, err)
	stdout := process.Stdout()
	stderr := process.Stderr()
	require.NoError(t, process.Start())
	stderrRead := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(stderr)
		stderrRead <- data
	}()
	payload, err := io.ReadAll(stdout)
	require.NoError(t, err)
	require.NoError(t, process.Wait(), string(<-stderrRead))
	lines := strings.Split(strings.TrimSpace(string(payload)), "\n")
	require.GreaterOrEqual(t, len(lines), 5)
	require.Equal(t, "yes", lines[0])
	require.NotEmpty(t, lines[1])
	require.NotEqual(t, os.Getenv("USERPROFILE"), lines[1])
	require.Equal(t, "lpac", lines[2])
	require.Equal(t, "target_denied", lines[3])
	require.Equal(t, "handle_denied", lines[4])
	_, err = os.Stat(lines[1])
	require.ErrorIs(t, err, os.ErrNotExist, "private home is removed after the job is empty")
}

func TestNativeWindowsConfinementBootstrapsAppContainerEnvironment(t *testing.T) {
	t.Setenv("WIPPY_HOST_SENTINEL", "must-not-leak")
	workDir := t.TempDir()
	factory := NewExecutorFactory(zap.NewNop())
	handle, err := factory.CreateExecutor(registry.ID{}, &execapi.NativeExecutorConfig{
		DefaultWorkDir: workDir,
		Confine: &execapi.Confinement{
			WorkDirRoots: []string{workDir},
			Env:          &execapi.ConfinementEnvironment{Set: map[string]string{"WIPPY_PINNED": "yes"}},
		},
	})
	require.NoError(t, err)
	executor := handle.(*Executor)
	t.Cleanup(func() { require.NoError(t, executor.Close()) })
	process, err := executor.NewProcess(windowsPayloadCommand(t, "environment"), execapi.ProcessOptions{})
	require.NoError(t, err)
	stdout := process.Stdout()
	require.NoError(t, process.Start())
	payload, err := io.ReadAll(stdout)
	require.NoError(t, err)
	require.NoError(t, process.Wait())
	lines := strings.Split(strings.TrimSpace(string(payload)), "\n")
	require.Len(t, lines, 7)
	require.Equal(t, "lpac", lines[0])
	require.Equal(t, "yes", lines[1])
	require.NotEmpty(t, lines[2], "Windows must rewrite LOCALAPPDATA for the AppContainer")
	require.NotEqual(t, os.Getenv("LOCALAPPDATA"), lines[2])
	require.NotEmpty(t, lines[3], "Windows must provide an AppContainer TEMP")
	require.NotEmpty(t, lines[4], "Windows must provide an AppContainer TMP")
	require.Equal(t, "sentinel=", lines[5], "unlisted host environment must not leak")
	require.Equal(t, "PASS", lines[6])
}

func TestNativeWindowsConfinementIsolatesPeerLPACs(t *testing.T) {
	workDir := t.TempDir()
	executor := newWindowsConfinedExecutor(t, workDir)
	holder, err := executor.NewProcess(windowsPayloadCommand(t, "hold"), execapi.ProcessOptions{})
	require.NoError(t, err)
	holderOutput := holder.Stdout()
	require.NoError(t, holder.Start())
	holderReader := bufio.NewReader(holderOutput)
	line, err := holderReader.ReadString('\n')
	require.NoError(t, err)
	holderPID, err := strconv.Atoi(strings.TrimSpace(line))
	require.NoError(t, err)

	sentinel := newInheritableWindowsSentinel(t)
	probe, err := executor.NewProcess(windowsPayloadCommand(t, "basic", strconv.Itoa(holderPID),
		strconv.FormatUint(uint64(sentinel), 10)), execapi.ProcessOptions{})
	require.NoError(t, err)
	probeOutput := probe.Stdout()
	require.NoError(t, probe.Start())
	payload, err := io.ReadAll(probeOutput)
	require.NoError(t, err)
	require.NoError(t, probe.Wait(), string(payload))
	require.Contains(t, string(payload), "target_denied")
	require.NoError(t, holder.WriteStdin([]byte("write\n")))
	line, err = holderReader.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "WROTE", strings.TrimSpace(line), "first sandbox must retain its ACL after peer cleanup")

	holder.(*ProcessExecutor).Stop()
	_ = holder.Wait()
	requireWindowsProcessGone(t, uint32(holderPID))
}

func newInheritableWindowsSentinel(t *testing.T) windows.Handle {
	t.Helper()
	handle, err := windows.CreateEvent(nil, 0, 0, nil)
	require.NoError(t, err)
	require.NoError(t, windows.SetHandleInformation(handle, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT))
	t.Cleanup(func() { require.NoError(t, windows.CloseHandle(handle)) })
	return handle
}

func TestNativeWindowsConfinementStopKillsDescendants(t *testing.T) {
	workDir := t.TempDir()
	executor := newWindowsConfinedExecutor(t, workDir)
	process, err := executor.NewProcess(windowsPayloadCommand(t, "tree"), execapi.ProcessOptions{})
	require.NoError(t, err)
	stdout := process.Stdout()
	stderr := process.Stderr()
	require.NoError(t, process.Start())
	stderrRead := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(stderr)
		stderrRead <- data
	}()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		require.NoError(t, err, string(<-stderrRead))
	}
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
