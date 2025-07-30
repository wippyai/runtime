package code

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

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
	mu     sync.RWMutex
}

func (b *testEventBus) Send(_ context.Context, e event.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
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

func (b *testEventBus) GetEvents() []event.Event {
	b.mu.RLock()
	defer b.mu.RUnlock()
	events := make([]event.Event, len(b.events))
	copy(events, b.events)
	return events
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
	assert.Empty(t, bus.GetEvents())

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
	assert.Len(t, bus.GetEvents(), 1)
	assert.Equal(t, api.System, bus.GetEvents()[0].System)
	assert.Equal(t, api.InvalidateNodes, bus.GetEvents()[0].Kind)
	assert.Len(t, bus.GetEvents()[0].Data.([]registry.ID), 1)

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

// TestManager_ConcurrentAddNode tests concurrent node additions
func TestManager_ConcurrentAddNode(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{})
	require.NoError(t, err)

	const numGoroutines = 10
	const numNodes = 50
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*numNodes)

	// Start multiple goroutines adding nodes concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < numNodes; j++ {
				node := Node{
					ID:     registry.ID{NS: "test", Name: fmt.Sprintf("node_%d_%d", goroutineID, j)},
					Kind:   api.KindFunction,
					Source: fmt.Sprintf("function test_%d_%d() return 'hello' end", goroutineID, j),
					Method: fmt.Sprintf("test_%d_%d", goroutineID, j),
				}
				if err := cm.AddNode(context.Background(), node, nil); err != nil {
					errors <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Errorf("Concurrent AddNode failed: %v", err)
	}

	// Verify all nodes were added
	for i := 0; i < numGoroutines; i++ {
		for j := 0; j < numNodes; j++ {
			nodeID := registry.ID{NS: "test", Name: fmt.Sprintf("node_%d_%d", i, j)}
			_, err := cm.memGraph.GetNode(nodeID)
			assert.NoError(t, err, "Node should exist after concurrent addition")
		}
	}
}

// TestManager_ConcurrentUpdateNode tests concurrent node updates
func TestManager_ConcurrentUpdateNode(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{})
	require.NoError(t, err)

	// Add initial nodes
	const numNodes = 10
	for i := 0; i < numNodes; i++ {
		node := Node{
			ID:     registry.ID{NS: "test", Name: fmt.Sprintf("node_%d", i)},
			Kind:   api.KindFunction,
			Source: fmt.Sprintf("function test_%d() return 'initial' end", i),
			Method: fmt.Sprintf("test_%d", i),
		}
		err := cm.AddNode(context.Background(), node, nil)
		require.NoError(t, err)
	}

	const numGoroutines = 5
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*numNodes)

	// Start multiple goroutines updating nodes concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < numNodes; j++ {
				node := Node{
					ID:     registry.ID{NS: "test", Name: fmt.Sprintf("node_%d", j)},
					Kind:   api.KindFunction,
					Source: fmt.Sprintf("function test_%d() return 'updated_%d' end", j, goroutineID),
					Method: fmt.Sprintf("test_%d", j),
				}
				if err := cm.UpdateNode(context.Background(), node, nil); err != nil {
					errors <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Errorf("Concurrent UpdateNode failed: %v", err)
	}

	// Verify all nodes were updated (last update wins)
	for i := 0; i < numNodes; i++ {
		nodeID := registry.ID{NS: "test", Name: fmt.Sprintf("node_%d", i)}
		node, err := cm.memGraph.GetNode(nodeID)
		assert.NoError(t, err)
		assert.Contains(t, node.Source, "updated_", "Node should be updated")
	}
}

// TestManager_ConcurrentTransactions tests concurrent transaction operations
func TestManager_ConcurrentTransactions(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{})
	require.NoError(t, err)

	const numGoroutines = 5
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	// Start multiple goroutines performing transactions concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			// Begin transaction
			cm.Begin(context.Background())

			// Add nodes during transaction
			for j := 0; j < 10; j++ {
				node := Node{
					ID:     registry.ID{NS: "test", Name: fmt.Sprintf("tx_node_%d_%d", goroutineID, j)},
					Kind:   api.KindFunction,
					Source: fmt.Sprintf("function test_%d_%d() return 'hello' end", goroutineID, j),
					Method: fmt.Sprintf("test_%d_%d", goroutineID, j),
				}
				if err := cm.AddNode(context.Background(), node, nil); err != nil {
					errors <- err
					return
				}
			}

			// Commit transaction
			cm.Commit(context.Background())
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Errorf("Concurrent transaction failed: %v", err)
	}

	// Verify all nodes were added
	for i := 0; i < numGoroutines; i++ {
		for j := 0; j < 10; j++ {
			nodeID := registry.ID{NS: "test", Name: fmt.Sprintf("tx_node_%d_%d", i, j)}
			_, err := cm.memGraph.GetNode(nodeID)
			assert.NoError(t, err, "Node should exist after transaction")
		}
	}
}

// TestManager_RaceConditionTxNodes tests for race conditions in txNodes access
func TestManager_RaceConditionTxNodes(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{})
	require.NoError(t, err)

	const numGoroutines = 10
	var wg sync.WaitGroup

	// Start goroutines that read and write txNodes concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			// Begin transaction
			cm.Begin(context.Background())

			// Add a node to trigger txNodes write
			node := Node{
				ID:     registry.ID{NS: "test", Name: fmt.Sprintf("race_node_%d", goroutineID)},
				Kind:   api.KindFunction,
				Source: fmt.Sprintf("function test_%d() return 'hello' end", goroutineID),
				Method: fmt.Sprintf("test_%d", goroutineID),
			}
			if err := cm.AddNode(context.Background(), node, nil); err != nil {
				t.Errorf("Failed to add node: %v", err)
				return
			}

			// Simulate some work
			time.Sleep(time.Millisecond)

			// Commit transaction
			cm.Commit(context.Background())
		}(i)
	}

	wg.Wait()

	// Verify no panic occurred and all nodes were added
	for i := 0; i < numGoroutines; i++ {
		nodeID := registry.ID{NS: "test", Name: fmt.Sprintf("race_node_%d", i)}
		_, err := cm.memGraph.GetNode(nodeID)
		assert.NoError(t, err, "Node should exist after race condition test")
	}
}

// TestManager_ConcurrentCompile tests concurrent compilation operations
func TestManager_ConcurrentCompile(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{})
	require.NoError(t, err)

	// Add nodes first
	const numNodes = 10
	for i := 0; i < numNodes; i++ {
		node := Node{
			ID:     registry.ID{NS: "test", Name: fmt.Sprintf("compile_node_%d", i)},
			Kind:   api.KindFunction,
			Source: fmt.Sprintf("function test_%d() return 'hello' end", i),
			Method: fmt.Sprintf("test_%d", i),
		}
		err := cm.AddNode(context.Background(), node, nil)
		require.NoError(t, err)
	}

	const numGoroutines = 5
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*numNodes)

	// Start multiple goroutines compiling nodes concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < numNodes; j++ {
				nodeID := registry.ID{NS: "test", Name: fmt.Sprintf("compile_node_%d", j)}
				_, err := cm.Compile(nodeID, &BuildOptions{})
				if err != nil {
					errors <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Errorf("Concurrent Compile failed: %v", err)
	}
}

// TestManager_TransactionIsolation tests transaction isolation
func TestManager_TransactionIsolation(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{})
	require.NoError(t, err)

	// Add initial node
	initialNode := Node{
		ID:     registry.ID{NS: "test", Name: "initial"},
		Kind:   api.KindFunction,
		Source: "function test() return 'initial' end",
		Method: "test",
	}
	err = cm.AddNode(context.Background(), initialNode, nil)
	require.NoError(t, err)

	// Begin transaction
	cm.Begin(context.Background())

	// Add node during transaction
	txNode := Node{
		ID:     registry.ID{NS: "test", Name: "transactional"},
		Kind:   api.KindFunction,
		Source: "function test() return 'transactional' end",
		Method: "test",
	}
	err = cm.AddNode(context.Background(), txNode, nil)
	require.NoError(t, err)

	// Verify node exists in graph (current implementation doesn't provide isolation)
	_, err = cm.memGraph.GetNode(txNode.ID)
	assert.NoError(t, err, "Node should exist in graph during transaction")

	// Discard transaction
	cm.Discard(context.Background())

	// Verify transactional node still exists (current implementation doesn't rollback)
	_, err = cm.memGraph.GetNode(txNode.ID)
	assert.NoError(t, err, "Transactional node should still exist after discard (no rollback implemented)")

	// Verify initial node still exists
	_, err = cm.memGraph.GetNode(initialNode.ID)
	assert.NoError(t, err, "Initial node should still exist after transaction discard")

	// Verify transaction tracking was cleared
	assert.Empty(t, cm.txNodes, "Transaction nodes should be cleared after discard")
}
