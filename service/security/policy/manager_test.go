package policy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	policyapi "github.com/wippyai/runtime/api/service/policy"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// mockFactory implements FactoryAPI for testing
type mockFactory struct {
	createFunc func(ctx context.Context, entry registry.Entry) (*security.PolicyEntry, error)
}

func (m *mockFactory) CreatePolicyEntry(ctx context.Context, entry registry.Entry) (*security.PolicyEntry, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, entry)
	}
	return &security.PolicyEntry{
		Policy: &mockPolicy{id: entry.ID},
		Groups: []registry.ID{},
	}, nil
}

// mockPolicy implements security.Policy for testing
type mockPolicy struct {
	id registry.ID
}

func (m *mockPolicy) ID() registry.ID {
	return m.id
}

func (m *mockPolicy) Evaluate(_ security.Actor, _, _ string, _ registry.Metadata) security.Result {
	return security.Allow
}

func setupManagerTest() (*Manager, *eventbus.Bus) {
	bus := eventbus.NewBus()
	factory := &mockFactory{}
	logger := zap.NewNop()
	manager := NewManager(bus, factory, logger)
	return manager, bus
}

func TestManager_Add_ConditionPolicy(t *testing.T) {
	ctx := context.Background()
	manager, bus := setupManagerTest()

	eventCh := make(chan event.Event, 10)
	subID, err := bus.Subscribe(ctx, security.System, eventCh)
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, subID)

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "policy1"},
		Kind: policyapi.Kind,
	}

	err = manager.Add(ctx, entry)
	require.NoError(t, err)

	select {
	case evt := <-eventCh:
		assert.Equal(t, security.System, evt.System)
		assert.Equal(t, security.PolicyRegister, evt.Kind)
		assert.Equal(t, "test:policy1", evt.Path)
		assert.NotNil(t, evt.Data)
	case <-ctx.Done():
		t.Fatal("timeout waiting for event")
	}
}

func TestManager_Add_ExprPolicy(t *testing.T) {
	ctx := context.Background()
	manager, bus := setupManagerTest()

	eventCh := make(chan event.Event, 10)
	subID, err := bus.Subscribe(ctx, security.System, eventCh)
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, subID)

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "expr1"},
		Kind: policyapi.ExprKind,
	}

	err = manager.Add(ctx, entry)
	require.NoError(t, err)

	select {
	case evt := <-eventCh:
		assert.Equal(t, security.PolicyRegister, evt.Kind)
		assert.Equal(t, "test:expr1", evt.Path)
	case <-ctx.Done():
		t.Fatal("timeout waiting for event")
	}
}

func TestManager_Add_UnsupportedKind(t *testing.T) {
	ctx := context.Background()
	manager, bus := setupManagerTest()

	eventCh := make(chan event.Event, 10)
	subID, err := bus.Subscribe(ctx, security.System, eventCh)
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, subID)

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "other"},
		Kind: "other.kind",
	}

	err = manager.Add(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported entry kind")

	select {
	case <-eventCh:
		t.Fatal("should not receive event for unsupported kind")
	default:
	}
}

func TestManager_Update_ConditionPolicy(t *testing.T) {
	ctx := context.Background()
	manager, bus := setupManagerTest()

	eventCh := make(chan event.Event, 10)
	subID, err := bus.Subscribe(ctx, security.System, eventCh)
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, subID)

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "policy1"},
		Kind: policyapi.Kind,
	}

	err = manager.Update(ctx, entry)
	require.NoError(t, err)

	select {
	case evt := <-eventCh:
		assert.Equal(t, security.PolicyUpdate, evt.Kind)
		assert.Equal(t, "test:policy1", evt.Path)
	case <-ctx.Done():
		t.Fatal("timeout waiting for event")
	}
}

func TestManager_Update_ExprPolicy(t *testing.T) {
	ctx := context.Background()
	manager, bus := setupManagerTest()

	eventCh := make(chan event.Event, 10)
	subID, err := bus.Subscribe(ctx, security.System, eventCh)
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, subID)

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "expr1"},
		Kind: policyapi.ExprKind,
	}

	err = manager.Update(ctx, entry)
	require.NoError(t, err)

	select {
	case evt := <-eventCh:
		assert.Equal(t, security.PolicyUpdate, evt.Kind)
		assert.Equal(t, "test:expr1", evt.Path)
	case <-ctx.Done():
		t.Fatal("timeout waiting for event")
	}
}

func TestManager_Delete_ConditionPolicy(t *testing.T) {
	ctx := context.Background()
	manager, bus := setupManagerTest()

	eventCh := make(chan event.Event, 10)
	subID, err := bus.Subscribe(ctx, security.System, eventCh)
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, subID)

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "policy1"},
		Kind: policyapi.Kind,
	}

	err = manager.Delete(ctx, entry)
	require.NoError(t, err)

	select {
	case evt := <-eventCh:
		assert.Equal(t, security.PolicyDelete, evt.Kind)
		assert.Equal(t, "test:policy1", evt.Path)
		assert.Nil(t, evt.Data)
	case <-ctx.Done():
		t.Fatal("timeout waiting for event")
	}
}

func TestManager_Delete_ExprPolicy(t *testing.T) {
	ctx := context.Background()
	manager, bus := setupManagerTest()

	eventCh := make(chan event.Event, 10)
	subID, err := bus.Subscribe(ctx, security.System, eventCh)
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, subID)

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "expr1"},
		Kind: policyapi.ExprKind,
	}

	err = manager.Delete(ctx, entry)
	require.NoError(t, err)

	select {
	case evt := <-eventCh:
		assert.Equal(t, security.PolicyDelete, evt.Kind)
		assert.Equal(t, "test:expr1", evt.Path)
	case <-ctx.Done():
		t.Fatal("timeout waiting for event")
	}
}

func TestManager_Add_FactoryError(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	factory := &mockFactory{
		createFunc: func(_ context.Context, _ registry.Entry) (*security.PolicyEntry, error) {
			return nil, assert.AnError
		},
	}
	logger := zap.NewNop()
	manager := NewManager(bus, factory, logger)

	eventCh := make(chan event.Event, 10)
	subID, err := bus.Subscribe(ctx, security.System, eventCh)
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, subID)

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "policy1"},
		Kind: policyapi.Kind,
	}

	err = manager.Add(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create policy entry")

	select {
	case <-eventCh:
		t.Fatal("should not receive event when factory fails")
	default:
	}
}

func TestManager_Update_FactoryError(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	factory := &mockFactory{
		createFunc: func(_ context.Context, _ registry.Entry) (*security.PolicyEntry, error) {
			return nil, assert.AnError
		},
	}
	logger := zap.NewNop()
	manager := NewManager(bus, factory, logger)

	eventCh := make(chan event.Event, 10)
	subID, err := bus.Subscribe(ctx, security.System, eventCh)
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, subID)

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "policy1"},
		Kind: policyapi.Kind,
	}

	err = manager.Update(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create policy entry")

	select {
	case <-eventCh:
		t.Fatal("should not receive event when factory fails")
	default:
	}
}
