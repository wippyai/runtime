// SPDX-License-Identifier: MPL-2.0

package composite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
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
	config     *envsvc.RouterStorageConfig
	shouldFail bool
}

func (m *mockTranscoder) Unmarshal(_ payload.Payload, out any) error {
	if m.shouldFail {
		return assert.AnError
	}
	if cfg, ok := out.(*envsvc.RouterStorageConfig); ok && m.config != nil {
		*cfg = *m.config
	}
	return nil
}

func (m *mockTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

type mockStorage struct {
	data map[string]string
}

func newMockStorage(data map[string]string) *mockStorage {
	if data == nil {
		data = make(map[string]string)
	}
	return &mockStorage{data: data}
}

func (m *mockStorage) Get(_ context.Context, name string) (string, error) {
	if val, ok := m.data[name]; ok {
		return val, nil
	}
	return "", env.ErrVariableNotFound
}

func (m *mockStorage) Set(_ context.Context, name, value string) error {
	m.data[name] = value
	return nil
}

func (m *mockStorage) Delete(_ context.Context, name string) error {
	delete(m.data, name)
	return nil
}

func (m *mockStorage) List(_ context.Context) (map[string]string, error) {
	result := make(map[string]string)
	for k, v := range m.data {
		result[k] = v
	}
	return result, nil
}

type mockRegistry struct {
	storages map[registry.ID]env.Storage
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{
		storages: make(map[registry.ID]env.Storage),
	}
}

func (m *mockRegistry) Get(context.Context, string) (string, error) {
	return "", nil
}

func (m *mockRegistry) Lookup(context.Context, string) (string, bool, error) {
	return "", false, nil
}

func (m *mockRegistry) Set(context.Context, string, string) error {
	return nil
}

func (m *mockRegistry) All(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

func (m *mockRegistry) GetStorage(_ context.Context, id registry.ID) (env.Storage, error) {
	if s, ok := m.storages[id]; ok {
		return s, nil
	}
	return nil, env.ErrStorageNotFound
}

func (m *mockRegistry) RegisterStorage(id registry.ID, storage env.Storage) {
	m.storages[id] = storage
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
			config: &envsvc.RouterStorageConfig{
				Storages: []string{"app:storage1"},
			},
		}
		log := zap.NewNop()

		mgr := NewManager(bus, dtt, log)

		storage1 := newMockStorage(map[string]string{"KEY1": "value1"})
		mgr.storages[registry.ParseID("app:storage1")] = &Storage{storages: []env.Storage{storage1}}

		mockReg := newMockRegistry()
		mockReg.storages[registry.ParseID("app:storage1")] = storage1
		ctx := ctxapi.NewRootContext()
		ctx = env.WithRegistry(ctx, mockReg)

		entry := registry.Entry{
			ID:   registry.ID{NS: "app", Name: "router1"},
			Kind: envsvc.StorageRouter,
			Meta: attrs.NewBag(),
			Data: payload.New(nil),
		}

		err := mgr.Add(ctx, entry)
		require.NoError(t, err)

		require.Len(t, bus.events, 1)
		assert.Equal(t, env.StorageRegister, bus.events[0].Kind)
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
			ID:   registry.ID{NS: "app", Name: "router1"},
			Kind: envsvc.StorageRouter,
			Data: payload.New(nil),
		}

		err := mgr.Add(context.Background(), entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode configuration")
	})

	t.Run("empty storages", func(t *testing.T) {
		bus := &mockBus{}
		dtt := &mockTranscoder{
			config: &envsvc.RouterStorageConfig{
				Storages: []string{},
			},
		}
		log := zap.NewNop()

		mgr := NewManager(bus, dtt, log)

		entry := registry.Entry{
			ID:   registry.ID{NS: "app", Name: "router1"},
			Kind: envsvc.StorageRouter,
			Data: payload.New(nil),
		}

		err := mgr.Add(context.Background(), entry)
		require.Error(t, err)
	})

	t.Run("storage not found", func(t *testing.T) {
		bus := &mockBus{}
		dtt := &mockTranscoder{
			config: &envsvc.RouterStorageConfig{
				Storages: []string{"app:nonexistent"},
			},
		}
		log := zap.NewNop()

		mgr := NewManager(bus, dtt, log)

		ctx := ctxapi.NewRootContext()

		entry := registry.Entry{
			ID:   registry.ID{NS: "app", Name: "router1"},
			Kind: envsvc.StorageRouter,
			Data: payload.New(nil),
		}

		err := mgr.Add(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "storage backend not found")
	})
}

func TestManager_Update(t *testing.T) {
	bus := &mockBus{}
	dtt := &mockTranscoder{
		config: &envsvc.RouterStorageConfig{
			Storages: []string{"app:storage1"},
		},
	}
	log := zap.NewNop()

	mgr := NewManager(bus, dtt, log)

	storage1 := newMockStorage(nil)
	mockReg := newMockRegistry()
	mockReg.storages[registry.ParseID("app:storage1")] = storage1
	ctx := ctxapi.NewRootContext()
	ctx = env.WithRegistry(ctx, mockReg)

	entry := registry.Entry{
		ID:   registry.ID{NS: "app", Name: "router1"},
		Kind: envsvc.StorageRouter,
		Meta: attrs.NewBag(),
		Data: payload.New(nil),
	}

	err := mgr.Update(ctx, entry)
	require.NoError(t, err)
}

func TestManager_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		bus := &mockBus{}
		dtt := &mockTranscoder{
			config: &envsvc.RouterStorageConfig{
				Storages: []string{"app:storage1"},
			},
		}
		log := zap.NewNop()

		mgr := NewManager(bus, dtt, log)

		storage1 := newMockStorage(nil)
		mockReg := newMockRegistry()
		mockReg.storages[registry.ParseID("app:storage1")] = storage1
		ctx := ctxapi.NewRootContext()
		ctx = env.WithRegistry(ctx, mockReg)

		entry := registry.Entry{
			ID:   registry.ID{NS: "app", Name: "router1"},
			Kind: envsvc.StorageRouter,
			Meta: attrs.NewBag(),
			Data: payload.New(nil),
		}

		err := mgr.Add(ctx, entry)
		require.NoError(t, err)

		bus.events = nil

		err = mgr.Delete(ctx, entry)
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
}

func TestManager_GetStorage(t *testing.T) {
	bus := &mockBus{}
	dtt := &mockTranscoder{
		config: &envsvc.RouterStorageConfig{
			Storages: []string{"app:storage1"},
		},
	}
	log := zap.NewNop()

	mgr := NewManager(bus, dtt, log)

	storage1 := newMockStorage(nil)
	mockReg := newMockRegistry()
	mockReg.storages[registry.ParseID("app:storage1")] = storage1
	ctx := ctxapi.NewRootContext()
	ctx = env.WithRegistry(ctx, mockReg)

	entry := registry.Entry{
		ID:   registry.ID{NS: "app", Name: "router1"},
		Kind: envsvc.StorageRouter,
		Meta: attrs.NewBag(),
		Data: payload.New(nil),
	}

	err := mgr.Add(ctx, entry)
	require.NoError(t, err)

	storage, ok := mgr.GetStorage(entry.ID)
	assert.True(t, ok)
	assert.NotNil(t, storage)

	_, ok = mgr.GetStorage(registry.ID{NS: "app", Name: "nonexistent"})
	assert.False(t, ok)
}

func TestManager_FindStorage_FromLocalCache(t *testing.T) {
	bus := &mockBus{}
	dtt := &mockTranscoder{
		config: &envsvc.RouterStorageConfig{
			Storages: []string{"app:cached"},
		},
	}
	log := zap.NewNop()

	mgr := NewManager(bus, dtt, log)

	cachedStorage := &Storage{storages: []env.Storage{newMockStorage(nil)}}
	mgr.storages[registry.ParseID("app:cached")] = cachedStorage

	ctx := ctxapi.NewRootContext()

	entry := registry.Entry{
		ID:   registry.ID{NS: "app", Name: "router1"},
		Kind: envsvc.StorageRouter,
		Meta: attrs.NewBag(),
		Data: payload.New(nil),
	}

	err := mgr.Add(ctx, entry)
	require.NoError(t, err)
}
