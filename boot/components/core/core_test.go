// SPDX-License-Identifier: MPL-2.0

package core

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	supervisorapi "github.com/wippyai/runtime/api/supervisor"
	bootpkg "github.com/wippyai/runtime/boot"
	"go.uber.org/zap"
)

type coreManagedService struct {
	started  chan struct{}
	stopped  chan struct{}
	updates  chan any
	startOne sync.Once
	stopOne  sync.Once
}

func newCoreManagedService() *coreManagedService {
	return &coreManagedService{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
		updates: make(chan any),
	}
}

func (s *coreManagedService) Start(context.Context) (<-chan any, error) {
	s.startOne.Do(func() { close(s.started) })
	return s.updates, nil
}

func (s *coreManagedService) Stop(context.Context) error {
	s.stopOne.Do(func() {
		close(s.updates)
		close(s.stopped)
	})
	return nil
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

	runtimeCtx, cancelRuntime := context.WithCancel(ctx)
	if err := loader.Start(runtimeCtx); err != nil {
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
				StopTimeout:  time.Second,
			},
		},
	})
	bus.Send(runtimeCtx, event.Event{System: registry.System, Kind: registry.TxCommit})
	select {
	case <-service.started:
	case <-time.After(time.Second):
		t.Fatal("managed service did not start")
	}

	cancelRuntime()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	if err := loader.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	select {
	case <-service.stopped:
	default:
		t.Fatal("managed service was not stopped with the shutdown context")
	}
}
