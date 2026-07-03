// SPDX-License-Identifier: MPL-2.0

package env

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
	sysenv "github.com/wippyai/runtime/system/env"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// mockStorage is a minimal in-memory env.Storage for manager wiring tests.
type mockStorage struct {
	data map[string]string
}

func (m *mockStorage) Get(_ context.Context, name string) (string, error) {
	if v, ok := m.data[name]; ok {
		return v, nil
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
	out := make(map[string]string, len(m.data))
	for k, v := range m.data {
		out[k] = v
	}
	return out, nil
}

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
	variable   *env.Variable
	shouldFail bool
}

func (m *mockTranscoder) Unmarshal(_ payload.Payload, out any) error {
	if m.shouldFail {
		return assert.AnError
	}
	if v, ok := out.(*env.Variable); ok && m.variable != nil {
		*v = *m.variable
	}
	return nil
}

func (m *mockTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

func TestNewVariableManager(t *testing.T) {
	bus := &mockBus{}
	dtt := &mockTranscoder{}
	log := zap.NewNop()

	mgr := NewVariableManager(bus, dtt, log)

	assert.NotNil(t, mgr)
	assert.Equal(t, bus, mgr.bus)
	assert.Equal(t, dtt, mgr.dtt)
}

func TestVariableManager_Add(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		bus := &mockBus{}
		variable := &env.Variable{
			ID:        registry.ParseID("app:my_var"),
			Name:      "MY_VAR",
			StorageID: registry.ParseID("app:storage"),
		}
		dtt := &mockTranscoder{variable: variable}
		log := zap.NewNop()

		mgr := NewVariableManager(bus, dtt, log)

		entry := registry.Entry{
			ID:   registry.ID{NS: "app", Name: "my_var"},
			Kind: envsvc.Variable,
			Meta: attrs.NewBag(),
			Data: payload.New(nil),
		}

		err := mgr.Add(context.Background(), entry)
		require.NoError(t, err)

		require.Len(t, bus.events, 1)
		assert.Equal(t, env.System, bus.events[0].System)
		assert.Equal(t, env.VariableRegister, bus.events[0].Kind)
	})

	t.Run("wrong kind", func(t *testing.T) {
		bus := &mockBus{}
		dtt := &mockTranscoder{}
		log := zap.NewNop()

		mgr := NewVariableManager(bus, dtt, log)

		entry := registry.Entry{
			ID:   registry.ID{NS: "app", Name: "storage"},
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

		mgr := NewVariableManager(bus, dtt, log)

		entry := registry.Entry{
			ID:   registry.ID{NS: "app", Name: "my_var"},
			Kind: envsvc.Variable,
			Data: payload.New(nil),
		}

		err := mgr.Add(context.Background(), entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode variable")
	})
}

func newSyncTestContext(reg env.Registry) context.Context {
	ctx := ctxapi.NewRootContext()
	return env.WithRegistry(ctx, reg)
}

func TestVariableManager_Add_SyncRegistration(t *testing.T) {
	log := zap.NewNop()
	reg := sysenv.NewRegistry(eventbus.NewBus(), log)

	storageID := registry.ParseID("app:storage")
	reg.RegisterStorage(storageID, &mockStorage{data: map[string]string{"MY_VAR": "sync_value"}})
	ctx := newSyncTestContext(reg)

	variable := &env.Variable{
		ID:        registry.ParseID("app:my_var"),
		Name:      "MY_VAR",
		StorageID: storageID,
	}
	bus := &mockBus{}
	mgr := NewVariableManager(bus, &mockTranscoder{variable: variable}, log)

	entry := registry.Entry{
		ID:   registry.ParseID("app:my_var"),
		Kind: envsvc.Variable,
		Meta: attrs.NewBag(),
		Data: payload.New(nil),
	}

	require.NoError(t, mgr.Add(ctx, entry))

	// Resolvable immediately, without waiting for the bus event.
	value, err := reg.Get(ctx, "MY_VAR")
	require.NoError(t, err)
	assert.Equal(t, "sync_value", value)

	// Bus event still emitted for external consumers.
	require.Len(t, bus.events, 1)
	assert.Equal(t, env.VariableRegister, bus.events[0].Kind)
}

func TestVariableManager_Add_SyncStorageNotFound(t *testing.T) {
	log := zap.NewNop()
	reg := sysenv.NewRegistry(eventbus.NewBus(), log)
	ctx := newSyncTestContext(reg)

	variable := &env.Variable{
		ID:        registry.ParseID("app:my_var"),
		Name:      "MY_VAR",
		StorageID: registry.ParseID("app:missing"),
	}
	bus := &mockBus{}
	mgr := NewVariableManager(bus, &mockTranscoder{variable: variable}, log)

	entry := registry.Entry{
		ID:   registry.ParseID("app:my_var"),
		Kind: envsvc.Variable,
		Data: payload.New(nil),
	}

	err := mgr.Add(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "referenced storage not found")

	// Invalid registration is not broadcast on the bus.
	assert.Empty(t, bus.events)
}

func TestVariableManager_Delete_SyncUnregister(t *testing.T) {
	log := zap.NewNop()
	reg := sysenv.NewRegistry(eventbus.NewBus(), log)

	storageID := registry.ParseID("app:storage")
	reg.RegisterStorage(storageID, &mockStorage{data: map[string]string{"MY_VAR": "sync_value"}})
	ctx := newSyncTestContext(reg)

	variable := &env.Variable{
		ID:        registry.ParseID("app:my_var"),
		Name:      "MY_VAR",
		StorageID: storageID,
	}
	bus := &mockBus{}
	mgr := NewVariableManager(bus, &mockTranscoder{variable: variable}, log)

	entry := registry.Entry{
		ID:   registry.ParseID("app:my_var"),
		Kind: envsvc.Variable,
		Data: payload.New(nil),
	}
	require.NoError(t, mgr.Add(ctx, entry))

	value, err := reg.Get(ctx, "MY_VAR")
	require.NoError(t, err)
	assert.Equal(t, "sync_value", value)

	require.NoError(t, mgr.Delete(ctx, entry))

	_, err = reg.Get(ctx, "MY_VAR")
	assert.ErrorIs(t, err, env.ErrVariableNotFound)
}

func TestVariableManager_Update(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		bus := &mockBus{}
		variable := &env.Variable{
			ID:        registry.ParseID("app:my_var"),
			Name:      "MY_VAR",
			StorageID: registry.ParseID("app:storage"),
		}
		dtt := &mockTranscoder{variable: variable}
		log := zap.NewNop()

		mgr := NewVariableManager(bus, dtt, log)

		entry := registry.Entry{
			ID:   registry.ID{NS: "app", Name: "my_var"},
			Kind: envsvc.Variable,
			Meta: attrs.NewBag(),
			Data: payload.New(nil),
		}

		err := mgr.Update(context.Background(), entry)
		require.NoError(t, err)

		require.Len(t, bus.events, 1)
		assert.Equal(t, env.VariableUpdate, bus.events[0].Kind)
	})

	t.Run("wrong kind", func(t *testing.T) {
		bus := &mockBus{}
		dtt := &mockTranscoder{}
		log := zap.NewNop()

		mgr := NewVariableManager(bus, dtt, log)

		entry := registry.Entry{
			ID:   registry.ID{NS: "app", Name: "storage"},
			Kind: envsvc.StorageMemory,
		}

		err := mgr.Update(context.Background(), entry)
		require.Error(t, err)
	})

	t.Run("decode error", func(t *testing.T) {
		bus := &mockBus{}
		dtt := &mockTranscoder{shouldFail: true}
		log := zap.NewNop()

		mgr := NewVariableManager(bus, dtt, log)

		entry := registry.Entry{
			ID:   registry.ID{NS: "app", Name: "my_var"},
			Kind: envsvc.Variable,
			Data: payload.New(nil),
		}

		err := mgr.Update(context.Background(), entry)
		require.Error(t, err)
	})
}

func TestVariableManager_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		bus := &mockBus{}
		dtt := &mockTranscoder{}
		log := zap.NewNop()

		mgr := NewVariableManager(bus, dtt, log)

		entry := registry.Entry{
			ID:   registry.ID{NS: "app", Name: "my_var"},
			Kind: envsvc.Variable,
		}

		err := mgr.Delete(context.Background(), entry)
		require.NoError(t, err)

		require.Len(t, bus.events, 1)
		assert.Equal(t, env.VariableDelete, bus.events[0].Kind)
	})

	t.Run("wrong kind", func(t *testing.T) {
		bus := &mockBus{}
		dtt := &mockTranscoder{}
		log := zap.NewNop()

		mgr := NewVariableManager(bus, dtt, log)

		entry := registry.Entry{
			ID:   registry.ID{NS: "app", Name: "storage"},
			Kind: envsvc.StorageMemory,
		}

		err := mgr.Delete(context.Background(), entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported entry kind")
	})
}
