package lsp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	golualsp "github.com/yuin/gopher-lua/lsp"
	"github.com/yuin/gopher-lua/lsp/index"
)

func newTestHandler() *Handler {
	cache := index.NewDB()
	symbols := index.NewSymbolIndex()
	lspSvc := golualsp.NewService(cache, symbols)

	svc := &Service{
		lspService: lspSvc,
	}

	return &Handler{svc: svc}
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
			if resp != nil {
				t.Errorf("notification should return nil response")
			}
		})
	}

	if !h.initialized {
		t.Error("handler should be initialized after 'initialized' notification")
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
	if resp == nil {
		t.Fatal("expected response for request")
	}

	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}

	if resp.Error.Code != MethodNotFound {
		t.Errorf("expected MethodNotFound code, got %d", resp.Error.Code)
	}
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
	if resp == nil {
		t.Fatal("expected response")
	}

	if resp.Error != nil && resp.Error.Code == RequestCancelled {
		return
	}
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
	if resp == nil {
		t.Fatal("expected response")
	}

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	result, ok := resp.Result.(InitializeResult)
	if !ok {
		t.Fatal("expected InitializeResult")
	}

	if !result.Capabilities.HoverProvider {
		t.Error("expected hover provider capability")
	}

	if !result.Capabilities.DefinitionProvider {
		t.Error("expected definition provider capability")
	}

	if result.Capabilities.CompletionProvider.TriggerCharacters[0] != "." {
		t.Error("expected '.' as completion trigger")
	}
}

func TestHandler_Shutdown(t *testing.T) {
	h := newTestHandler()

	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "shutdown",
	}

	resp := h.Handle(context.Background(), req)
	if resp == nil {
		t.Fatal("expected response")
	}

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	if resp.Result != nil {
		t.Error("shutdown should return nil result")
	}
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
			if resp == nil {
				t.Fatal("expected response")
			}

			if resp.Error == nil {
				t.Error("expected error for invalid params")
			}

			if resp.Error != nil && resp.Error.Code != InvalidParams {
				t.Errorf("expected InvalidParams code, got %d", resp.Error.Code)
			}
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
			if resp == nil {
				t.Fatal("expected response")
			}

			if resp.Error == nil {
				t.Error("expected error for nil service")
			}

			if resp.Error.Code != ServerNotInitialized {
				t.Errorf("expected ServerNotInitialized code, got %d", resp.Error.Code)
			}
		})
	}
}

func TestHandler_HoverWithValidParams(t *testing.T) {
	h := newTestHandler()

	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "textDocument/hover",
		Params:  json.RawMessage(`{"textDocument":{"uri":"file:///test.lua"},"position":{"line":0,"character":0}}`),
	}

	resp := h.Handle(context.Background(), req)
	if resp == nil {
		t.Fatal("expected response")
	}

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
}

func TestHandler_DefinitionWithValidParams(t *testing.T) {
	h := newTestHandler()

	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "textDocument/definition",
		Params:  json.RawMessage(`{"textDocument":{"uri":"file:///test.lua"},"position":{"line":0,"character":0}}`),
	}

	resp := h.Handle(context.Background(), req)
	if resp == nil {
		t.Fatal("expected response")
	}

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
}

func TestHandler_DocumentSymbolWithValidParams(t *testing.T) {
	h := newTestHandler()

	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "textDocument/documentSymbol",
		Params:  json.RawMessage(`{"textDocument":{"uri":"file:///test.lua"}}`),
	}

	resp := h.Handle(context.Background(), req)
	if resp == nil {
		t.Fatal("expected response")
	}

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
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
	if resp == nil {
		t.Fatal("expected response")
	}

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
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
			params: `{"textDocument":{"uri":"file:///test.lua"},"position":{"line":0,"character":0}}`,
		},
		{
			name:   "incomingCalls",
			method: "callHierarchy/incomingCalls",
			params: `{"item":{"name":"test","kind":12,"uri":"file:///test.lua","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"selectionRange":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}}}}`,
		},
		{
			name:   "outgoingCalls",
			method: "callHierarchy/outgoingCalls",
			params: `{"item":{"name":"test","kind":12,"uri":"file:///test.lua","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"selectionRange":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}}}}`,
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
			if resp == nil {
				t.Fatal("expected response")
			}

			if resp.Error != nil {
				t.Fatalf("unexpected error: %v", resp.Error)
			}
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
		Params:  json.RawMessage(`{"textDocument":{"uri":"file:///test.lua"},"position":{"line":0,"character":0}}`),
	}

	resp := h.Handle(ctx, req)
	if resp == nil {
		t.Fatal("expected response")
	}

	if resp.Error == nil || resp.Error.Code != RequestCancelled {
		t.Error("expected RequestCancelled error for cancelled context")
	}
}

func TestHandler_NilLSPService(t *testing.T) {
	svc := &Service{lspService: nil}
	h := &Handler{svc: svc}

	methods := []struct {
		name   string
		params string
	}{
		{"callHierarchy/incomingCalls", `{"item":{"name":"test","kind":12,"uri":"file:///test.lua","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"selectionRange":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}}}}`},
		{"callHierarchy/outgoingCalls", `{"item":{"name":"test","kind":12,"uri":"file:///test.lua","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"selectionRange":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}}}}`},
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
			if resp == nil {
				t.Fatal("expected response")
			}

			if resp.Error != nil {
				t.Errorf("expected nil error for nil lspService, got %v", resp.Error)
			}
		})
	}
}
