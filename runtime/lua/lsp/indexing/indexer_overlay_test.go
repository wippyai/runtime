// SPDX-License-Identifier: MPL-2.0

package indexing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	golualsp "github.com/wippyai/go-lua/lsp"
	"github.com/wippyai/go-lua/lsp/index"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"go.uber.org/zap"
)

func TestIndexer_UsesDocumentOverlay(t *testing.T) {
	log := zap.NewNop()
	bus := &testEventBus{}
	cm, err := code.NewCodeManager(log, bus, code.Config{})
	require.NoError(t, err)

	node := code.Node{
		ID:     registry.NewID("app", "overlay"),
		Kind:   luaapi.Function,
		Source: "return {}",
		Method: "handler",
	}
	require.NoError(t, cm.AddNode(context.Background(), node, nil))

	cache := index.NewDB()
	symbols := index.NewSymbolIndex()
	callGraph := index.NewCallGraph()
	lspSvc := golualsp.NewService(cache, symbols, callGraph)

	docs := NewDocumentStore()
	docs.Set(node.ID, "local = ", 1)

	idx := NewIndexer(log, NewManagerProvider(cm), lspSvc, symbols, callGraph, docs, nil)
	err = idx.IndexEntry(context.Background(), node.ID)
	require.Error(t, err, "expected parse error from overlay source")
}
