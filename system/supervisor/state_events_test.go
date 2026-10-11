// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	api "github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

func TestSupervisorStateEventMatchesControllerSnapshot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus := eventbus.NewBus()
	defer bus.Stop()
	events := make(chan event.Event, 8)
	id, err := bus.SubscribeP(ctx, api.System, api.ServiceUpdate, events)
	require.NoError(t, err)
	defer bus.Unsubscribe(context.Background(), id)
	s := NewSupervisor(bus, zap.NewNop())
	s.ctx = ctx
	c := &Controller{state: newInternalState()}
	c.state.setDesiredStatus(api.StatusRunning)
	c.state.updateState(api.StatusRunning, nil)
	c.state.incRetryCount()
	c.state.incRetryCount()
	c.onStateChange = s.createStateHandler("test:svc")
	c.updateState(api.StatusFailed, "retryable failure")
	want := c.State()
	select {
	case e := <-events:
		require.Equal(t, "test:svc", e.Path)
		require.Equal(t, want, e.Data)
	case <-time.After(time.Second):
		t.Fatal("state event not delivered")
	}
}

func TestInternalStateTransitionSnapshotsAreCoherent(t *testing.T) {
	s := newInternalState()
	s.updateState(api.StatusRunning, api.StatusRunning)
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		for range 10000 {
			s.updateState(api.StatusFailed, api.StatusFailed)
			s.updateState(api.StatusRunning, api.StatusRunning)
		}
	}()
	for range 10000 {
		state := s.publicState()
		if state.Status != state.Details {
			t.Errorf("partial transition: status=%v details=%v", state.Status, state.Details)
			break
		}
	}
	writer.Wait()
	want := s.updateState(api.StatusFailed, "captured")
	s.updateState(api.StatusRunning, "later")
	require.Equal(t, api.StatusFailed, want.Status)
	require.Equal(t, "captured", want.Details)
}

func TestSupervisorDetailsEventUsesCompleteState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus := eventbus.NewBus()
	defer bus.Stop()
	events := make(chan event.Event, 16)
	id, err := bus.SubscribeP(ctx, api.System, api.ServiceUpdate, events)
	require.NoError(t, err)
	defer bus.Unsubscribe(context.Background(), id)
	s := NewSupervisor(bus, zap.NewNop())
	s.ctx = ctx
	details := make(chan any, 1)
	c := newController(ctx, &mockService{
		startFunc: func(context.Context) (<-chan any, error) { return details, nil },
		stopFunc:  func(context.Context) error { close(details); return nil },
	}, api.LifecycleConfig{StartTimeout: time.Second, StopTimeout: time.Second}, s.createStateHandler("test:health"))
	defer c.close()
	require.NoError(t, c.Start())
	details <- "healthy"
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for {
		select {
		case e := <-events:
			state := e.Data.(State)
			if state.Details != "healthy" {
				continue
			}
			require.Equal(t, c.State(), state)
			require.Equal(t, api.StatusRunning, state.Desired)
			require.False(t, state.StartedAt.IsZero())
			require.NoError(t, c.Stop())
			return
		case <-deadline.C:
			t.Fatal("details event not delivered")
		}
	}
}
