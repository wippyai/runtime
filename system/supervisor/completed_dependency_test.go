// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

type dependencyGateService struct {
	completed     chan struct{}
	completionErr error
	terminal      error
	runs          atomic.Int32
}

func (s *dependencyGateService) Start(ctx context.Context) (<-chan any, error) {
	s.runs.Add(1)
	status := make(chan any, 1)
	go func() {
		defer close(status)
		select {
		case <-s.completed:
			status <- s.terminal
		case <-ctx.Done():
		}
	}()
	return status, nil
}

func (*dependencyGateService) Stop(context.Context) error { return nil }

func (s *dependencyGateService) WaitCompletion(ctx context.Context) error {
	select {
	case <-s.completed:
		return s.completionErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestSupervisorCompletedDependencySurvivesDependentRestart(t *testing.T) {
	bus := eventbus.NewBus()
	defer bus.Stop()
	sup := NewSupervisor(bus, zap.NewNop())
	require.NoError(t, sup.Start(t.Context()))
	defer func() { require.NoError(t, sup.Stop()) }()

	gate := &dependencyGateService{completed: make(chan struct{}), terminal: supervisor.ErrExit}
	var dependentRuns atomic.Int32
	dependent := &mockService{
		startFunc: func(context.Context) (<-chan any, error) {
			dependentRuns.Add(1)
			return make(chan any), nil
		},
		stopFunc: func(context.Context) error { return nil },
	}
	tx := newRegTx(zap.NewNop())
	tx.register["gate"] = &supervisor.Entry{Service: gate, Config: supervisor.LifecycleConfig{
		Startup: supervisor.StartupComplete, StartTimeout: time.Second, StopTimeout: time.Second,
	}}
	tx.register["dependent"] = &supervisor.Entry{Service: dependent, Config: supervisor.LifecycleConfig{
		AutoStart: true, Requires: []string{"gate"}, StartTimeout: time.Second, StopTimeout: time.Second,
	}}
	booted := make(chan error, 1)
	go func() { booted <- sup.execute(t.Context(), tx) }()
	require.Eventually(t, func() bool { return gate.runs.Load() == 1 }, time.Second, time.Millisecond)
	require.Zero(t, dependentRuns.Load(), "dependent must wait for gate completion")
	close(gate.completed)
	require.NoError(t, <-booted)
	require.Equal(t, int32(1), dependentRuns.Load())
	require.Eventually(t, func() bool {
		state, err := sup.GetState("gate")
		return err == nil && state.Status == supervisor.StatusExited && state.Details == nil
	}, time.Second, time.Millisecond)

	sup.handleEvent(event.Event{System: supervisor.System, Kind: supervisor.ServiceStop, Path: "dependent"})
	require.Eventually(t, func() bool {
		state, err := sup.GetState("dependent")
		return err == nil && state.Status == supervisor.StatusStopped
	}, time.Second, time.Millisecond)
	sup.handleEvent(event.Event{System: supervisor.System, Kind: supervisor.ServiceStart, Path: "dependent"})
	require.Eventually(t, func() bool {
		state, err := sup.GetState("dependent")
		return err == nil && state.Status == supervisor.StatusRunning && dependentRuns.Load() == 2
	}, time.Second, time.Millisecond)
	require.Equal(t, int32(1), gate.runs.Load(), "dependent restart must reuse the completed gate")

	// An explicit start still runs the gate again.
	sup.handleEvent(event.Event{System: supervisor.System, Kind: supervisor.ServiceStart, Path: "gate"})
	require.Eventually(t, func() bool { return gate.runs.Load() == 2 }, time.Second, time.Millisecond)
	require.Eventually(t, func() bool {
		state, err := sup.GetState("gate")
		return err == nil && state.Status == supervisor.StatusExited
	}, time.Second, time.Millisecond)

	// A replacement has its own completion lifetime and must run once.
	replacement := &dependencyGateService{completed: make(chan struct{}), terminal: supervisor.ErrExit}
	close(replacement.completed)
	update := newRegTx(zap.NewNop())
	update.register["gate"] = &supervisor.Entry{Service: replacement, Config: tx.register["gate"].Config}
	require.NoError(t, sup.execute(t.Context(), update))
	require.Equal(t, int32(1), replacement.runs.Load())
	require.Equal(t, int32(3), dependentRuns.Load())
}

func TestSupervisorFailedCompletionBlocksDependent(t *testing.T) {
	failure := errors.New("migration failed")
	for _, terminal := range []error{failure, supervisor.ErrExit} {
		t.Run(terminal.Error(), func(t *testing.T) {
			bus := eventbus.NewBus()
			defer bus.Stop()
			sup := NewSupervisor(bus, zap.NewNop())
			require.NoError(t, sup.Start(t.Context()))
			defer func() { require.NoError(t, sup.Stop()) }()
			gate := &dependencyGateService{
				completed: make(chan struct{}), completionErr: failure, terminal: terminal,
			}
			close(gate.completed)
			dependent := newTestService()
			tx := newRegTx(zap.NewNop())
			tx.register["gate"] = &supervisor.Entry{Service: gate, Config: supervisor.LifecycleConfig{
				Startup: supervisor.StartupComplete, StartTimeout: time.Second, StopTimeout: time.Second,
				RetryPolicy: supervisor.RetryPolicy{MaxAttempts: 1},
			}}
			tx.register["dependent"] = &supervisor.Entry{Service: dependent, Config: supervisor.LifecycleConfig{
				AutoStart: true, Startup: supervisor.StartupRequired, Requires: []string{"gate"},
				StartTimeout: time.Second, StopTimeout: time.Second,
			}}
			require.ErrorIs(t, sup.execute(t.Context(), tx), failure)
			require.False(t, dependent.IsStarted())
			require.Eventually(t, func() bool {
				state, err := sup.GetState("gate")
				return err == nil && (state.Status == supervisor.StatusFailed || state.Status == supervisor.StatusExited)
			}, time.Second, time.Millisecond)

			// A later dependent start must still observe the failed completion,
			// including services that emit ErrExit when no return result exists.
			ops, err := sup.buildStartOperations(sup.snapshotControllers(), "dependent")
			require.NoError(t, err)
			require.ErrorIs(t, sup.executeOperations(t.Context(), ops), failure)
			require.False(t, dependent.IsStarted())
		})
	}
}

type rerunGateService struct {
	failure error
	runs    atomic.Int32
}

func (s *rerunGateService) Start(ctx context.Context) (<-chan any, error) {
	s.runs.Add(1)
	status := make(chan any, 1)
	status <- supervisor.ErrExit
	close(status)
	return status, nil
}

func (*rerunGateService) Stop(context.Context) error { return nil }

func (s *rerunGateService) WaitCompletion(context.Context) error {
	if s.runs.Load() > 1 {
		return s.failure
	}
	return nil
}

func TestSupervisorFailedRerunBlocksDependent(t *testing.T) {
	bus := eventbus.NewBus()
	defer bus.Stop()
	sup := NewSupervisor(bus, zap.NewNop())
	require.NoError(t, sup.Start(t.Context()))
	defer func() { require.NoError(t, sup.Stop()) }()

	failure := errors.New("second run failed")
	gate := &rerunGateService{failure: failure}
	dependent := newTestService()
	tx := newRegTx(zap.NewNop())
	tx.register["gate"] = &supervisor.Entry{Service: gate, Config: supervisor.LifecycleConfig{
		AutoStart: true, Startup: supervisor.StartupComplete, StartTimeout: time.Second, StopTimeout: time.Second,
		RetryPolicy: supervisor.RetryPolicy{MaxAttempts: 1},
	}}
	require.NoError(t, sup.execute(t.Context(), tx))
	require.Equal(t, int32(1), gate.runs.Load())

	// Completion and terminal status arrive independently. Starting a service
	// still marked running is a no-op, so observe the first exit before rerunning.
	require.Eventually(t, func() bool {
		state, err := sup.GetState("gate")
		return err == nil && state.Status == supervisor.StatusExited
	}, time.Second, time.Millisecond)
	sup.handleEvent(event.Event{System: supervisor.System, Kind: supervisor.ServiceStart, Path: "gate"})
	require.Eventually(t, func() bool {
		state, err := sup.GetState("gate")
		return err == nil && gate.runs.Load() == 2 && state.Status != supervisor.StatusRunning
	}, time.Second, time.Millisecond)

	register := newRegTx(zap.NewNop())
	register.register["dependent"] = &supervisor.Entry{Service: dependent, Config: supervisor.LifecycleConfig{
		Startup: supervisor.StartupRequired, Requires: []string{"gate"},
		StartTimeout: time.Second, StopTimeout: time.Second,
	}}
	require.NoError(t, sup.execute(t.Context(), register))
	ops, err := sup.buildStartOperations(sup.snapshotControllers(), "dependent")
	require.NoError(t, err)
	require.ErrorIs(t, sup.executeOperations(t.Context(), ops), failure)
	require.False(t, dependent.IsStarted())
}
