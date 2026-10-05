// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
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
	ready := make(chan struct{}, 1)
	readyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case ready <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(readyServer.Close)
	require.NoError(t, os.Mkdir("src", 0o755))
	require.NoError(t, os.WriteFile("wippy.lock", []byte("directories:\n  src: ./src\n  modules: .wippy\n"), 0o600))
	require.NoError(t, os.WriteFile("wippy.yaml", []byte("version: \"1.0\"\nshutdown:\n  timeout: 20s\n"), 0o600))
	require.NoError(t, os.WriteFile("src/_index.yaml", []byte(`version: "1.0"
namespace: app
entries:
  - name: workers
    kind: process.host
    host:
      workers: 2
    lifecycle:
      auto_start: true
  - name: worker
    kind: process.lua
    method: main
    modules: [process, http_client]
    source: |
      local process = require("process")
      local http = require("http_client")
      return {main = function()
        local events = process.events()
        local response, err = http.get("`+readyServer.URL+`")
        if err then error(err) end
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
	finished := make(chan struct{})
	go func() {
		result <- runWithUseCase(cmd, cmd.Flags().Args(), defaultUseCase)
		close(finished)
	}()
	t.Cleanup(func() {
		cancelCaller()
		select {
		case <-finished:
		case <-time.After(25 * time.Second):
			t.Error("run did not stop during test cleanup")
		}
	})

	select {
	case <-ready:
	case err := <-result:
		t.Fatalf("run returned before service readiness: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("service never started")
	}

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

func TestWaitForShutdownStartsShutdownOnCallerCancellation(t *testing.T) {
	caller, cancel := context.WithCancel(context.Background())
	runtime, err := detachFromCaller(caller)
	require.NoError(t, err)
	runtime, stop := context.WithCancel(runtime)
	defer stop()

	done := make(chan struct{})
	go func() {
		waitForShutdown(runtime, newTestShutdownSources(), zap.NewNop(), nil)
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
