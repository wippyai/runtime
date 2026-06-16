// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	event "github.com/wippyai/runtime/api/event"
	regapi "github.com/wippyai/runtime/api/registry"
	supervisorapi "github.com/wippyai/runtime/api/supervisor"
	supervisorpkg "github.com/wippyai/runtime/system/supervisor"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type fakeShutdownService struct {
	updates chan any
	mu      sync.Mutex
	stopped bool
}

func (s *fakeShutdownService) Start(_ context.Context) (<-chan any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updates = make(chan any, 1)
	return s.updates, nil
}

func (s *fakeShutdownService) Stop(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.updates != nil {
		close(s.updates)
		s.updates = nil
	}
	s.stopped = true
	return nil
}

func (s *fakeShutdownService) isStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

func TestRunPackEntries_GracefulShutdownStopsRunningService(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "wippy.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: \"1.0\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	prevConfigFile := configFile
	configFile = cfgPath
	t.Cleanup(func() { configFile = prevConfigFile })

	core, logs := observer.New(zapcore.DebugLevel)
	baseLogger := zap.New(core)

	ctx, loader, _, embedReg, err := bootstrapPackRuntime(nil, baseLogger)
	if err != nil {
		t.Fatalf("bootstrap pack runtime: %v", err)
	}
	t.Cleanup(func() { _ = embedReg.Close() })

	const serviceID = "test.shutdown:service"
	svc := &fakeShutdownService{}

	done := make(chan error, 1)
	go func() {
		done <- runPackEntries(ctx, loader, baseLogger, nil, nil, defaultUseCase)
	}()

	waitForLogMessage(t, logs, "supervisor started")

	sup, ok := supervisorapi.GetSupervisor(ctx).(*supervisorpkg.Supervisor)
	if !ok || sup == nil {
		t.Fatal("supervisor not available")
	}

	bus := event.GetBus(ctx)
	if bus == nil {
		t.Fatal("event bus not available")
	}

	bus.Send(ctx, event.Event{System: regapi.System, Kind: regapi.TxBegin})
	bus.Send(ctx, event.Event{
		System: supervisorapi.System,
		Kind:   supervisorapi.ServiceRegister,
		Path:   serviceID,
		Data: &supervisorapi.Entry{
			Service: svc,
			Config: supervisorapi.LifecycleConfig{
				AutoStart:    true,
				StartTimeout: 5 * time.Second,
				StopTimeout:  5 * time.Second,
				RetryPolicy:  supervisorapi.RetryPolicy{MaxAttempts: 3, InitialDelay: 50 * time.Millisecond},
			},
		},
	})
	bus.Send(ctx, event.Event{System: regapi.System, Kind: regapi.TxCommit})

	waitForServiceStatus(t, sup, serviceID, supervisorapi.StatusRunning)

	supervisorapi.TriggerShutdown(ctx, 0)

	select {
	case runErr := <-done:
		if runErr != nil {
			t.Fatalf("runPackEntries returned error: %v", runErr)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for runPackEntries to return")
	}

	if !svc.isStopped() {
		t.Fatal("service was not gracefully stopped during shutdown")
	}

	for _, entry := range logs.All() {
		errField, _ := entry.ContextMap()["error"].(string)
		if strings.Contains(entry.Message, "failed to stop controllers during shutdown") ||
			strings.Contains(entry.Message, "supervisor is stopped") ||
			strings.Contains(errField, "supervisor is stopped") {
			t.Fatalf("graceful shutdown produced a supervisor-stopped error: message=%q error=%q", entry.Message, errField)
		}
	}
}

func TestWaitForShutdownSignal_InvokesOnFirstSignalOnlyWhenSet(t *testing.T) {
	t.Run("invokes callback when set", func(t *testing.T) {
		sigChan := make(chan os.Signal, 1)
		sigChan <- syscall.SIGTERM

		called := make(chan struct{}, 1)
		waitForShutdownSignal(sigChan, zap.NewNop(), func() { called <- struct{}{} })

		select {
		case <-called:
		case <-time.After(time.Second):
			t.Fatal("onFirstSignal was not invoked")
		}
	})

	t.Run("tolerates a nil callback", func(t *testing.T) {
		sigChan := make(chan os.Signal, 1)
		sigChan <- syscall.SIGTERM

		waitForShutdownSignal(sigChan, zap.NewNop(), nil)
	})
}

func waitForLogMessage(t *testing.T, logs *observer.ObservedLogs, message string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, entry := range logs.All() {
			if entry.Message == message {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timeout waiting for log message %q", message)
}

func waitForServiceStatus(t *testing.T, sup *supervisorpkg.Supervisor, serviceID string, status supervisorapi.Status) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		state, err := sup.GetState(serviceID)
		if err == nil && state.Status == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timeout waiting for service %q to reach status %q", serviceID, status)
}
