package lsp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	golualsp "github.com/wippyai/go-lua/lsp"
	"github.com/wippyai/go-lua/lsp/index"
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

	idx := NewIndexer(log, cm, lspSvc, symbols, callGraph)
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
