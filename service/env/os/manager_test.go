package os

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	envsvc "github.com/wippyai/runtime/api/service/env"
	"go.uber.org/zap"
)

type mockBus struct {
	events []event.Event
}

func (m *mockBus) Send(_ context.Context, e event.Event) {
	m.events = append(m.events, e)
}

func (m *mockBus) Subscribe(context.Context, event.System, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (m *mockBus) SubscribeP(context.Context, event.System, event.Kind, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (m *mockBus) Unsubscribe(context.Context, event.SubscriberID) {}

type mockTranscoder struct {
	shouldFail bool
}

func (m *mockTranscoder) Unmarshal(_ payload.Payload, out any) error {
	if m.shouldFail {
		return assert.AnError
	}
	return nil
}

func (m *mockTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

func TestNewManager(t *testing.T) {
	bus := &mockBus{}
	dtt := &mockTranscoder{}
	log := zap.NewNop()

	mgr := NewManager(bus, dtt, log)

	assert.NotNil(t, mgr)
	assert.NotNil(t, mgr.storages)
	assert.Nil(t, mgr.staticEnv)
}

func TestNewManager_WithStaticEnv(t *testing.T) {
	bus := &mockBus{}
	dtt := &mockTranscoder{}
	log := zap.NewNop()

	staticEnv := map[string]string{"KEY": "value"}
	mgr := NewManager(bus, dtt, log, WithStaticEnv(staticEnv))

	assert.NotNil(t, mgr)
	assert.Equal(t, staticEnv, mgr.staticEnv)
}

func TestManager_Add(t *testing.T) {
	t.Run("success with os storage", func(t *testing.T) {
		bus := &mockBus{}
		dtt := &mockTranscoder{}
		log := zap.NewNop()

		mgr := NewManager(bus, dtt, log)

		entry := registry.Entry{
			ID:   registry.ID{NS: "app", Name: "os1"},
			Kind: envsvc.StorageOS,
			Meta: attrs.NewBag(),
			Data: payload.New(nil),
		}

		err := mgr.Add(context.Background(), entry)
		require.NoError(t, err)

		require.Len(t, bus.events, 1)
		assert.Equal(t, env.StorageRegister, bus.events[0].Kind)

		storage, ok := mgr.GetStorage(entry.ID)
		assert.True(t, ok)
		assert.NotNil(t, storage)
	})

	t.Run("success with static storage", func(t *testing.T) {
		bus := &mockBus{}
		dtt := &mockTranscoder{}
		log := zap.NewNop()

		staticEnv := map[string]string{"KEY": "value"}
		mgr := NewManager(bus, dtt, log, WithStaticEnv(staticEnv))

		entry := registry.Entry{
			ID:   registry.ID{NS: "app", Name: "static1"},
			Kind: envsvc.StorageOS,
			Meta: attrs.NewBag(),
			Data: payload.New(nil),
		}

		err := mgr.Add(context.Background(), entry)
		require.NoError(t, err)

		storage, ok := mgr.GetStorage(entry.ID)
		assert.True(t, ok)

		value, err := storage.Get(context.Background(), "KEY")
		require.NoError(t, err)
		assert.Equal(t, "value", value)
	})

	t.Run("wrong kind", func(t *testing.T) {
		bus := &mockBus{}
		dtt := &mockTranscoder{}
		log := zap.NewNop()

		mgr := NewManager(bus, dtt, log)

		entry := registry.Entry{
			ID:   registry.ID{NS: "app", Name: "memory1"},
			Kind: envsvc.StorageMemory,
		}

		err := mgr.Add(context.Background(), entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported entry kind")
	})

	t.Run("decode error", func(t *testing.T) {
		bus := &mockBus{}
		dtt := &mockTranscoder{shouldFail: true}
		log := zap.NewNop()

		mgr := NewManager(bus, dtt, log)

		entry := registry.Entry{
			ID:   registry.ID{NS: "app", Name: "os1"},
			Kind: envsvc.StorageOS,
			Data: payload.New(nil),
		}

		err := mgr.Add(context.Background(), entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode configuration")
	})
}

func TestManager_Update(t *testing.T) {
	bus := &mockBus{}
	dtt := &mockTranscoder{}
	log := zap.NewNop()

	mgr := NewManager(bus, dtt, log)

	entry := registry.Entry{
		ID:   registry.ID{NS: "app", Name: "os1"},
		Kind: envsvc.StorageOS,
		Meta: attrs.NewBag(),
		Data: payload.New(nil),
	}

	err := mgr.Update(context.Background(), entry)
	require.NoError(t, err)
}

func TestManager_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		bus := &mockBus{}
		dtt := &mockTranscoder{}
		log := zap.NewNop()

		mgr := NewManager(bus, dtt, log)

		entry := registry.Entry{
			ID:   registry.ID{NS: "app", Name: "os1"},
			Kind: envsvc.StorageOS,
			Meta: attrs.NewBag(),
			Data: payload.New(nil),
		}

		err := mgr.Add(context.Background(), entry)
		require.NoError(t, err)

		bus.events = nil

		err = mgr.Delete(context.Background(), entry)
		require.NoError(t, err)

		require.Len(t, bus.events, 1)
		assert.Equal(t, env.StorageDelete, bus.events[0].Kind)

		_, ok := mgr.GetStorage(entry.ID)
		assert.False(t, ok)
	})

	t.Run("wrong kind", func(t *testing.T) {
		bus := &mockBus{}
		dtt := &mockTranscoder{}
		log := zap.NewNop()

		mgr := NewManager(bus, dtt, log)

		entry := registry.Entry{
			ID:   registry.ID{NS: "app", Name: "memory1"},
			Kind: envsvc.StorageMemory,
		}

		err := mgr.Delete(context.Background(), entry)
		require.Error(t, err)
	})

	t.Run("not found", func(t *testing.T) {
		bus := &mockBus{}
		dtt := &mockTranscoder{}
		log := zap.NewNop()

		mgr := NewManager(bus, dtt, log)

		entry := registry.Entry{
			ID:   registry.ID{NS: "app", Name: "nonexistent"},
			Kind: envsvc.StorageOS,
		}

		err := mgr.Delete(context.Background(), entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "storage does not exist")
	})
}

func TestManager_GetStorage(t *testing.T) {
	bus := &mockBus{}
	dtt := &mockTranscoder{}
	log := zap.NewNop()

	mgr := NewManager(bus, dtt, log)

	entry := registry.Entry{
		ID:   registry.ID{NS: "app", Name: "os1"},
		Kind: envsvc.StorageOS,
		Meta: attrs.NewBag(),
		Data: payload.New(nil),
	}

	err := mgr.Add(context.Background(), entry)
	require.NoError(t, err)

	storage, ok := mgr.GetStorage(entry.ID)
	assert.True(t, ok)
	assert.NotNil(t, storage)

	_, ok = mgr.GetStorage(registry.ID{NS: "app", Name: "nonexistent"})
	assert.False(t, ok)
}
