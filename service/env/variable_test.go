package env

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
	variable   *env.Variable
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
			Kind: envsvc.KindVariable,
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
			Kind: envsvc.KindStorageMemory,
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
			Kind: envsvc.KindVariable,
			Data: payload.New(nil),
		}

		err := mgr.Add(context.Background(), entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode variable")
	})
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
			Kind: envsvc.KindVariable,
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
			Kind: envsvc.KindStorageMemory,
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
			Kind: envsvc.KindVariable,
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
			Kind: envsvc.KindVariable,
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
			Kind: envsvc.KindStorageMemory,
		}

		err := mgr.Delete(context.Background(), entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported entry kind")
	})
}
