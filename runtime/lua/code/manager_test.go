package code

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// testEventBus implements event.Bus for testing
type testEventBus struct {
	events []event.Event
}

func (b *testEventBus) Send(_ context.Context, e event.Event) {
	b.events = append(b.events, e)
}

func (b *testEventBus) Subscribe(_ context.Context, _ event.System, _ chan<- event.Event) (event.SubscriberID, error) {
	return "test", nil
}

func (b *testEventBus) SubscribeP(_ context.Context, _ event.System, _ event.Kind, _ chan<- event.Event) (event.SubscriberID, error) {
	return "test", nil
}

func (b *testEventBus) Unsubscribe(_ context.Context, _ event.SubscriberID) {
}

func TestNewCodeManager(t *testing.T) {
	tests := []struct {
		name           string
		modules        []api.Module
		protoCacheSize int
		mainCacheSize  int
		expectErr      bool
	}{
		{
			name:           "Default cache sizes",
			modules:        []api.Module{&testModule{name: "test"}},
			protoCacheSize: 0,
			mainCacheSize:  0,
			expectErr:      false,
		},
		{
			name:           "Custom cache sizes",
			modules:        []api.Module{&testModule{name: "test"}},
			protoCacheSize: 100,
			mainCacheSize:  50,
			expectErr:      false,
		},
		{
			name:           "No modules",
			modules:        []api.Module{},
			protoCacheSize: 0,
			mainCacheSize:  0,
			expectErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zap.NewNop()
			bus := &testEventBus{}
			cfg := Config{
				Modules:        tt.modules,
				ProtoCacheSize: tt.protoCacheSize,
				MainCacheSize:  tt.mainCacheSize,
			}

			cm, err := NewCodeManager(logger, bus, cfg)
			if tt.expectErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, cm)
			assert.NotNil(t, cm.memGraph)
			assert.NotNil(t, cm.compiler)
			assert.NotNil(t, cm.txNodes)
		})
	}
}

func TestManager_Transaction(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{})
	require.NoError(t, err)

	// Begin transaction
	cm.Begin(context.Background())
	assert.Empty(t, bus.events)

	// Add a node during transaction
	node := Node{
		ID:     registry.ID{NS: "test", Name: "node"},
		Kind:   api.KindFunction,
		Source: "function test() return 'hello' end",
		Method: "test",
	}
	err = cm.AddNode(context.Background(), node, nil)
	require.NoError(t, err)

	// Commit transaction
	cm.Commit(context.Background())
	assert.Len(t, bus.events, 1)
	assert.Equal(t, api.System, bus.events[0].System)
	assert.Equal(t, api.InvalidateNodes, bus.events[0].Kind)
	assert.Len(t, bus.events[0].Data.([]registry.ID), 1)

	// Discard transaction
	cm.Discard(context.Background())
	assert.Empty(t, cm.txNodes)
}

func TestManager_AddNode(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{})
	require.NoError(t, err)

	tests := []struct {
		name      string
		node      Node
		deps      []Import
		expectErr bool
	}{
		{
			name: "Add node without dependencies",
			node: Node{
				ID:     registry.ID{NS: "test", Name: "node1"},
				Kind:   api.KindFunction,
				Source: "function test1() return 'hello' end",
				Method: "test1",
			},
			deps:      nil,
			expectErr: false,
		},
		{
			name: "Add node with dependencies",
			node: Node{
				ID:     registry.ID{NS: "test", Name: "node2"},
				Kind:   api.KindFunction,
				Source: "function test2() return 'hello' end",
				Method: "test2",
			},
			deps: []Import{
				{
					ID:    registry.ID{NS: "test", Name: "node1"},
					Alias: "dep1",
				},
			},
			expectErr: false,
		},
		{
			name: "Add duplicate node",
			node: Node{
				ID:     registry.ID{NS: "test", Name: "node1"},
				Kind:   api.KindFunction,
				Source: "function test1() return 'hello' end",
				Method: "test1",
			},
			deps:      nil,
			expectErr: true,
		},
		{
			name: "Add node with non-existent dependency",
			node: Node{
				ID:     registry.ID{NS: "test", Name: "node3"},
				Kind:   api.KindFunction,
				Source: "function test3() return 'hello' end",
				Method: "test3",
			},
			deps: []Import{
				{
					ID:    registry.ID{NS: "test", Name: "non-existent"},
					Alias: "dep",
				},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cm.AddNode(context.Background(), tt.node, tt.deps)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Verify node exists
				_, err := cm.memGraph.GetNode(tt.node.ID)
				assert.NoError(t, err)
				// Verify dependencies
				if len(tt.deps) > 0 {
					deps, err := cm.memGraph.GetDirectDependencies(tt.node.ID)
					assert.NoError(t, err)
					assert.Len(t, deps, len(tt.deps))
				}
			}
		})
	}
}

func TestManager_UpdateNode(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{})
	require.NoError(t, err)

	// Add initial node
	node := Node{
		ID:     registry.ID{NS: "test", Name: "node"},
		Kind:   api.KindFunction,
		Source: "function test() return 'hello' end",
		Method: "test",
	}
	err = cm.AddNode(context.Background(), node, nil)
	require.NoError(t, err)

	tests := []struct {
		name      string
		node      Node
		deps      []Import
		expectErr bool
	}{
		{
			name: "Update node content",
			node: Node{
				ID:     registry.ID{NS: "test", Name: "node"},
				Kind:   api.KindFunction,
				Source: "function test() return 'world' end",
				Method: "test",
			},
			deps:      nil,
			expectErr: false,
		},
		{
			name: "Update node dependencies",
			node: Node{
				ID:     registry.ID{NS: "test", Name: "node"},
				Kind:   api.KindFunction,
				Source: "function test() return 'world' end",
				Method: "test",
			},
			deps: []Import{
				{
					ID:    registry.ID{NS: "test", Name: "dep"},
					Alias: "dep",
				},
			},
			expectErr: true, // Because dep node doesn't exist
		},
		{
			name: "Update non-existent node",
			node: Node{
				ID:     registry.ID{NS: "test", Name: "non-existent"},
				Kind:   api.KindFunction,
				Source: "function test() return 'world' end",
				Method: "test",
			},
			deps:      nil,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cm.UpdateNode(context.Background(), tt.node, tt.deps)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Verify node was updated
				updated, err := cm.memGraph.GetNode(tt.node.ID)
				assert.NoError(t, err)
				assert.Equal(t, tt.node.Source, updated.Source)
				assert.Equal(t, tt.node.Method, updated.Method)
			}
		})
	}
}

func TestManager_DeleteNode(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{})
	require.NoError(t, err)

	// Add a node
	node := Node{
		ID:     registry.ID{NS: "test", Name: "node"},
		Kind:   api.KindFunction,
		Source: "function test() return 'hello' end",
		Method: "test",
	}
	err = cm.AddNode(context.Background(), node, nil)
	require.NoError(t, err)

	tests := []struct {
		name      string
		id        registry.ID
		expectErr bool
	}{
		{
			name:      "Delete existing node",
			id:        node.ID,
			expectErr: false,
		},
		{
			name:      "Delete non-existent node",
			id:        registry.ID{NS: "test", Name: "non-existent"},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cm.DeleteNode(context.Background(), tt.id)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Verify node was deleted
				_, err := cm.memGraph.GetNode(tt.id)
				assert.Error(t, err)
			}
		})
	}
}

func TestManager_Compile(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{})
	require.NoError(t, err)

	// Add a node
	node := Node{
		ID:     registry.ID{NS: "test", Name: "node"},
		Kind:   api.KindFunction,
		Source: "function test() return 'hello' end",
		Method: "test",
	}
	err = cm.AddNode(context.Background(), node, nil)
	require.NoError(t, err)

	tests := []struct {
		name      string
		id        registry.ID
		options   *BuildOptions
		expectErr bool
	}{
		{
			name:      "Compile existing node",
			id:        node.ID,
			options:   &BuildOptions{},
			expectErr: false,
		},
		{
			name:      "Compile non-existent node",
			id:        registry.ID{NS: "test", Name: "non-existent"},
			options:   &BuildOptions{},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiled, err := cm.Compile(tt.id, tt.options)
			if tt.expectErr {
				assert.Error(t, err)
				assert.Nil(t, compiled)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, compiled)
				assert.NotNil(t, compiled.Main)
			}
		})
	}
}
