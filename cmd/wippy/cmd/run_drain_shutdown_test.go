// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// A process drained by its host during runtime shutdown keeps receiving relay
// deliveries: its timer fires on CANCEL, it returns, and the host drains long
// before the shutdown deadline.
func TestShutdownDrainDeliversTimerToCancelledProcess(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.Mkdir("src", 0o755))
	require.NoError(t, os.WriteFile("wippy.lock", []byte("directories:\n  src: ./src\n  modules: .wippy\n"), 0o600))
	require.NoError(t, os.WriteFile("wippy.yaml", []byte("version: \"1.0\"\nshutdown:\n  timeout: 8s\n"), 0o600))
	require.NoError(t, os.WriteFile("src/_index.yaml", []byte(`version: "1.0"
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
  - name: drainer
    kind: process.lua
    method: main
    modules: [process, time]
    source: |
      local process = require("process")
      local time = require("time")
      return {main = function(parent)
        local events = process.events()
        process.send(parent, "ready", true)
        while true do
          local event = events:receive()
          if event.kind == process.event.CANCEL then
            time.after("100ms"):receive()
            return 0
          end
        end
      end}
  - name: spawner
    kind: security.policy
    policy:
      actions: "*"
      resources: "*"
      effect: allow
  - name: main
    kind: process.lua
    method: main
    modules: [process]
    meta:
      command:
        name: drain-probe
        security:
          actor: {id: app:main}
          policies: [app:spawner]
    source: |
      local process = require("process")
      return {main = function()
        local ready = process.listen("ready")
        local drainer, err = process.spawn("app:drainer", "app:workers", process.pid())
        if not drainer then error(tostring(err)) end
        ready:receive()
      end}
`), 0o600))
	setTestConfigFiles(t, "wippy.yaml")
	oldSilent := silentLogs
	t.Cleanup(func() { silentLogs = oldSilent })
	cmd := &cobra.Command{}
	cmd.Flags().String("exec", "", "")
	cmd.Flags().String("host", "", "")
	cmd.Flags().String("registry", "", "")
	require.NoError(t, cmd.ParseFlags([]string{"--exec", "app:main"}))
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd.SetContext(ctx)

	started := time.Now()
	require.NoError(t, runWithUseCase(cmd, cmd.Flags().Args(), defaultUseCase))
	require.Less(t, time.Since(started), 4*time.Second,
		"host drain waited for the shutdown deadline instead of the cancelled process's timer")
}
