// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	apisupervisor "github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestY03SupervisorStopIdempotent(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	bus := eventbus.NewBus()
	defer bus.Stop()
	sup := NewSupervisor(bus, zap.New(core))
	startCtx, cancelStart := context.WithCancel(context.Background())
	require.NoError(t, sup.Start(startCtx))

	service := newTestService()
	tx := newRegTx(zap.NewNop())
	tx.open = true
	tx.register["managed"] = &apisupervisor.Entry{
		Service: service,
		Config: apisupervisor.LifecycleConfig{
			AutoStart:    true,
			StartTimeout: time.Second,
			StopTimeout:  time.Second,
		},
	}
	require.NoError(t, sup.execute(startCtx, tx))
	require.True(t, service.IsStarted())
	cancelStart()

	first := make(chan error, 1)
	go func() { first <- sup.StopContext(context.Background()) }()
	require.NoError(t, <-first)
	require.True(t, service.IsStopped())

	second := make(chan error, 1)
	go func() { second <- sup.Stop() }()
	require.NoError(t, <-second)
	require.Equal(t, 1, logs.FilterMessage("supervisor stopped").Len())
}

func TestY04LateControlAfterStopIsSafe(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previousProcs)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus := eventbus.NewBus()
	defer bus.Stop()
	sup := NewSupervisor(bus, zap.NewNop())
	require.NoError(t, sup.Start(ctx))

	service := newBlockingStartService()
	config := apisupervisor.LifecycleConfig{StartTimeout: time.Second, StopTimeout: time.Second}
	controller := NewController(ctx, service, config, sup.createStateHandler("blocked"))
	sup.mu.Lock()
	sup.controllers["blocked"] = controller
	sup.mu.Unlock()
	sup.handleEvent(event.Event{System: apisupervisor.System, Kind: apisupervisor.ServiceStart, Path: "blocked"})
	<-service.startedCh

	for len(sup.actions) < cap(sup.actions) {
		sup.actions <- action{kind: actStart, serviceID: "queued"}
	}

	callbackStarted := make(chan struct{})
	callbackDone := make(chan struct{})
	go func() {
		close(callbackStarted)
		sup.handleEvent(event.Event{System: apisupervisor.System, Kind: apisupervisor.ServiceStart, Path: "late"})
		close(callbackDone)
	}()
	<-callbackStarted
	runtime.Gosched()
	select {
	case <-callbackDone:
		t.Fatal("late callback unexpectedly bypassed the full action queue")
	default:
	}

	stopDone := make(chan error, 1)
	go func() { stopDone <- sup.Stop() }()
	require.NoError(t, <-stopDone)
	<-callbackDone
	_, err := sup.GetState("late")
	require.Error(t, err)
}

func TestY05LateRegistryCallbackAfterStopIsSafe(t *testing.T) {
	bus := eventbus.NewBus()
	defer bus.Stop()
	sup := NewSupervisor(bus, zap.NewNop())
	require.NoError(t, sup.Start(context.Background()))

	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		<-release
		sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
		close(done)
	}()

	require.NoError(t, sup.Stop())
	close(release)
	<-done
	require.False(t, sup.tx.open)
	require.Empty(t, sup.GetAllStates())
}
