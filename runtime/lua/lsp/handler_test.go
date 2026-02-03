package lsp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	golualsp "github.com/wippyai/go-lua/lsp"
	"github.com/wippyai/go-lua/lsp/index"
)

func newTestHandler() *Handler {
	cache := index.NewDB()
	symbols := index.NewSymbolIndex()
	callGraph := index.NewCallGraph()
	lspSvc := golualsp.NewService(cache, symbols, callGraph)

	svc := &Service{
		lspService: lspSvc,
	}

	return &Handler{svc: svc}
}

// respError converts a response error to a standard error for use with require.NoError.
func respError(resp *Response) error {
	if resp.Error != nil {
		return resp.Error
	}
	return nil
}

// Error implements the error interface for ResponseError.
func (e *ResponseError) Error() string {
	return e.Message
}

func TestHandler_HandleNotification(t *testing.T) {
	h := &Handler{}

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
			req := &Request{Method: tt.method}
			resp := h.Handle(context.Background(), req)
			assert.Nil(t, resp, "notification should return nil response")
		})
	}
}

func TestHandler_HandleRequest_MethodNotFound(t *testing.T) {
	h := newTestHandler()

	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "unknown/method",
	}

	resp := h.Handle(context.Background(), req)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	assert.Equal(t, MethodNotFound, resp.Error.Code)
}

func TestHandler_HandleRequest_Timeout(t *testing.T) {
	h := newTestHandler()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	time.Sleep(5 * time.Millisecond)

	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "shutdown",
	}

	resp := h.Handle(ctx, req)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	assert.Equal(t, RequestCancelled, resp.Error.Code)
}

func TestHandler_Initialize(t *testing.T) {
	h := newTestHandler()

	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  json.RawMessage(`{}`),
	}

	resp := h.Handle(context.Background(), req)
	require.NotNil(t, resp)
	require.NoError(t, respError(resp))

	result, ok := resp.Result.(InitializeResult)
	require.True(t, ok, "expected InitializeResult")

	assert.True(t, result.Capabilities.HoverProvider)
	assert.True(t, result.Capabilities.DefinitionProvider)
	require.NotNil(t, result.Capabilities.CompletionProvider)
	require.NotEmpty(t, result.Capabilities.CompletionProvider.TriggerCharacters)
	assert.Equal(t, ".", result.Capabilities.CompletionProvider.TriggerCharacters[0])
}

func TestHandler_Shutdown(t *testing.T) {
	h := newTestHandler()

	req := &Request{
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
			req := &Request{
				JSONRPC: "2.0",
				ID:      1,
				Method:  tt.method,
				Params:  json.RawMessage(`{invalid json`),
			}

			resp := h.Handle(context.Background(), req)
			require.NotNil(t, resp)
			require.NotNil(t, resp.Error)
			assert.Equal(t, InvalidParams, resp.Error.Code)
		})
	}
}

func TestHandler_NilService(t *testing.T) {
	h := &Handler{svc: nil}

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
			req := &Request{
				JSONRPC: "2.0",
				ID:      1,
				Method:  method,
				Params:  json.RawMessage(`{}`),
			}

			resp := h.Handle(context.Background(), req)
			require.NotNil(t, resp)
			require.NotNil(t, resp.Error)
			assert.Equal(t, ServerNotInitialized, resp.Error.Code)
		})
	}
}

func TestHandler_HoverWithValidParams(t *testing.T) {
	h := newTestHandler()

	req := &Request{
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

	req := &Request{
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

	req := &Request{
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

	req := &Request{
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
			req := &Request{
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

	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "textDocument/hover",
		Params:  json.RawMessage(`{"textDocument":{"uri":"wippy://@test/module"},"position":{"line":0,"character":0}}`),
	}

	resp := h.Handle(ctx, req)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	assert.Equal(t, RequestCancelled, resp.Error.Code)
}

func TestHandler_NilLSPService(t *testing.T) {
	svc := &Service{lspService: nil}
	h := &Handler{svc: svc}

	methods := []struct {
		name   string
		params string
	}{
		{"callHierarchy/incomingCalls", `{"item":{"name":"test","kind":12,"uri":"wippy://@test/module","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"selectionRange":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}}}}`},
		{"callHierarchy/outgoingCalls", `{"item":{"name":"test","kind":12,"uri":"wippy://@test/module","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"selectionRange":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}}}}`},
	}

	for _, tt := range methods {
		t.Run(tt.name, func(t *testing.T) {
			req := &Request{
				JSONRPC: "2.0",
				ID:      1,
				Method:  tt.name,
				Params:  json.RawMessage(tt.params),
			}

			resp := h.Handle(context.Background(), req)
			require.NotNil(t, resp)
			require.NotNil(t, resp.Error)
			assert.Equal(t, ServerNotInitialized, resp.Error.Code)
		})
	}
}
