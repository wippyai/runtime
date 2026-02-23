// SPDX-License-Identifier: MPL-2.0

package indexing

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	golualsp "github.com/wippyai/go-lua/lsp"
	"github.com/wippyai/go-lua/lsp/index"
	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"go.uber.org/zap"
)

type testEventBus struct{}

func (b *testEventBus) Send(_ context.Context, _ event.Event) {}
func (b *testEventBus) Subscribe(_ context.Context, _ event.System, _ chan<- event.Event) (event.SubscriberID, error) {
	return "test", nil
}
func (b *testEventBus) SubscribeP(_ context.Context, _ event.System, _ event.Kind, _ chan<- event.Event) (event.SubscriberID, error) {
	return "test", nil
}
func (b *testEventBus) Unsubscribe(_ context.Context, _ event.SubscriberID) {}

type testDiagStore struct {
	diagnostics map[string][]diag.Diagnostic
	mu          sync.Mutex
}

func (s *testDiagStore) StoreDiagnostics(fileID string, diagnostics []diag.Diagnostic) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.diagnostics == nil {
		s.diagnostics = make(map[string][]diag.Diagnostic)
	}
	if len(diagnostics) == 0 {
		delete(s.diagnostics, fileID)
	} else {
		s.diagnostics[fileID] = diagnostics
	}
}

func (s *testDiagStore) Get(fileID string) []diag.Diagnostic {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.diagnostics[fileID]
}

func newTestIndexer(t *testing.T) (*Indexer, *code.Manager) {
	t.Helper()

	log := zap.NewNop()
	bus := &testEventBus{}
	cm, err := code.NewCodeManager(log, bus, code.Config{})
	require.NoError(t, err)

	cache := index.NewDB()
	symbols := index.NewSymbolIndex()
	callGraph := index.NewCallGraph()
	lspSvc := golualsp.NewService(cache, symbols, callGraph)

	idx := NewIndexer(log, NewManagerProvider(cm), lspSvc, symbols, callGraph, nil, nil)
	return idx, cm
}

func TestIndexer_IndexEntry_IndexesSymbols(t *testing.T) {
	idx, cm := newTestIndexer(t)

	node := code.Node{
		ID:     registry.NewID("app", "index_ok"),
		Kind:   luaapi.Function,
		Source: "local x = 1\nlocal function foo() return x end\nreturn { foo = foo }",
		Method: "handler",
	}
	require.NoError(t, cm.AddNode(context.Background(), node, nil))
	require.NoError(t, idx.IndexEntry(context.Background(), node.ID))

	require.NotNil(t, idx.lspService.Manifests().Lookup(node.ID.String()))
}

func TestIndexer_IndexEntry_WithTypeErrorsStillIndexes(t *testing.T) {
	idx, cm := newTestIndexer(t)

	node := code.Node{
		ID:     registry.NewID("app", "index_type_error"),
		Kind:   luaapi.Function,
		Source: "local function foo(): string\n  return 1\nend\nreturn { foo = foo }",
		Method: "handler",
	}
	require.NoError(t, cm.AddNode(context.Background(), node, nil))
	require.NoError(t, idx.IndexEntry(context.Background(), node.ID))

	require.NotNil(t, idx.lspService.Manifests().Lookup(node.ID.String()))
}

func TestIndexer_IndexEntry_StoresDiagnostics(t *testing.T) {
	log := zap.NewNop()
	bus := &testEventBus{}
	cm, err := code.NewCodeManager(log, bus, code.Config{})
	require.NoError(t, err)

	cache := index.NewDB()
	symbols := index.NewSymbolIndex()
	callGraph := index.NewCallGraph()
	lspSvc := golualsp.NewService(cache, symbols, callGraph)

	diagStore := &testDiagStore{}
	idx := NewIndexer(log, NewManagerProvider(cm), lspSvc, symbols, callGraph, nil, diagStore)

	node := code.Node{
		ID:     registry.NewID("app", "with_error"),
		Kind:   luaapi.Function,
		Source: "local function foo(): string\n  return 1\nend\nreturn { foo = foo }",
		Method: "handler",
	}
	require.NoError(t, cm.AddNode(context.Background(), node, nil))
	require.NoError(t, idx.IndexEntry(context.Background(), node.ID))

	diags := diagStore.Get(node.ID.String())
	require.NotEmpty(t, diags, "expected diagnostics for type error")
	assert.Equal(t, diag.SeverityError, diags[0].Severity)
}

func TestIndexer_IndexEntry_ClearsDiagnosticsOnSuccess(t *testing.T) {
	log := zap.NewNop()
	bus := &testEventBus{}
	cm, err := code.NewCodeManager(log, bus, code.Config{})
	require.NoError(t, err)

	cache := index.NewDB()
	symbols := index.NewSymbolIndex()
	callGraph := index.NewCallGraph()
	lspSvc := golualsp.NewService(cache, symbols, callGraph)

	diagStore := &testDiagStore{}
	idx := NewIndexer(log, NewManagerProvider(cm), lspSvc, symbols, callGraph, nil, diagStore)

	node := code.Node{
		ID:     registry.NewID("app", "no_error"),
		Kind:   luaapi.Function,
		Source: "local function foo()\n  return 1\nend\nreturn { foo = foo }",
		Method: "handler",
	}
	require.NoError(t, cm.AddNode(context.Background(), node, nil))
	require.NoError(t, idx.IndexEntry(context.Background(), node.ID))

	diags := diagStore.Get(node.ID.String())
	assert.Empty(t, diags, "expected no diagnostics for valid code")
}

func TestIndexer_IndexEntry_InvalidatesMissingEntry(t *testing.T) {
	idx, cm := newTestIndexer(t)

	node := code.Node{
		ID:     registry.NewID("app", "delete_me"),
		Kind:   luaapi.Function,
		Source: "function foo() return 1 end\nreturn { foo = foo }",
		Method: "handler",
	}
	require.NoError(t, cm.AddNode(context.Background(), node, nil))
	require.NoError(t, idx.IndexEntry(context.Background(), node.ID))

	fileID := node.ID.String()
	require.NotNil(t, idx.symbols.LookupByName(fileID, "foo"))
	require.NotNil(t, idx.lspService.Manifests().Lookup(fileID))

	require.NoError(t, cm.DeleteNode(context.Background(), node.ID))

	_ = idx.IndexEntry(context.Background(), node.ID)

	assert.Nil(t, idx.symbols.LookupByName(fileID, "foo"))
	assert.Nil(t, idx.lspService.Manifests().Lookup(fileID))
}
