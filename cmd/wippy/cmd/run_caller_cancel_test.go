// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// The run command owns the runtime lifetime: canceling the context its caller
// supplied starts the orderly shutdown instead of tearing down the control
// mailbox that delivers child exits to supervised process services. Each
// service stops on its cancel event, long before its stop timeout.
func TestRunCallerCancellationShutsDownGracefully(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)
	marks := filepath.Join(workDir, "marks")
	require.NoError(t, os.Mkdir("src", 0o755))
	require.NoError(t, os.WriteFile("wippy.lock", []byte("directories:\n  src: ./src\n  modules: .wippy\n"), 0o600))
	require.NoError(t, os.WriteFile("wippy.yaml", []byte("version: \"1.0\"\nshutdown:\n  timeout: 20s\n"), 0o600))
	require.NoError(t, os.WriteFile("src/_index.yaml", []byte(`version: "1.0"
namespace: app
entries:
  - name: marks
    kind: fs.directory
    directory: `+marks+`
    auto_init: true
    lifecycle:
      auto_start: true
  - name: workers
    kind: process.host
    host:
      workers: 2
    lifecycle:
      auto_start: true
  - name: worker
    kind: process.lua
    method: main
    modules: [process, fs]
    source: |
      local process = require("process")
      local fs = require("fs")
      return {main = function()
        local events = process.events()
        fs.get("app:marks"):writefile("ready", "1")
        while true do
          local event = events:receive()
          if event.kind == process.event.CANCEL then
            return 0
          end
        end
      end}
  - name: allow
    kind: security.policy
    policy:
      actions: "*"
      resources: "*"
      effect: allow
  - name: service
    kind: process.service
    process: app:worker
    host: app:workers
    lifecycle:
      auto_start: true
      stop_timeout: 15s
      security:
        actor: {id: app:service}
        policies: [app:allow]
`), 0o600))
	setTestConfigFiles(t, "wippy.yaml")
	oldSilent := silentLogs
	t.Cleanup(func() { silentLogs = oldSilent })

	cmd := &cobra.Command{}
	cmd.Flags().String("exec", "", "")
	cmd.Flags().String("host", "", "")
	cmd.Flags().String("registry", "", "")
	require.NoError(t, cmd.ParseFlags(nil))
	caller, cancelCaller := context.WithCancel(t.Context())
	defer cancelCaller()
	cmd.SetContext(caller)

	result := make(chan error, 1)
	go func() { result <- runWithUseCase(cmd, cmd.Flags().Args(), defaultUseCase) }()

	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(marks, "ready"))
		return err == nil
	}, 10*time.Second, 10*time.Millisecond, "service never started")

	started := time.Now()
	cancelCaller()
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(25 * time.Second):
		t.Fatal("run did not return after caller cancellation")
	}
	require.Less(t, time.Since(started), 5*time.Second,
		"service waited for its stop timeout instead of receiving its exit event")
}

func TestDetachFromCallerKeepsRuntimeAliveAndReportsCancellation(t *testing.T) {
	caller, cancel := context.WithCancel(context.Background())
	runtime, err := detachFromCaller(caller)
	require.NoError(t, err)

	cancel()
	<-callerCancellation(runtime)
	require.NoError(t, runtime.Err(), "caller cancellation reached the runtime context")
}

func TestDetachFromCallerRejectsUnavailableCaller(t *testing.T) {
	_, err := detachFromCaller(nil)
	require.Error(t, err)

	caller, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = detachFromCaller(caller)
	require.ErrorIs(t, err, context.Canceled)
}

func TestWaitForShutdownSignalStartsShutdownOnCallerCancellation(t *testing.T) {
	caller, cancel := context.WithCancel(context.Background())
	runtime, err := detachFromCaller(caller)
	require.NoError(t, err)
	runtime, stop := context.WithCancel(runtime)
	defer stop()

	done := make(chan struct{})
	go func() {
		waitForShutdownSignal(runtime, make(chan os.Signal, 1), zap.NewNop(), nil)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown waiter ignored caller cancellation")
	}
	require.NoError(t, runtime.Err(), "waiting for shutdown cancelled the runtime")
}
