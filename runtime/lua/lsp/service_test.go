package lsp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

func newTestService(t *testing.T) (*Service, *eventbus.Bus) {
	t.Helper()

	bus := eventbus.NewBus()
	log := zap.NewNop()
	cfg := DefaultConfig()

	svc := New(cfg, log, bus, nil)
	return svc, bus
}

func TestService_StartStop(t *testing.T) {
	svc, bus := newTestService(t)
	defer bus.Stop()

	ctx := context.Background()

	err := svc.Start(ctx)
	require.NoError(t, err)

	assert.NotNil(t, svc.LSPService())
	assert.NotNil(t, svc.Completion())
	assert.NotNil(t, svc.Signature())
	assert.NotNil(t, svc.Indexer())

	err = svc.Stop()
	require.NoError(t, err)
}

func TestService_StartTwice(t *testing.T) {
	svc, bus := newTestService(t)
	defer bus.Stop()

	ctx := context.Background()

	err := svc.Start(ctx)
	require.NoError(t, err)

	err = svc.Start(ctx)
	require.NoError(t, err)

	err = svc.Stop()
	require.NoError(t, err)
}

func TestService_StopWithoutStart(t *testing.T) {
	svc, bus := newTestService(t)
	defer bus.Stop()

	err := svc.Stop()
	require.NoError(t, err)
}

func TestService_StopTwice(t *testing.T) {
	svc, bus := newTestService(t)
	defer bus.Stop()

	ctx := context.Background()

	err := svc.Start(ctx)
	require.NoError(t, err)

	err = svc.Stop()
	require.NoError(t, err)

	err = svc.Stop()
	require.NoError(t, err)
}

func TestService_EventSubscription(t *testing.T) {
	svc, bus := newTestService(t)
	defer bus.Stop()

	ctx := context.Background()

	err := svc.Start(ctx)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	ids := []registry.ID{registry.NewID("", "test/module")}
	bus.Send(ctx, event.Event{
		System: luaapi.System,
		Kind:   luaapi.InvalidateNodes,
		Data:   ids,
	})

	time.Sleep(50 * time.Millisecond)

	err = svc.Stop()
	require.NoError(t, err)
}

func TestService_EventSubscriptionWithCanceledContext(t *testing.T) {
	svc, bus := newTestService(t)
	defer bus.Stop()

	ctx, cancel := context.WithCancel(context.Background())

	err := svc.Start(ctx)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	cancel()

	time.Sleep(50 * time.Millisecond)

	err = svc.Stop()
	require.NoError(t, err)
}

func TestService_HandleEventWithWrongKind(t *testing.T) {
	svc, bus := newTestService(t)
	defer bus.Stop()

	ctx := context.Background()

	err := svc.Start(ctx)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	bus.Send(ctx, event.Event{
		System: luaapi.System,
		Kind:   "some.other.event",
		Data:   "test",
	})

	time.Sleep(50 * time.Millisecond)

	err = svc.Stop()
	require.NoError(t, err)
}

func TestService_HandleEventWithWrongDataType(t *testing.T) {
	svc, bus := newTestService(t)
	defer bus.Stop()

	ctx := context.Background()

	err := svc.Start(ctx)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	bus.Send(ctx, event.Event{
		System: luaapi.System,
		Kind:   luaapi.InvalidateNodes,
		Data:   "not an array of registry.ID",
	})

	time.Sleep(50 * time.Millisecond)

	err = svc.Stop()
	require.NoError(t, err)
}

func TestService_ConcurrentStartStop(t *testing.T) {
	for i := 0; i < 10; i++ {
		svc, bus := newTestService(t)

		ctx := context.Background()

		go func() {
			_ = svc.Start(ctx)
		}()

		go func() {
			time.Sleep(5 * time.Millisecond)
			_ = svc.Stop()
		}()

		time.Sleep(20 * time.Millisecond)
		bus.Stop()
	}
}

func TestService_EventsDuringShutdown(t *testing.T) {
	svc, bus := newTestService(t)
	defer bus.Stop()

	ctx := context.Background()

	err := svc.Start(ctx)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	go func() {
		for i := 0; i < 100; i++ {
			ids := []registry.ID{registry.NewID("", "test/module")}
			bus.Send(ctx, event.Event{
				System: luaapi.System,
				Kind:   luaapi.InvalidateNodes,
				Data:   ids,
			})
		}
	}()

	time.Sleep(10 * time.Millisecond)

	err = svc.Stop()
	require.NoError(t, err)
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name     string
		wantAddr string
		cfg      Config
	}{
		{
			name:     "valid address",
			cfg:      Config{Address: ":8080"},
			wantAddr: ":8080",
		},
		{
			name:     "empty address gets default",
			cfg:      Config{Address: ""},
			wantAddr: DefaultAddress,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cfg.Validate()
			assert.Equal(t, tt.wantAddr, tt.cfg.Address)
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.False(t, cfg.Enabled)
	assert.Equal(t, DefaultAddress, cfg.Address)
}

func TestService_NilBus(t *testing.T) {
	log := zap.NewNop()
	cfg := DefaultConfig()

	svc := New(cfg, log, nil, nil)

	ctx := context.Background()

	err := svc.Start(ctx)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	err = svc.Stop()
	require.NoError(t, err)
}
