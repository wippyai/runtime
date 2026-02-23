// SPDX-License-Identifier: MPL-2.0

package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	golualsp "github.com/wippyai/go-lua/lsp"
	"github.com/wippyai/go-lua/lsp/index"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/lsp/indexing"
	"github.com/wippyai/runtime/runtime/lua/lsp/transport"
)

func newTestHandler() *transport.Handler {
	cache := index.NewDB()
	symbols := index.NewSymbolIndex()
	callGraph := index.NewCallGraph()
	lspSvc := golualsp.NewService(cache, symbols, callGraph)

	svc := &Service{
		lspService: lspSvc,
	}

	return transport.NewHandler(svc)
}

// respError converts a response error to a standard error for use with require.NoError.
func respError(resp *transport.Response) error {
	if resp.Error != nil {
		return errors.New(resp.Error.Message)
	}
	return nil
}

func TestHandler_HandleNotification(t *testing.T) {
	h := transport.NewHandler(nil)

	tests := []struct {
		name   string
		method string
	}{
		{"initialized", "initialized"},
		{"exit", "exit"},
		{"didOpen", "textDocument/didOpen"},
		{"unknown", "unknown/method"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &transport.Request{Method: tt.method}
			resp := h.Handle(context.Background(), req)
			assert.Nil(t, resp, "notification should return nil response")
		})
	}
}

func TestHandler_HandleRequest_MethodNotFound(t *testing.T) {
	h := newTestHandler()

	req := &transport.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "unknown/method",
	}

	resp := h.Handle(context.Background(), req)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	assert.Equal(t, transport.MethodNotFound, resp.Error.Code)
}

func TestHandler_HandleRequest_Timeout(t *testing.T) {
	h := newTestHandler()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	time.Sleep(5 * time.Millisecond)

	req := &transport.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "shutdown",
	}

	resp := h.Handle(ctx, req)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	assert.Equal(t, transport.RequestCancelled, resp.Error.Code)
}

func TestHandler_Initialize(t *testing.T) {
	h := newTestHandler()

	req := &transport.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  json.RawMessage(`{}`),
	}

	resp := h.Handle(context.Background(), req)
	require.NotNil(t, resp)
	require.NoError(t, respError(resp))

	result, ok := resp.Result.(transport.InitializeResult)
	require.True(t, ok, "expected transport.InitializeResult")

	assert.True(t, result.Capabilities.HoverProvider)
	assert.True(t, result.Capabilities.DefinitionProvider)
	require.NotNil(t, result.Capabilities.CompletionProvider)
	require.NotEmpty(t, result.Capabilities.CompletionProvider.TriggerCharacters)
	assert.Equal(t, ".", result.Capabilities.CompletionProvider.TriggerCharacters[0])
}

func TestHandler_Shutdown(t *testing.T) {
	h := newTestHandler()

	req := &transport.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "shutdown",
	}

	resp := h.Handle(context.Background(), req)
	require.NotNil(t, resp)
	require.NoError(t, respError(resp))
	assert.Nil(t, resp.Result, "shutdown should return nil result")
}

func TestHandler_InvalidParams(t *testing.T) {
	h := newTestHandler()

	tests := []struct {
		name   string
		method string
	}{
		{"hover", "textDocument/hover"},
		{"definition", "textDocument/definition"},
		{"references", "textDocument/references"},
		{"documentSymbol", "textDocument/documentSymbol"},
		{"workspaceSymbol", "workspace/symbol"},
		{"completion", "textDocument/completion"},
		{"signatureHelp", "textDocument/signatureHelp"},
		{"prepareCallHierarchy", "textDocument/prepareCallHierarchy"},
		{"incomingCalls", "callHierarchy/incomingCalls"},
		{"outgoingCalls", "callHierarchy/outgoingCalls"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &transport.Request{
				JSONRPC: "2.0",
				ID:      1,
				Method:  tt.method,
				Params:  json.RawMessage(`{invalid json`),
			}

			resp := h.Handle(context.Background(), req)
			require.NotNil(t, resp)
			require.NotNil(t, resp.Error)
			assert.Equal(t, transport.InvalidParams, resp.Error.Code)
		})
	}
}

func TestHandler_NilService(t *testing.T) {
	h := transport.NewHandler(nil)

	methods := []string{
		"textDocument/hover",
		"textDocument/definition",
		"textDocument/references",
		"textDocument/documentSymbol",
		"workspace/symbol",
		"textDocument/completion",
		"textDocument/signatureHelp",
		"textDocument/prepareCallHierarchy",
		"callHierarchy/incomingCalls",
		"callHierarchy/outgoingCalls",
	}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := &transport.Request{
				JSONRPC: "2.0",
				ID:      1,
				Method:  method,
				Params:  json.RawMessage(`{}`),
			}

			resp := h.Handle(context.Background(), req)
			require.NotNil(t, resp)
			require.NotNil(t, resp.Error)
			assert.Equal(t, transport.ServerNotInitialized, resp.Error.Code)
		})
	}
}

func TestHandler_HoverWithValidParams(t *testing.T) {
	h := newTestHandler()

	req := &transport.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "textDocument/hover",
		Params:  json.RawMessage(`{"textDocument":{"uri":"wippy://@test/module"},"position":{"line":0,"character":0}}`),
	}

	resp := h.Handle(context.Background(), req)
	require.NotNil(t, resp)
	require.NoError(t, respError(resp))
}

func TestHandler_DefinitionWithValidParams(t *testing.T) {
	h := newTestHandler()

	req := &transport.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "textDocument/definition",
		Params:  json.RawMessage(`{"textDocument":{"uri":"wippy://@test/module"},"position":{"line":0,"character":0}}`),
	}

	resp := h.Handle(context.Background(), req)
	require.NotNil(t, resp)
	require.NoError(t, respError(resp))
}

func TestHandler_DocumentSymbolWithValidParams(t *testing.T) {
	h := newTestHandler()

	req := &transport.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "textDocument/documentSymbol",
		Params:  json.RawMessage(`{"textDocument":{"uri":"wippy://@test/module"}}`),
	}

	resp := h.Handle(context.Background(), req)
	require.NotNil(t, resp)
	require.NoError(t, respError(resp))
}

func TestHandler_WorkspaceSymbolWithValidParams(t *testing.T) {
	h := newTestHandler()

	req := &transport.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "workspace/symbol",
		Params:  json.RawMessage(`{"query":"test"}`),
	}

	resp := h.Handle(context.Background(), req)
	require.NotNil(t, resp)
	require.NoError(t, respError(resp))
}

func TestHandler_CallHierarchyWithValidParams(t *testing.T) {
	h := newTestHandler()

	tests := []struct {
		name   string
		method string
		params string
	}{
		{
			name:   "prepareCallHierarchy",
			method: "textDocument/prepareCallHierarchy",
			params: `{"textDocument":{"uri":"wippy://@test/module"},"position":{"line":0,"character":0}}`,
		},
		{
			name:   "incomingCalls",
			method: "callHierarchy/incomingCalls",
			params: `{"item":{"name":"test","kind":12,"uri":"wippy://@test/module","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"selectionRange":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}}}}`,
		},
		{
			name:   "outgoingCalls",
			method: "callHierarchy/outgoingCalls",
			params: `{"item":{"name":"test","kind":12,"uri":"wippy://@test/module","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"selectionRange":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &transport.Request{
				JSONRPC: "2.0",
				ID:      1,
				Method:  tt.method,
				Params:  json.RawMessage(tt.params),
			}

			resp := h.Handle(context.Background(), req)
			require.NotNil(t, resp)
			require.NoError(t, respError(resp))
		})
	}
}

func TestHandler_EarlyCancellation(t *testing.T) {
	h := newTestHandler()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := &transport.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "textDocument/hover",
		Params:  json.RawMessage(`{"textDocument":{"uri":"wippy://@test/module"},"position":{"line":0,"character":0}}`),
	}

	resp := h.Handle(ctx, req)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	assert.Equal(t, transport.RequestCancelled, resp.Error.Code)
}

func TestHandler_NilLSPService(t *testing.T) {
	svc := &Service{lspService: nil}
	h := transport.NewHandler(svc)

	methods := []struct {
		name   string
		params string
	}{
		{"callHierarchy/incomingCalls", `{"item":{"name":"test","kind":12,"uri":"wippy://@test/module","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"selectionRange":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}}}}`},
		{"callHierarchy/outgoingCalls", `{"item":{"name":"test","kind":12,"uri":"wippy://@test/module","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"selectionRange":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}}}}`},
	}

	for _, tt := range methods {
		t.Run(tt.name, func(t *testing.T) {
			req := &transport.Request{
				JSONRPC: "2.0",
				ID:      1,
				Method:  tt.name,
				Params:  json.RawMessage(tt.params),
			}

			resp := h.Handle(context.Background(), req)
			require.NotNil(t, resp)
			require.NotNil(t, resp.Error)
			assert.Equal(t, transport.ServerNotInitialized, resp.Error.Code)
		})
	}
}

func TestHandler_DocumentSyncLifecycle(t *testing.T) {
	svc := &Service{
		documents: indexing.NewDocumentStore(),
		running:   true,
	}
	h := transport.NewHandler(svc)

	openReq := &transport.Request{
		Method: "textDocument/didOpen",
		Params: json.RawMessage(`{"textDocument":{"uri":"wippy://app:test","version":1,"text":"return {}"}}`),
	}
	resp := h.Handle(context.Background(), openReq)
	require.Nil(t, resp)

	doc, ok := svc.documents.Get(registry.ParseID("app:test"))
	require.True(t, ok)
	assert.Equal(t, "return {}", doc.Text)
	assert.Equal(t, 1, doc.Version)

	changeReq := &transport.Request{
		Method: "textDocument/didChange",
		Params: json.RawMessage(`{"textDocument":{"uri":"wippy://app:test","version":2},"contentChanges":[{"text":"return {a=1}"}]}`),
	}
	resp = h.Handle(context.Background(), changeReq)
	require.Nil(t, resp)

	doc, ok = svc.documents.Get(registry.ParseID("app:test"))
	require.True(t, ok)
	assert.Equal(t, "return {a=1}", doc.Text)
	assert.Equal(t, 2, doc.Version)

	closeReq := &transport.Request{
		Method: "textDocument/didClose",
		Params: json.RawMessage(`{"textDocument":{"uri":"wippy://app:test"}}`),
	}
	resp = h.Handle(context.Background(), closeReq)
	require.Nil(t, resp)

	_, ok = svc.documents.Get(registry.ParseID("app:test"))
	assert.False(t, ok)
}

func TestHandler_DocumentSyncOpenResetsVersion(t *testing.T) {
	svc := &Service{
		documents: indexing.NewDocumentStore(),
		running:   true,
	}
	h := transport.NewHandler(svc)

	svc.documents.Set(registry.ParseID("app:test"), "return {old=true}", 10)

	openReq := &transport.Request{
		Method: "textDocument/didOpen",
		Params: json.RawMessage(`{"textDocument":{"uri":"wippy://app:test","version":1,"text":"return {new=true}"}}`),
	}
	resp := h.Handle(context.Background(), openReq)
	require.Nil(t, resp)

	doc, ok := svc.documents.Get(registry.ParseID("app:test"))
	require.True(t, ok)
	assert.Equal(t, "return {new=true}", doc.Text)
	assert.Equal(t, 1, doc.Version)
}
