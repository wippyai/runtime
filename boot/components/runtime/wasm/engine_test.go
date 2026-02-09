package wasm

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/components/dispatchers"
)

type testDispatcherRegistry struct{}

func (testDispatcherRegistry) Get(dispatcher.CommandID) dispatcher.Handler { return nil }
func (testDispatcherRegistry) Has(dispatcher.CommandID) bool               { return true }
func (testDispatcherRegistry) Dispatch(dispatcher.Command) dispatcher.Handler {
	return nil
}

type testBus struct{}

func (testBus) Subscribe(context.Context, event.System, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}
func (testBus) SubscribeP(context.Context, event.System, event.Kind, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}
func (testBus) Unsubscribe(context.Context, event.SubscriberID) {}
func (testBus) Send(context.Context, event.Event)               {}

func TestAllIncludesEngine(t *testing.T) {
	all := All()
	if len(all) != 1 {
		t.Fatalf("All() len = %d, want 1", len(all))
	}
	if all[0].Name() != EngineName {
		t.Fatalf("All()[0].Name() = %q, want %q", all[0].Name(), EngineName)
	}
}

func TestEngineAlias(t *testing.T) {
	if Engine().Name() != EngineWithHostProfiles().Name() {
		t.Fatalf("Engine() should delegate to EngineWithHostProfiles()")
	}
}

func TestDefaultHostProfiles(t *testing.T) {
	profiles := DefaultHostProfiles(nil, nil)
	if len(profiles) != 3 {
		t.Fatalf("DefaultHostProfiles() len = %d, want 3", len(profiles))
	}
	if profiles[0].Name != "funcs" {
		t.Fatalf("profiles[0].Name = %q, want funcs", profiles[0].Name)
	}
	if profiles[1].Name != "wasi1" {
		t.Fatalf("profiles[1].Name = %q, want wasi1", profiles[1].Name)
	}
	if profiles[2].Name != "wasi2" {
		t.Fatalf("profiles[2].Name = %q, want wasi2", profiles[2].Name)
	}
}

func TestEngineWithHostProfiles_LoadRequiresDispatcher(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = event.WithBus(ctx, testBus{})
	ctx = payload.WithTranscoder(ctx, nil)
	ctx = bootpkg.WithHandlerRegistry(ctx, bootpkg.NewHandlerRegistry())

	component := EngineWithHostProfiles()
	_, err := component.Load(ctx)
	if err == nil {
		t.Fatal("Load() expected dispatcher not found error")
	}
	if err.Error() != dispatchers.ErrDispatcherNotFound.Error() {
		t.Fatalf("Load() error = %q, want %q", err.Error(), dispatchers.ErrDispatcherNotFound.Error())
	}
}

func TestEngineWithHostProfiles_LoadStartStop(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = event.WithBus(ctx, testBus{})
	ctx = bootpkg.WithHandlerRegistry(ctx, bootpkg.NewHandlerRegistry())

	if err := dispatcher.WithRegistry(ctx, testDispatcherRegistry{}); err != nil {
		t.Fatalf("WithRegistry() error = %v", err)
	}

	component := EngineWithHostProfiles()
	if component.Name() != EngineName {
		t.Fatalf("Name() = %q, want %q", component.Name(), EngineName)
	}

	deps := component.DependsOn()
	if len(deps) != 2 || deps[0] != dispatchers.ClockDispatcherName || deps[1] != dispatchers.SocketDispatcherName {
		t.Fatalf("DependsOn() = %#v, want [%q, %q]", deps, dispatchers.ClockDispatcherName, dispatchers.SocketDispatcherName)
	}

	loadedCtx, err := component.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if wasmapi.GetTransportRegistry(loadedCtx) == nil {
		t.Fatal("transport registry not attached to context")
	}

	reg := bootpkg.GetHandlerRegistry(loadedCtx)
	if reg == nil {
		t.Fatal("handler registry missing after Load()")
	}
	if len(reg.Handlers()) != 2 {
		t.Fatalf("handlers len = %d, want 2", len(reg.Handlers()))
	}

	if starter, ok := component.(interface{ Start(context.Context) error }); ok {
		if err := starter.Start(loadedCtx); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
	}

	if stopper, ok := component.(interface{ Stop(context.Context) error }); ok {
		if err := stopper.Stop(loadedCtx); err != nil {
			t.Fatalf("Stop() error = %v", err)
		}
	}
}
