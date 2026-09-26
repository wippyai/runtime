// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/wapp"
)

func TestRunBootReadinessOkHelper(t *testing.T) {
	if os.Getenv("WIPPY_BOOT_READINESS_OK_HELPER") != "1" {
		return
	}
	setTestConfigFiles(t, "wippy.yaml")
	cmd := &cobra.Command{}
	cmd.Flags().String("exec", "", "")
	cmd.Flags().String("host", "", "")
	cmd.Flags().String("registry", "", "")
	cmd.Flags().Bool("verbose", false, "")
	require.NoError(t, cmd.ParseFlags([]string{"--verbose", "--exec", "app:command"}))
	cmd.SetContext(context.Background())
	err := runWithUseCase(cmd, cmd.Flags().Args(), defaultUseCase)
	if err != nil {
		os.Exit(1)
	}
}

func TestRunBootReadinessFailHelper(t *testing.T) {
	if os.Getenv("WIPPY_BOOT_READINESS_FAIL_HELPER") != "1" {
		return
	}
	setTestConfigFiles(t, "wippy.yaml")
	cmd := &cobra.Command{}
	cmd.Flags().String("exec", "", "")
	cmd.Flags().String("host", "", "")
	cmd.Flags().String("registry", "", "")
	cmd.Flags().Bool("verbose", false, "")
	require.NoError(t, cmd.ParseFlags([]string{"--verbose", "--exec", "app:command"}))
	cmd.SetContext(context.Background())
	err := runWithUseCase(cmd, cmd.Flags().Args(), defaultUseCase)
	if err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}

func TestRunPackBootReadinessOkHelper(t *testing.T) {
	if os.Getenv("WIPPY_PACK_BOOT_READINESS_OK_HELPER") != "1" {
		return
	}
	setTestConfigFiles(t, "wippy.yaml")
	cmd := &cobra.Command{}
	cmd.Flags().String("exec", "", "")
	cmd.Flags().String("host", "", "")
	cmd.Flags().String("registry", "", "")
	cmd.Flags().Bool("verbose", false, "")
	require.NoError(t, cmd.ParseFlags([]string{"--verbose"}))
	cmd.SetContext(context.Background())
	err := runWithUseCase(cmd, []string{"app.wapp"}, defaultUseCase)
	if err != nil {
		os.Exit(1)
	}
}

func TestRunPackBootReadinessFailHelper(t *testing.T) {
	if os.Getenv("WIPPY_PACK_BOOT_READINESS_FAIL_HELPER") != "1" {
		return
	}
	setTestConfigFiles(t, "wippy.yaml")
	cmd := &cobra.Command{}
	cmd.Flags().String("exec", "", "")
	cmd.Flags().String("host", "", "")
	cmd.Flags().String("registry", "", "")
	cmd.Flags().Bool("verbose", false, "")
	require.NoError(t, cmd.ParseFlags([]string{"--verbose"}))
	cmd.SetContext(context.Background())
	err := runWithUseCase(cmd, []string{"app.wapp"}, defaultUseCase)
	if err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}

func TestRun_BootReadiness_Success_LogOrder(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.Mkdir(workDir+"/src", 0o755))
	require.NoError(t, os.WriteFile(workDir+"/wippy.lock", []byte("directories:\n  src: ./src\n  modules: .wippy\n"), 0o600))
	require.NoError(t, os.WriteFile(workDir+"/wippy.yaml", []byte("version: \"1.0\"\nshutdown:\n  timeout: 5s\n"), 0o600))
	require.NoError(t, os.WriteFile(workDir+"/src/_index.yaml", []byte(`version: "1.0"
namespace: app
entries:
  - name: terminal
    kind: terminal.host
    lifecycle:
      auto_start: true
  - name: workers
    kind: process.host
    host:
      workers: 2
    lifecycle:
      auto_start: true
  - name: boot_proc
    kind: process.lua
    method: main
    modules: [time]
    source: |
      local time = require("time")
      return {main = function()
        time.after("150ms"):receive()
        print("BOOTLOADER_FINISHED")
      end}
  - name: boot_service
    kind: process.service
    process: app:boot_proc
    host: app:workers
    lifecycle:
      auto_start: true
      boot_gate: true
  - name: command
    kind: process.lua
    method: main
    source: |
      return {main = function()
        print("COMMAND_EXECUTED")
      end}
`), 0o600))

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRunBootReadinessOkHelper$")
	child.Dir = workDir
	child.Env = append(os.Environ(), "WIPPY_BOOT_READINESS_OK_HELPER=1")
	output, err := child.CombinedOutput()
	require.NoError(t, err, "output: %s", output)

	outStr := string(output)
	bootIdx := strings.Index(outStr, "BOOTLOADER_FINISHED")
	cmdIdx := strings.Index(outStr, "COMMAND_EXECUTED")
	require.GreaterOrEqual(t, bootIdx, 0, "BOOTLOADER_FINISHED missing in output: %s", outStr)
	require.GreaterOrEqual(t, cmdIdx, 0, "COMMAND_EXECUTED missing in output: %s", outStr)
	require.Less(t, bootIdx, cmdIdx, "BOOTLOADER_FINISHED must appear BEFORE COMMAND_EXECUTED in output: %s", outStr)
}

func TestRun_BootReadiness_Fail_AbortsCommand(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.Mkdir(workDir+"/src", 0o755))
	require.NoError(t, os.WriteFile(workDir+"/wippy.lock", []byte("directories:\n  src: ./src\n  modules: .wippy\n"), 0o600))
	require.NoError(t, os.WriteFile(workDir+"/wippy.yaml", []byte("version: \"1.0\"\nshutdown:\n  timeout: 5s\n"), 0o600))
	require.NoError(t, os.WriteFile(workDir+"/src/_index.yaml", []byte(`version: "1.0"
namespace: app
entries:
  - name: terminal
    kind: terminal.host
    lifecycle:
      auto_start: true
  - name: workers
    kind: process.host
    host:
      workers: 2
    lifecycle:
      auto_start: true
  - name: boot_proc
    kind: process.lua
    method: main
    modules: [time]
    source: |
      return {main = function()
        error("deliberate migration failure")
      end}
  - name: boot_service
    kind: process.service
    process: app:boot_proc
    host: app:workers
    lifecycle:
      auto_start: true
      boot_gate: true
      restart:
        initial_delay: 50ms
  - name: command
    kind: process.lua
    method: main
    source: |
      return {main = function()
        print("COMMAND_SHOULD_NEVER_RUN")
      end}
`), 0o600))

	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRunBootReadinessFailHelper$")
	child.Dir = workDir
	child.Env = append(os.Environ(), "WIPPY_BOOT_READINESS_FAIL_HELPER=1")
	output, err := child.CombinedOutput()
	var exitErr *exec.ExitError
	require.True(t, errors.As(err, &exitErr), "expected non-zero exit, got err=%v output: %s", err, output)
	require.Equal(t, 1, exitErr.ExitCode(), "output: %s", output)

	outStr := string(output)
	require.False(t, strings.Contains(outStr, "COMMAND_SHOULD_NEVER_RUN"), "command entrypoint must NOT have executed: %s", outStr)
	require.True(t, strings.Contains(outStr, "deliberate migration failure") || strings.Contains(outStr, "boot gate \"app:boot_service\" failed"), "output should mention boot failure: %s", outStr)
}

func TestRun_BootReadiness_NoGatingServicesUnchanged(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.Mkdir(workDir+"/src", 0o755))
	require.NoError(t, os.WriteFile(workDir+"/wippy.lock", []byte("directories:\n  src: ./src\n  modules: .wippy\n"), 0o600))
	require.NoError(t, os.WriteFile(workDir+"/wippy.yaml", []byte("version: \"1.0\"\nshutdown:\n  timeout: 5s\n"), 0o600))
	require.NoError(t, os.WriteFile(workDir+"/src/_index.yaml", []byte(`version: "1.0"
namespace: app
entries:
  - name: terminal
    kind: terminal.host
    lifecycle:
      auto_start: true
  - name: command
    kind: process.lua
    method: main
    source: |
      return {main = function()
        print("NO_GATE_COMMAND_EXECUTED")
      end}
`), 0o600))

	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRunBootReadinessOkHelper$")
	child.Dir = workDir
	child.Env = append(os.Environ(), "WIPPY_BOOT_READINESS_OK_HELPER=1")
	output, err := child.CombinedOutput()
	require.NoError(t, err, "output: %s", output)
	require.True(t, strings.Contains(string(output), "NO_GATE_COMMAND_EXECUTED"), "output: %s", output)
}

func TestRunPack_BootReadiness_Success_LogOrder(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(workDir+"/wippy.yaml", []byte("version: \"1.0\"\nshutdown:\n  timeout: 5s\n"), 0o600))
	createTestPackFile(t, workDir, "app", []wapp.Entry{
		{
			ID:   wapp.NewID("app", "terminal"),
			Kind: "terminal.host",
			Data: map[string]any{
				"lifecycle": map[string]any{
					"auto_start": true,
				},
			},
		},
		{
			ID:   wapp.NewID("app", "workers"),
			Kind: "process.host",
			Data: map[string]any{
				"host": map[string]any{
					"workers": 2,
				},
				"lifecycle": map[string]any{
					"auto_start": true,
				},
			},
		},
		{
			ID:   wapp.NewID("app", "boot_proc"),
			Kind: "process.lua",
			Data: map[string]any{
				"method":  "main",
				"modules": []string{"time"},
				"source": `local time = require("time")
return {main = function()
  time.after("150ms"):receive()
  print("BOOTLOADER_FINISHED")
end}`,
			},
		},
		{
			ID:   wapp.NewID("app", "boot_service"),
			Kind: "process.service",
			Data: map[string]any{
				"process": "app:boot_proc",
				"host":    "app:workers",
				"lifecycle": map[string]any{
					"auto_start": true,
					"boot_gate":  true,
				},
			},
		},
		{
			ID:   wapp.NewID("app", "command"),
			Kind: "process.lua",
			Meta: map[string]any{
				"command": map[string]any{
					"name": "command",
					"main": true,
				},
			},
			Data: map[string]any{
				"method": "main",
				"source": `return {main = function()
  print("COMMAND_EXECUTED")
end}`,
			},
		},
	})

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRunPackBootReadinessOkHelper$")
	child.Dir = workDir
	child.Env = append(os.Environ(), "WIPPY_PACK_BOOT_READINESS_OK_HELPER=1")
	output, err := child.CombinedOutput()
	require.NoError(t, err, "output: %s", output)

	outStr := string(output)
	bootIdx := strings.Index(outStr, "BOOTLOADER_FINISHED")
	cmdIdx := strings.Index(outStr, "COMMAND_EXECUTED")
	require.GreaterOrEqual(t, bootIdx, 0, "BOOTLOADER_FINISHED missing in output: %s", outStr)
	require.GreaterOrEqual(t, cmdIdx, 0, "COMMAND_EXECUTED missing in output: %s", outStr)
	require.Less(t, bootIdx, cmdIdx, "BOOTLOADER_FINISHED must appear BEFORE COMMAND_EXECUTED in output: %s", outStr)
}

func TestRunPack_BootReadiness_Fail_AbortsCommand(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(workDir+"/wippy.yaml", []byte("version: \"1.0\"\nshutdown:\n  timeout: 5s\n"), 0o600))
	createTestPackFile(t, workDir, "app", []wapp.Entry{
		{
			ID:   wapp.NewID("app", "terminal"),
			Kind: "terminal.host",
			Data: map[string]any{
				"lifecycle": map[string]any{
					"auto_start": true,
				},
			},
		},
		{
			ID:   wapp.NewID("app", "workers"),
			Kind: "process.host",
			Data: map[string]any{
				"host": map[string]any{
					"workers": 2,
				},
				"lifecycle": map[string]any{
					"auto_start": true,
				},
			},
		},
		{
			ID:   wapp.NewID("app", "boot_proc"),
			Kind: "process.lua",
			Data: map[string]any{
				"method":  "main",
				"modules": []string{"time"},
				"source": `return {main = function()
  error("deliberate migration failure")
end}`,
			},
		},
		{
			ID:   wapp.NewID("app", "boot_service"),
			Kind: "process.service",
			Data: map[string]any{
				"process": "app:boot_proc",
				"host":    "app:workers",
				"lifecycle": map[string]any{
					"auto_start": true,
					"boot_gate":  true,
					"restart": map[string]any{
						"initial_delay": "50ms",
					},
				},
			},
		},
		{
			ID:   wapp.NewID("app", "command"),
			Kind: "process.lua",
			Meta: map[string]any{
				"command": map[string]any{
					"name": "command",
					"main": true,
				},
			},
			Data: map[string]any{
				"method": "main",
				"source": `return {main = function()
  print("COMMAND_SHOULD_NEVER_RUN")
end}`,
			},
		},
	})

	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRunPackBootReadinessFailHelper$")
	child.Dir = workDir
	child.Env = append(os.Environ(), "WIPPY_PACK_BOOT_READINESS_FAIL_HELPER=1")
	output, err := child.CombinedOutput()
	var exitErr *exec.ExitError
	require.True(t, errors.As(err, &exitErr), "expected non-zero exit, got err=%v output: %s", err, output)
	require.Equal(t, 1, exitErr.ExitCode(), "output: %s", output)

	outStr := string(output)
	require.False(t, strings.Contains(outStr, "COMMAND_SHOULD_NEVER_RUN"), "command entrypoint must NOT have executed: %s", outStr)
	require.True(t, strings.Contains(outStr, "deliberate migration failure") || strings.Contains(outStr, "boot gate \"app:boot_service\" failed"), "output should mention boot failure: %s", outStr)
}

func TestRunPack_BootReadiness_NoGatingServicesUnchanged(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(workDir+"/wippy.yaml", []byte("version: \"1.0\"\nshutdown:\n  timeout: 5s\n"), 0o600))
	createTestPackFile(t, workDir, "app", []wapp.Entry{
		{
			ID:   wapp.NewID("app", "terminal"),
			Kind: "terminal.host",
			Data: map[string]any{
				"lifecycle": map[string]any{
					"auto_start": true,
				},
			},
		},
		{
			ID:   wapp.NewID("app", "command"),
			Kind: "process.lua",
			Meta: map[string]any{
				"command": map[string]any{
					"name": "command",
					"main": true,
				},
			},
			Data: map[string]any{
				"method": "main",
				"source": `return {main = function()
  print("NO_GATE_COMMAND_EXECUTED")
end}`,
			},
		},
	})

	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRunPackBootReadinessOkHelper$")
	child.Dir = workDir
	child.Env = append(os.Environ(), "WIPPY_PACK_BOOT_READINESS_OK_HELPER=1")
	output, err := child.CombinedOutput()
	require.NoError(t, err, "output: %s", output)
	require.True(t, strings.Contains(string(output), "NO_GATE_COMMAND_EXECUTED"), "output: %s", output)
}
