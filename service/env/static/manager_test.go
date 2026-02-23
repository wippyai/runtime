// SPDX-License-Identifier: MPL-2.0

package static

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
	config     *envsvc.StaticStorageConfig
	shouldFail bool
}

func (m *mockTranscoder) Unmarshal(_ payload.Payload, out any) error {
	if m.shouldFail {
		return assert.AnError
	}
	if cfg, ok := out.(*envsvc.StaticStorageConfig); ok && m.config != nil {
		*cfg = *m.config
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
}

func TestManager_Add(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		bus := &mockBus{}
		dtt := &mockTranscoder{
			config: &envsvc.StaticStorageConfig{
				Values: map[string]string{
					"PUBLIC_API_HOST": "https://example.test",
					"APP_ENV":         "production",
				},
			},
		}
		log := zap.NewNop()
		mgr := NewManager(bus, dtt, log)

		entry := registry.Entry{
			ID:   registry.ID{NS: "app", Name: "static1"},
			Kind: envsvc.StorageStatic,
			Meta: attrs.NewBag(),
			Data: payload.New(nil),
		}

		err := mgr.Add(context.Background(), entry)
		require.NoError(t, err)

		require.Len(t, bus.events, 1)
		assert.Equal(t, env.StorageRegister, bus.events[0].Kind)

		storage, ok := mgr.GetStorage(entry.ID)
		require.True(t, ok)
		require.NotNil(t, storage)

		value, err := storage.Get(context.Background(), "PUBLIC_API_HOST")
		require.NoError(t, err)
		assert.Equal(t, "https://example.test", value)

		list, err := storage.List(context.Background())
		require.NoError(t, err)
		assert.Equal(t, map[string]string{
			"PUBLIC_API_HOST": "https://example.test",
			"APP_ENV":         "production",
		}, list)
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
			ID:   registry.ID{NS: "app", Name: "static1"},
			Kind: envsvc.StorageStatic,
			Data: payload.New(nil),
		}

		err := mgr.Add(context.Background(), entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode configuration")
	})
}

func TestManager_Update(t *testing.T) {
	bus := &mockBus{}
	dtt := &mockTranscoder{
		config: &envsvc.StaticStorageConfig{
			Values: map[string]string{
				"KEY": "value",
			},
		},
	}
	log := zap.NewNop()
	mgr := NewManager(bus, dtt, log)

	entry := registry.Entry{
		ID:   registry.ID{NS: "app", Name: "static1"},
		Kind: envsvc.StorageStatic,
		Meta: attrs.NewBag(),
		Data: payload.New(nil),
	}

	err := mgr.Update(context.Background(), entry)
	require.NoError(t, err)
}

func TestManager_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		bus := &mockBus{}
		dtt := &mockTranscoder{
			config: &envsvc.StaticStorageConfig{
				Values: map[string]string{
					"KEY": "value",
				},
			},
		}
		log := zap.NewNop()
		mgr := NewManager(bus, dtt, log)

		entry := registry.Entry{
			ID:   registry.ID{NS: "app", Name: "static1"},
			Kind: envsvc.StorageStatic,
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
			ID:   registry.ID{NS: "app", Name: "missing"},
			Kind: envsvc.StorageStatic,
		}

		err := mgr.Delete(context.Background(), entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "storage does not exist")
	})
}

func TestManager_GetStorage(t *testing.T) {
	bus := &mockBus{}
	dtt := &mockTranscoder{
		config: &envsvc.StaticStorageConfig{
			Values: map[string]string{
				"KEY": "value",
			},
		},
	}
	log := zap.NewNop()
	mgr := NewManager(bus, dtt, log)

	entry := registry.Entry{
		ID:   registry.ID{NS: "app", Name: "static1"},
		Kind: envsvc.StorageStatic,
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
