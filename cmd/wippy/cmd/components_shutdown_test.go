// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	bootapi "github.com/wippyai/runtime/api/boot"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/pid"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	relayapi "github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	supervisorapi "github.com/wippyai/runtime/api/supervisor"
	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/components/core"
	bootsystem "github.com/wippyai/runtime/boot/components/system"
	"github.com/wippyai/runtime/system/eventbus"
	processsystem "github.com/wippyai/runtime/system/process"
	relaysystem "github.com/wippyai/runtime/system/relay"
	"github.com/wippyai/runtime/system/supervisor"
	"go.uber.org/zap"
)

type shutdownTTYProducer struct {
	surface ttyapi.Surface
	started chan struct{}
	stopped chan error
	status  chan any
}

func (s *shutdownTTYProducer) Start(context.Context) (<-chan any, error) {
	close(s.started)
	return s.status, nil
}

func (s *shutdownTTYProducer) Stop(context.Context) error {
	_, err := s.surface.Present(ttyapi.Frame{Rows: []string{"last frame"}})
	s.stopped <- err
	close(s.status)
	return err
}

func TestStandardComponentsShutdownBeforeInfrastructure(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = ctxapi.WithFrameResolvers(ctx, ctxapi.NewFrameResolvers())
	ctx = processapi.WithLifecycleRegistry(ctx, processsystem.NewLifecycleRegistry())
	ctx = relayapi.WithNode(ctx, relaysystem.NewNode("shutdown-test"))
	bus := eventbus.NewBus()
	defer bus.Stop()
	sup := supervisor.NewSupervisor(bus, zap.NewNop())
	defer sup.Stop()

	var components []bootapi.Component
	var loaded, stopped []string
	// Keep every standard dependency edge. Only TTY and the supervisor need live
	// services; other components record their actual loader callbacks so the test
	// needs neither external services nor runtime compilation.
	for _, component := range StandardComponents() {
		components = append(components, bootapi.New(bootapi.P{
			Name: component.Name(), DependsOn: component.DependsOn(),
			Load: func(ctx context.Context) (context.Context, error) {
				loaded = append(loaded, component.Name())
				if component.Name() == bootsystem.TTYName {
					return component.Load(ctx)
				}
				return ctx, nil
			},
			Stop: func(ctx context.Context) error {
				stopped = append(stopped, component.Name())
				switch component.Name() {
				case bootsystem.TTYName:
					return component.(bootapi.Stopper).Stop(ctx)
				case core.SupervisorName:
					return sup.StopContext(ctx)
				}
				return nil
			},
		}))
	}
	loader, err := boot.NewLoader(components...)
	require.NoError(t, err)
	ctx, err = loader.Load(ctx)
	require.NoError(t, err)
	processCtx, frame := ctxapi.OpenFrameContext(ctx)
	defer frame.Close()
	require.NoError(t, frame.Set(runtime.FramePIDKey, pid.PID{Node: "shutdown-test", Host: "workers", UniqID: "app"}))
	service := ttyapi.GetService(ctx)
	viewport, err := service.Create(processCtx, 80, 24)
	require.NoError(t, err)
	binding, err := service.Binding(viewport.Grant())
	require.NoError(t, err)
	port, err := binding.Resolve(processCtx)
	require.NoError(t, err)
	surface, err := port.OpenSurface(ttyapi.SurfaceOptions{})
	require.NoError(t, err)
	producer := &shutdownTTYProducer{surface: surface, started: make(chan struct{}), stopped: make(chan error, 1), status: make(chan any)}
	require.NoError(t, sup.Start(ctx))
	bus.Send(ctx, event.Event{System: registry.System, Kind: registry.TxBegin})
	bus.Send(ctx, event.Event{System: supervisorapi.System, Kind: supervisorapi.ServiceRegister, Path: "test:producer", Data: &supervisorapi.Entry{Service: producer, Config: supervisorapi.LifecycleConfig{AutoStart: true}}})
	bus.Send(ctx, event.Event{System: registry.System, Kind: registry.TxCommit})
	select {
	case <-producer.started:
	case <-time.After(5 * time.Second):
		t.Fatal("supervised producer did not start")
	}
	require.NoError(t, loader.Shutdown(ctx))
	t.Logf("component shutdown order: %v", stopped)
	select {
	case err := <-producer.stopped:
		require.NoError(t, err, "TTY must remain usable until the supervised producer stops")
	default:
		t.Fatal("shutdown did not stop supervised producer")
	}
	_, err = surface.Present(ttyapi.Frame{Rows: []string{"after shutdown"}})
	require.Error(t, err, "shutdown must still close TTY sessions")
	require.Equal(t, core.SupervisorName, stopped[0], "supervised services must stop before infrastructure")
	// All remaining components retain their reverse load order.
	expected := slices.DeleteFunc(slices.Clone(loaded), func(name string) bool { return name == core.SupervisorName })
	slices.Reverse(expected)
	require.Equal(t, expected, stopped[1:])
}
