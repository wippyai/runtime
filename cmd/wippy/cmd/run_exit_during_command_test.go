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
)

func TestRunExitDuringCommandHelper(t *testing.T) {
	if os.Getenv("WIPPY_EXIT_DURING_COMMAND_HELPER") != "1" {
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
	require.NoError(t, runWithUseCase(cmd, cmd.Flags().Args(), defaultUseCase))
}

func TestRunExitDuringCommandCancelsCommandAndReturnsRequestedCode(t *testing.T) {
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
  - name: exit_policy
    kind: security.policy
    policy:
      actions: "system.exit"
      resources: "*"
      effect: allow
  - name: exiter
    kind: process.lua
    method: main
    modules: [system, time]
    security:
      actor: {id: app:exiter}
      policies: [app:exit_policy]
    source: |
      local system = require("system")
      local time = require("time")
      return {main = function()
        time.after("750ms"):receive()
        local ok, err = system.exit(3)
        if not ok then error(tostring(err)) end
        print("EXIT_REQUESTED")
      end}
  - name: exit_service
    kind: process.service
    process: app:exiter
    host: app:workers
    lifecycle:
      auto_start: true
  - name: command
    kind: process.lua
    method: main
    modules: [process]
    source: |
      local process = require("process")
      return {main = function()
        print("COMMAND_STARTED")
        local events = process.events()
        while true do
          local event = events:receive()
          if event.kind == process.event.CANCEL then
            print("COMMAND_CANCELLED")
            return 0
          end
        end
      end}
`), 0o600))

	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRunExitDuringCommandHelper$")
	child.Dir = workDir
	child.Env = append(os.Environ(), "WIPPY_EXIT_DURING_COMMAND_HELPER=1")
	started := time.Now()
	output, err := child.CombinedOutput()
	require.NoError(t, ctx.Err(), "run did not stop promptly: %s", output)
	require.Less(t, time.Since(started), 4*time.Second, "run waited for the command or shutdown deadline: %s", output)
	var exitErr *exec.ExitError
	require.True(t, errors.As(err, &exitErr), "run error = %v, output: %s", err, output)
	require.Equal(t, 3, exitErr.ExitCode(), "output: %s", output)
	require.True(t, strings.Contains(string(output), "COMMAND_STARTED"), "command did not start: %s", output)
	require.True(t, strings.Contains(string(output), "EXIT_REQUESTED"), "service did not request exit: %s", output)
	require.True(t, strings.Contains(string(output), "COMMAND_CANCELLED"), "command was not cancelled: %s", output)
	serviceStop := strings.Index(string(output), "stopping service\t{\"service_id\": \"app:exit_service\"")
	workersStop := strings.Index(string(output), "stopping service\t{\"service_id\": \"app:workers\"")
	require.GreaterOrEqual(t, serviceStop, 0, "exit service was not stopped: %s", output)
	require.Greater(t, workersStop, serviceStop, "services stopped out of order: %s", output)
}
