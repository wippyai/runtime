// SPDX-License-Identifier: MPL-2.0

package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	supervisorapi "github.com/wippyai/runtime/api/supervisor"
	bootpkg "github.com/wippyai/runtime/boot"
	"go.uber.org/zap"
)

type coreManagedService struct {
	started      chan struct{}
	stopped      chan struct{}
	stopDeadline chan time.Time
	updates      chan any
	startOne     sync.Once
	stopOne      sync.Once
}

func newCoreManagedService() *coreManagedService {
	return &coreManagedService{
		started:      make(chan struct{}),
		stopped:      make(chan struct{}),
		stopDeadline: make(chan time.Time, 1),
		updates:      make(chan any),
	}
}

func (s *coreManagedService) Start(context.Context) (<-chan any, error) {
	s.startOne.Do(func() { close(s.started) })
	return s.updates, nil
}

func (s *coreManagedService) Stop(ctx context.Context) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		return errors.New("managed stop context has no deadline")
	}
	s.stopDeadline <- deadline
	<-ctx.Done()
	s.stopOne.Do(func() {
		close(s.updates)
		close(s.stopped)
	})
	return ctx.Err()
}

func TestCorePlugins(t *testing.T) {
	logger := zap.NewExample()
	ctx, err := bootpkg.NewBootstrapContext(logger, nil)
	if err != nil {
		t.Fatalf("NewBootstrapContext() error = %v", err)
	}

	loader, err := bootpkg.NewLoader(All()...)
	if err != nil {
		t.Fatalf("NewLoader() error = %v", err)
	}

	ctx, err = loader.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if logapi.GetLogger(ctx) == nil {
		t.Error("logger not available in context")
	}

	if event.GetBus(ctx) == nil {
		t.Error("event bus not available in context")
	}

	if process.GetPIDGenerator(ctx) == nil {
		t.Error("PID generator not available in context")
	}

	lifecycleLoader, err := bootpkg.NewLoader(Registry(), Supervisor())
	require.NoError(t, err)
	ctx, err = lifecycleLoader.Load(ctx)
	require.NoError(t, err)
	runtimeCtx, cancelRuntime := context.WithCancel(ctx)
	defer cancelRuntime()
	if err := lifecycleLoader.Start(runtimeCtx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	service := newCoreManagedService()
	bus := event.GetBus(ctx)
	bus.Send(runtimeCtx, event.Event{System: registry.System, Kind: registry.TxBegin})
	bus.Send(runtimeCtx, event.Event{
		System: supervisorapi.System,
		Kind:   supervisorapi.ServiceRegister,
		Path:   "test:shutdown",
		Data: &supervisorapi.Entry{
			Service: service,
			Config: supervisorapi.LifecycleConfig{
				AutoStart:    true,
				StartTimeout: time.Second,
				StopTimeout:  500 * time.Millisecond,
			},
		},
	})
	bus.Send(runtimeCtx, event.Event{System: registry.System, Kind: registry.TxCommit})
	select {
	case <-service.started:
	case <-time.After(time.Second):
		t.Fatal("managed service did not start")
	}
	barrier := make(chan event.Event, 1)
	_, err = bus.SubscribeP(runtimeCtx, "test", "barrier", barrier)
	require.NoError(t, err)
	bus.Send(runtimeCtx, event.Event{System: "test", Kind: "barrier"})
	select {
	case <-barrier:
	case <-time.After(time.Second):
		t.Fatal("event barrier did not complete")
	}
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelShutdown()
	shutdownDeadline, _ := shutdownCtx.Deadline()
	shutdownStarted := time.Now()
	if err := lifecycleLoader.Shutdown(shutdownCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown() error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(shutdownStarted); elapsed > 250*time.Millisecond {
		t.Fatalf("Shutdown() elapsed = %v, want shutdown context to bound service stop", elapsed)
	}
	stopDeadline := <-service.stopDeadline
	if delta := stopDeadline.Sub(shutdownDeadline); delta < -time.Millisecond || delta > time.Millisecond {
		t.Fatalf("managed stop deadline = %v, want shutdown deadline %v", stopDeadline, shutdownDeadline)
	}
	select {
	case <-service.stopped:
	default:
		t.Fatal("managed service did not observe shutdown cancellation")
	}
}
