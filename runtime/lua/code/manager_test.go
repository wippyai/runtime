// SPDX-License-Identifier: MPL-2.0

package code

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	glua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
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
		modules        []*api.ModuleDef
		protoCacheSize int
		mainCacheSize  int
		expectErr      bool
	}{
		{
			name:           "Default cache sizes",
			modules:        []*api.ModuleDef{{Name: "test"}},
			protoCacheSize: 0,
			mainCacheSize:  0,
			expectErr:      false,
		},
		{
			name:           "Custom cache sizes",
			modules:        []*api.ModuleDef{{Name: "test"}},
			protoCacheSize: 100,
			mainCacheSize:  50,
			expectErr:      false,
		},
		{
			name:           "No modules",
			modules:        []*api.ModuleDef{},
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
			assert.NotNil(t, cm.txAffected)
		})
	}
}

func TestManager_Transaction(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{})
	require.NoError(t, err)

	// Begin transaction
	require.NoError(t, cm.Begin(context.Background()))
	assert.Empty(t, bus.events)

	// Add a node during transaction
	node := Node{
		ID:     registry.NewID("", "testTx"),
		Kind:   api.Function,
		Source: "function test() return 'hello' end",
		Method: "test",
	}
	err = cm.AddNode(context.Background(), node, nil)
	require.NoError(t, err)

	// Commit transaction
	require.NoError(t, cm.Commit(context.Background()))
	assert.Len(t, bus.events, 1)
	assert.Equal(t, api.System, bus.events[0].System)
	assert.Equal(t, api.InvalidateNodes, bus.events[0].Kind)
	assert.Len(t, bus.events[0].Data.([]registry.ID), 1)

	// Discard transaction
	require.NoError(t, cm.Discard(context.Background()))
	assert.Empty(t, cm.txAffected)
}

func TestManager_TransactionDeleteInvalidatesDeletedNodeWithoutErrorLog(t *testing.T) {
	core, logs := observer.New(zapcore.ErrorLevel)
	logger := zap.New(core)
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{})
	require.NoError(t, err)

	id := registry.NewID("", "deleteTx")
	err = cm.AddNode(context.Background(), Node{
		ID:     id,
		Kind:   api.Function,
		Source: "function test() return 'hello' end",
		Method: "test",
	}, nil)
	require.NoError(t, err)
	cm.Commit(context.Background())
	bus.events = nil

	cm.Begin(context.Background())
	err = cm.DeleteNode(context.Background(), id)
	require.NoError(t, err)
	cm.Commit(context.Background())

	require.Empty(t, logs.All())
	require.Len(t, bus.events, 1)
	ids := bus.events[0].Data.([]registry.ID)
	require.Equal(t, []registry.ID{id}, ids)
}

func TestManager_TransactionAddThenDeleteInvalidatesWithoutErrorLog(t *testing.T) {
	core, logs := observer.New(zapcore.ErrorLevel)
	logger := zap.New(core)
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{})
	require.NoError(t, err)

	id := registry.NewID("", "addDeleteTx")
	cm.Begin(context.Background())
	err = cm.AddNode(context.Background(), Node{
		ID:     id,
		Kind:   api.Function,
		Source: "function test() return 'hello' end",
		Method: "test",
	}, nil)
	require.NoError(t, err)
	err = cm.DeleteNode(context.Background(), id)
	require.NoError(t, err)
	cm.Commit(context.Background())

	require.Empty(t, logs.All())
	require.Len(t, bus.events, 1)
	ids := bus.events[0].Data.([]registry.ID)
	require.Equal(t, []registry.ID{id}, ids)
}

func TestManager_TransactionUpdateInvalidatesDependents(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{})
	require.NoError(t, err)

	libID := registry.NewID("app", "lib")
	funcID := registry.NewID("app", "fn")

	err = cm.AddNode(context.Background(), Node{
		ID:     libID,
		Kind:   api.Library,
		Source: "local M = {}; return M",
	}, nil)
	require.NoError(t, err)
	err = cm.AddNode(context.Background(), Node{
		ID:     funcID,
		Kind:   api.Function,
		Source: "function handler() return true end",
		Method: "handler",
	}, []Import{{ID: libID, Alias: "lib"}})
	require.NoError(t, err)
	cm.Commit(context.Background())
	bus.events = nil

	cm.Begin(context.Background())
	err = cm.UpdateNode(context.Background(), Node{
		ID:     libID,
		Kind:   api.Library,
		Source: "local M = { value = 1 }; return M",
	}, nil)
	require.NoError(t, err)
	cm.Commit(context.Background())

	require.Len(t, bus.events, 1)
	ids := bus.events[0].Data.([]registry.ID)
	assert.ElementsMatch(t, []registry.ID{libID, funcID}, ids)
}

func TestManager_CommitWaitsForLuaInvalidationAcknowledgement(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	bus := eventbus.NewBus()
	awaitSvc := eventbus.NewAwaitService(bus)
	require.NoError(t, awaitSvc.Start(ctx))
	defer func() { require.NoError(t, awaitSvc.Stop()) }()
	ctx = event.WithAwaitService(ctx, awaitSvc)

	cm, err := NewCodeManager(zap.NewNop(), bus, Config{InvalidationWaitTimeout: time.Second})
	require.NoError(t, err)

	libID := registry.NewID("test", "lib")
	fnID := registry.NewID("test", "fn")
	require.NoError(t, cm.AddNode(ctx, Node{
		ID:     libID,
		Kind:   api.Library,
		Source: "return { value = 'v1' }",
	}, nil))
	require.NoError(t, cm.AddNode(ctx, Node{
		ID:     fnID,
		Kind:   api.Function,
		Source: "function handler() return lib.value end",
		Method: "handler",
	}, []Import{{ID: libID, Alias: "lib"}}))

	reqSeen := make(chan api.InvalidateNodesRequest, 1)
	releaseAck := make(chan struct{})
	sub, err := eventbus.NewSubscriber(ctx, bus, api.System, api.InvalidateNodes, func(evt event.Event) {
		req, ok := evt.Data.(api.InvalidateNodesRequest)
		if !ok {
			return
		}
		reqSeen <- req
		<-releaseAck
		for _, node := range req.Nodes {
			if node.ID.Equal(fnID) {
				bus.Send(ctx, event.Event{
					System: api.System,
					Kind:   api.InvalidateNodesAccept,
					Path:   req.AckPrefix + "/" + node.ID.String(),
				})
			}
		}
	})
	require.NoError(t, err)
	defer sub.Close()

	require.NoError(t, cm.Begin(ctx))
	require.NoError(t, cm.UpdateNode(ctx, Node{
		ID:     libID,
		Source: "return { value = 'v2' }",
	}, nil))

	done := make(chan error, 1)
	go func() {
		done <- cm.Commit(ctx)
	}()

	select {
	case req := <-reqSeen:
		require.NotEmpty(t, req.AckPrefix)
		assert.ElementsMatch(t, []api.InvalidateNode{
			{ID: libID, Kind: api.Library},
			{ID: fnID, Kind: api.Function},
		}, req.Nodes)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for lua invalidation request")
	}

	select {
	case err := <-done:
		require.NoError(t, err)
		t.Fatal("commit completed before callable invalidation acknowledgement")
	default:
	}

	close(releaseAck)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for commit after acknowledgement")
	}
}

func TestManager_CommitReturnsErrorWhenLuaInvalidationIsNotAcknowledged(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	bus := eventbus.NewBus()
	awaitSvc := eventbus.NewAwaitService(bus)
	require.NoError(t, awaitSvc.Start(ctx))
	defer func() { require.NoError(t, awaitSvc.Stop()) }()
	ctx = event.WithAwaitService(ctx, awaitSvc)

	cm, err := NewCodeManager(zap.NewNop(), bus, Config{InvalidationWaitTimeout: 10 * time.Millisecond})
	require.NoError(t, err)

	fnID := registry.NewID("test", "unacked_fn")
	require.NoError(t, cm.AddNode(ctx, Node{
		ID:     fnID,
		Kind:   api.Function,
		Source: "function handler() return 'ok' end",
		Method: "handler",
	}, nil))

	require.NoError(t, cm.Begin(ctx))
	require.NoError(t, cm.UpdateNode(ctx, Node{
		ID:     fnID,
		Source: "function handler() return 'updated' end",
		Method: "handler",
	}, nil))

	err = cm.Commit(ctx)
	require.Error(t, err)
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
				ID:     registry.NewID("", "test1"),
				Kind:   api.Function,
				Source: "function test1() return 'hello' end",
				Method: "test1",
			},
			deps:      nil,
			expectErr: false,
		},
		{
			name: "Add node with dependencies",
			node: Node{
				ID:     registry.NewID("", "test2"),
				Kind:   api.Function,
				Source: "function test2() return 'hello' end",
				Method: "test2",
			},
			deps: []Import{
				{
					ID:    registry.NewID("", "test1"),
					Alias: "dep1",
				},
			},
			expectErr: false,
		},
		{
			name: "Add duplicate node",
			node: Node{
				ID:     registry.NewID("", "test1"),
				Kind:   api.Function,
				Source: "function test1() return 'hello' end",
				Method: "test1",
			},
			deps:      nil,
			expectErr: true,
		},
		{
			name: "Add node with non-existent dependency",
			node: Node{
				ID:     registry.NewID("", "test3"),
				Kind:   api.Function,
				Source: "function test3() return 'hello' end",
				Method: "test3",
			},
			deps: []Import{
				{
					ID:    registry.NewID("", "nonExistent"),
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
		ID:     registry.NewID("", "testUpdate"),
		Kind:   api.Function,
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
				ID:     registry.NewID("", "testUpdate"),
				Kind:   api.Function,
				Source: "function test() return 'world' end",
				Method: "test",
			},
			deps:      nil,
			expectErr: false,
		},
		{
			name: "Update node dependencies",
			node: Node{
				ID:     registry.NewID("", "testUpdate"),
				Kind:   api.Function,
				Source: "function test() return 'world' end",
				Method: "test",
			},
			deps: []Import{
				{
					ID:    registry.NewID("", "nonExistentDep"),
					Alias: "dep",
				},
			},
			expectErr: true, // Because dep node doesn't exist
		},
		{
			name: "Update non-existent node",
			node: Node{
				ID:     registry.NewID("", "nonExistentUpdate"),
				Kind:   api.Function,
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

func TestManager_UpdateNodeFailureLeavesGraphUnchanged(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{
		ProtoCacheSize: 8,
		MainCacheSize:  8,
	})
	require.NoError(t, err)

	ctx := context.Background()
	mainID := registry.NewID("app.atomic", "main")
	oldDepID := registry.NewID("app.atomic", "old_dep")
	missingDepID := registry.NewID("app.atomic", "missing_dep")

	require.NoError(t, cm.AddNode(ctx, Node{
		ID:     oldDepID,
		Kind:   api.Library,
		Source: `return "old_dep"`,
	}, nil))
	require.NoError(t, cm.AddNode(ctx, Node{
		ID:     mainID,
		Kind:   api.Function,
		Source: `return "old"`,
		Method: "handler",
	}, []Import{{ID: oldDepID, Alias: "dep"}}))

	err = cm.UpdateNode(ctx, Node{
		ID:     mainID,
		Kind:   api.Function,
		Source: `return "new"`,
		Method: "handler",
	}, []Import{{ID: missingDepID, Alias: "dep"}})
	require.Error(t, err)

	got, err := cm.GetNode(mainID)
	require.NoError(t, err)
	require.Equal(t, `return "old"`, got.Source)

	deps, err := cm.GetDirectDependencies(mainID)
	require.NoError(t, err)
	require.Len(t, deps, 1)
	require.Equal(t, oldDepID, deps[0].ID)

	compiled, err := cm.Compile(mainID, nil)
	require.NoError(t, err)
	require.Equal(t, "old", executeCompiledString(t, compiled.Main))
}

func TestManager_DeleteNode(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{})
	require.NoError(t, err)

	// Add a node
	node := Node{
		ID:     registry.NewID("", "testDelete"),
		Kind:   api.Function,
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
			id:        registry.NewID("test", "non-existent"),
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
		ID:     registry.NewID("", "testCompile"),
		Kind:   api.Function,
		Source: "function test() return 'hello' end",
		Method: "test",
	}
	err = cm.AddNode(context.Background(), node, nil)
	require.NoError(t, err)

	tests := []struct {
		options   *BuildOptions
		id        registry.ID
		name      string
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
			id:        registry.NewID("test", "non-existent"),
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

func TestManager_DeleteThenAddSameIDInvalidatesCompileCaches(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{
		ProtoCacheSize: 8,
		MainCacheSize:  8,
	})
	require.NoError(t, err)

	id := registry.NewID("app.replay", "same_id")
	ctx := context.Background()

	err = cm.AddNode(ctx, Node{
		ID:     id,
		Kind:   api.Function,
		Source: `return "bad"`,
		Method: "handler",
	}, nil)
	require.NoError(t, err)

	first, err := cm.Compile(id, nil)
	require.NoError(t, err)
	require.Equal(t, "bad", executeCompiledString(t, first.Main))

	err = cm.DeleteNode(ctx, id)
	require.NoError(t, err)

	err = cm.AddNode(ctx, Node{
		ID:     id,
		Kind:   api.Function,
		Source: `return "good"`,
		Method: "handler",
	}, nil)
	require.NoError(t, err)

	second, err := cm.Compile(id, nil)
	require.NoError(t, err)
	require.Equal(t, "good", executeCompiledString(t, second.Main))
	require.NotSame(t, first, second)
	require.NotSame(t, first.Main, second.Main)
}

func TestManager_SameIDRecreateUsesNewRevisionEvenWithoutManualInvalidation(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{
		ProtoCacheSize: 8,
		MainCacheSize:  8,
	})
	require.NoError(t, err)

	id := registry.NewID("app.replay", "revision_tag")
	ctx := context.Background()

	require.NoError(t, cm.AddNode(ctx, Node{
		ID:     id,
		Kind:   api.Function,
		Source: `return "old"`,
		Method: "handler",
	}, nil))

	first, err := cm.Compile(id, nil)
	require.NoError(t, err)
	require.Equal(t, "old", executeCompiledString(t, first.Main))

	require.NoError(t, cm.memGraph.RemoveNode(id))
	require.NoError(t, cm.memGraph.AddNode(&Node{
		ID:      id,
		Kind:    api.Function,
		Source:  `return "new"`,
		Method:  "handler",
		Version: cm.nextVersion(HashNode(&Node{ID: id, Kind: api.Function, Source: `return "new"`, Method: "handler"})),
	}))

	second, err := cm.Compile(id, nil)
	require.NoError(t, err)
	require.Equal(t, "new", executeCompiledString(t, second.Main))
	require.NotSame(t, first.Main, second.Main)
}

func TestManager_UpdateInvalidatesDependentMainCacheByFingerprint(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{
		ProtoCacheSize: 8,
		MainCacheSize:  8,
	})
	require.NoError(t, err)

	ctx := context.Background()
	depID := registry.NewID("app.replay", "dep")
	mainID := registry.NewID("app.replay", "main")

	require.NoError(t, cm.AddNode(ctx, Node{
		ID:     depID,
		Kind:   api.Library,
		Source: `return "old"`,
		Method: "main",
	}, nil))
	require.NoError(t, cm.AddNode(ctx, Node{
		ID:     mainID,
		Kind:   api.Function,
		Source: `return dep`,
		Method: "handler",
	}, []Import{{ID: depID, Alias: "dep"}}))

	first, err := cm.Compile(mainID, nil)
	require.NoError(t, err)
	require.Equal(t, "old", executeCompiledString(t, first.Dependencies[0].Proto))

	require.NoError(t, cm.UpdateNode(ctx, Node{
		ID:     depID,
		Kind:   api.Library,
		Source: `return "new"`,
		Method: "main",
	}, nil))

	second, err := cm.Compile(mainID, nil)
	require.NoError(t, err)
	require.Equal(t, "new", executeCompiledString(t, second.Dependencies[0].Proto))
	require.NotSame(t, first, second)
	require.NotSame(t, first.Dependencies[0].Proto, second.Dependencies[0].Proto)
}

func TestBuildOptionsFingerprintSeparatesMainCache(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{
		ProtoCacheSize: 8,
		MainCacheSize:  8,
	})
	require.NoError(t, err)

	id := registry.NewID("app.replay", "options")
	allowed := registry.NewID("app.replay", "allowed")
	ctx := context.Background()

	require.NoError(t, cm.AddNode(ctx, Node{
		ID:     id,
		Kind:   api.Function,
		Source: `return "ok"`,
		Method: "handler",
	}, nil))

	first, err := cm.Compile(id, NewBuildOptions())
	require.NoError(t, err)

	second, err := cm.Compile(id, NewBuildOptions().WithMode(AllowListed).WithAllowed(id, allowed))
	require.NoError(t, err)

	require.NotSame(t, first, second)
	require.Equal(t, "ok", executeCompiledString(t, second.Main))
}

func TestManager_ConcurrentCompileAndUpdate(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{
		ProtoCacheSize: 16,
		MainCacheSize:  16,
	})
	require.NoError(t, err)

	id := registry.NewID("app.race", "compile_update")
	ctx := context.Background()

	require.NoError(t, cm.AddNode(ctx, Node{
		ID:     id,
		Kind:   api.Function,
		Source: `return "v0"`,
		Method: "handler",
	}, nil))

	start := make(chan struct{})
	done := make(chan struct{})
	errCh := make(chan error, 128)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for {
				select {
				case <-done:
					return
				default:
				}

				compiled, err := cm.Compile(id, nil)
				if err != nil {
					errCh <- err
					return
				}
				if compiled == nil || compiled.Main == nil {
					errCh <- assert.AnError
					return
				}
			}
		}()
	}

	close(start)
	for i := 1; i <= 100; i++ {
		source := fmt.Sprintf(`return "v%d"`, i%10)
		if err := cm.UpdateNode(ctx, Node{
			ID:     id,
			Kind:   api.Function,
			Source: source,
			Method: "handler",
		}, nil); err != nil {
			errCh <- err
			break
		}
	}
	close(done)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}
}

func executeCompiledString(t *testing.T, proto *glua.FunctionProto) string {
	t.Helper()

	l := glua.NewState()
	defer l.Close()

	fn := l.LoadProto(proto)
	l.Push(fn)
	require.NoError(t, l.PCall(0, 1, nil))

	got := l.Get(-1)
	l.Pop(1)
	return got.String()
}

func TestManager_Compile_TypeCallsFromManifest(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	typeCfg := DefaultTypeCheckConfig()
	typeCfg.Enabled = true
	cm, err := NewCodeManager(logger, bus, Config{TypeCheck: typeCfg})
	require.NoError(t, err)

	node := Node{
		ID:     registry.NewID("", "typeCall"),
		Kind:   api.Function,
		Source: "type MyStr = string\nreturn MyStr(\"ok\")",
		Method: "main",
	}
	err = cm.AddNode(context.Background(), node, nil)
	require.NoError(t, err)

	compiled, err := cm.Compile(node.ID, nil)
	require.NoError(t, err)
	require.NotNil(t, compiled.Main)
	if len(compiled.Main.TypeInfo) == 0 {
		t.Fatal("expected type info on compiled proto")
	}
	checkedNode, err := cm.memGraph.GetNode(node.ID)
	require.NoError(t, err)
	if checkedNode.Manifest == nil || checkedNode.Manifest.Types == nil {
		t.Fatal("expected manifest types on node")
	}
	if _, ok := checkedNode.Manifest.Types["MyStr"]; !ok {
		t.Fatalf("expected manifest to include MyStr, got %v", checkedNode.Manifest.Types)
	}

	l := glua.NewState()
	defer l.Close()
	fn := l.LoadProto(compiled.Main)
	l.Push(fn)
	err = l.PCall(0, 1, nil)
	require.NoError(t, err)
	if got := l.Get(-1).String(); got != "ok" {
		t.Fatalf("expected 'ok', got %q", got)
	}
}

func TestManager_AddNodeWithProto(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{})
	require.NoError(t, err)

	// Create a simple proto for testing
	proto := &glua.FunctionProto{
		NumParameters:    0,
		IsVarArg:         0,
		NumUpvalues:      0,
		NumUsedRegisters: 2,
	}

	tests := []struct {
		node      Node
		proto     *glua.FunctionProto
		name      string
		deps      []Import
		expectErr bool
	}{
		{
			name: "Add node with proto",
			node: Node{
				ID:     registry.NewID("", "bytecodeTest1"),
				Kind:   api.FunctionBytecode,
				Method: "handler",
			},
			deps:      nil,
			proto:     proto,
			expectErr: false,
		},
		{
			name: "Add node with proto and nil proto",
			node: Node{
				ID:     registry.NewID("", "bytecodeTest2"),
				Kind:   api.FunctionBytecode,
				Method: "handler",
			},
			deps:      nil,
			proto:     nil,
			expectErr: false,
		},
		{
			name: "Add duplicate node fails",
			node: Node{
				ID:     registry.NewID("", "bytecodeTest1"),
				Kind:   api.FunctionBytecode,
				Method: "handler",
			},
			deps:      nil,
			proto:     proto,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cm.AddNodeWithProto(context.Background(), tt.node, tt.deps, tt.proto)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Verify node exists
				node, err := cm.memGraph.GetNode(tt.node.ID)
				assert.NoError(t, err)
				assert.Equal(t, tt.node.Kind, node.Kind)
				assert.Equal(t, tt.node.Method, node.Method)
			}
		})
	}
}

func TestManager_UpdateNodeWithProto(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{})
	require.NoError(t, err)

	proto := &glua.FunctionProto{
		NumParameters:    0,
		IsVarArg:         0,
		NumUpvalues:      0,
		NumUsedRegisters: 2,
	}

	// Add initial node
	node := Node{
		ID:     registry.NewID("", "bytecodeUpdate"),
		Kind:   api.FunctionBytecode,
		Method: "handler",
	}
	err = cm.AddNodeWithProto(context.Background(), node, nil, proto)
	require.NoError(t, err)

	tests := []struct {
		node      Node
		proto     *glua.FunctionProto
		name      string
		deps      []Import
		expectErr bool
	}{
		{
			name: "Update node with new proto",
			node: Node{
				ID:     registry.NewID("", "bytecodeUpdate"),
				Kind:   api.FunctionBytecode,
				Method: "newHandler",
			},
			deps:      nil,
			proto:     proto,
			expectErr: false,
		},
		{
			name: "Update non-existent node fails",
			node: Node{
				ID:     registry.NewID("", "nonExistentBytecode"),
				Kind:   api.FunctionBytecode,
				Method: "handler",
			},
			deps:      nil,
			proto:     proto,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cm.UpdateNodeWithProto(context.Background(), tt.node, tt.deps, tt.proto)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Verify node was updated
				updated, err := cm.memGraph.GetNode(tt.node.ID)
				assert.NoError(t, err)
				assert.Equal(t, tt.node.Method, updated.Method)
			}
		})
	}
}

func TestManager_AddNodeWithProto_CompileUsesProto(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{})
	require.NoError(t, err)

	// Create a proto that we can identify
	proto := &glua.FunctionProto{
		NumParameters:    1,
		IsVarArg:         0,
		NumUpvalues:      0,
		NumUsedRegisters: 3,
	}

	node := Node{
		ID:     registry.NewID("", "bytecodeCompileTest"),
		Kind:   api.FunctionBytecode,
		Method: "execute",
	}

	err = cm.AddNodeWithProto(context.Background(), node, nil, proto)
	require.NoError(t, err)

	// Compile the node - should use the injected proto
	compiled, err := cm.Compile(node.ID, nil)
	require.NoError(t, err)
	require.NotNil(t, compiled)
	require.NotNil(t, compiled.Main)

	// Verify it's our injected proto
	assert.Equal(t, uint8(1), compiled.Main.NumParameters)
	assert.Equal(t, uint8(3), compiled.Main.NumUsedRegisters)
}

func TestManager_AddNodeWithProtoSameIDRecreateUsesNewRevision(t *testing.T) {
	logger := zap.NewNop()
	bus := &testEventBus{}
	cm, err := NewCodeManager(logger, bus, Config{
		ProtoCacheSize: 8,
		MainCacheSize:  8,
	})
	require.NoError(t, err)

	id := registry.NewID("app.replay", "bytecode_same_id")
	firstProto := &glua.FunctionProto{
		NumParameters:    1,
		IsVarArg:         0,
		NumUpvalues:      0,
		NumUsedRegisters: 3,
	}
	secondProto := &glua.FunctionProto{
		NumParameters:    2,
		IsVarArg:         0,
		NumUpvalues:      0,
		NumUsedRegisters: 4,
	}

	require.NoError(t, cm.AddNodeWithProto(context.Background(), Node{
		ID:     id,
		Kind:   api.FunctionBytecode,
		Method: "execute",
	}, nil, firstProto))

	first, err := cm.Compile(id, nil)
	require.NoError(t, err)
	require.Equal(t, uint8(1), first.Main.NumParameters)

	require.NoError(t, cm.DeleteNode(context.Background(), id))
	require.NoError(t, cm.AddNodeWithProto(context.Background(), Node{
		ID:     id,
		Kind:   api.FunctionBytecode,
		Method: "execute",
	}, nil, secondProto))

	second, err := cm.Compile(id, nil)
	require.NoError(t, err)
	require.Equal(t, uint8(2), second.Main.NumParameters)
	require.NotSame(t, first.Main, second.Main)
}
