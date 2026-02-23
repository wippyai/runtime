// SPDX-License-Identifier: MPL-2.0

package transport

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	golualsp "github.com/wippyai/go-lua/lsp"
	"github.com/wippyai/go-lua/lsp/completion"
	"github.com/wippyai/go-lua/lsp/index"
	"github.com/wippyai/go-lua/lsp/signature"
	"github.com/wippyai/go-lua/types/typ"
)

type testDoc struct {
	Text    string
	Version int
}

type testService struct {
	indexCtx    context.Context
	lsp         *golualsp.Service
	completion  *completion.Provider
	signature   *signature.Provider
	docs        map[string]testDoc
	diagnostics map[string][]Diagnostic
	indexCalls  int
	mu          sync.Mutex
}

func (s *testService) LSPService() *golualsp.Service {
	return s.lsp
}

func (s *testService) Completion() *completion.Provider {
	return s.completion
}

func (s *testService) Signature() *signature.Provider {
	return s.signature
}

func (s *testService) GetDiagnostics(id string) []Diagnostic {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.diagnostics == nil {
		return nil
	}
	return s.diagnostics[id]
}

func (s *testService) DocumentText(id string) (string, bool) {
	doc, ok := s.getDoc(id)
	if !ok {
		return "", false
	}
	return doc.Text, true
}

func (s *testService) ResolveReceiverTypeAt(fileID string, line, col int) typ.Type {
	return nil
}

func (s *testService) ResolveLocalSymbolsAt(fileID string, line, col int) []*index.Symbol {
	return nil
}

func (s *testService) EnsureIndexed(ctx context.Context, fileID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.indexCalls++
	s.indexCtx = ctx
}

func (s *testService) ApplyDocumentOpen(id string, text string, version int) {
	s.setDoc(id, text, version)
}

func (s *testService) ApplyDocumentChange(id string, text string, version int) {
	s.setDoc(id, text, version)
}

func (s *testService) ApplyDocumentClose(id string) {
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.docs, id)
}

func (s *testService) setDoc(id string, text string, version int) {
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.docs == nil {
		s.docs = make(map[string]testDoc)
	}
	s.docs[id] = testDoc{Text: text, Version: version}
}

func (s *testService) getDoc(id string) (testDoc, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, ok := s.docs[id]
	return doc, ok
}

func newTestHandler() *Handler {
	cache := index.NewDB()
	symbols := index.NewSymbolIndex()
	callGraph := index.NewCallGraph()
	lspSvc := golualsp.NewService(cache, symbols, callGraph)

	svc := &testService{
		lsp:  lspSvc,
		docs: make(map[string]testDoc),
	}

	return NewHandler(svc)
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
		{"diagnostic", "textDocument/diagnostic"},
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
		"textDocument/diagnostic",
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

func TestHandler_HoverPassesRequestContextToEnsureIndexed(t *testing.T) {
	h := newTestHandler()
	svc, ok := h.svc.(*testService)
	require.True(t, ok)

	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "textDocument/hover",
		Params:  json.RawMessage(`{"textDocument":{"uri":"wippy://@test/module"},"position":{"line":0,"character":0}}`),
	}

	resp := h.Handle(context.Background(), req)
	require.NotNil(t, resp)
	require.NoError(t, respError(resp))

	svc.mu.Lock()
	defer svc.mu.Unlock()
	assert.Equal(t, 1, svc.indexCalls)
	require.NotNil(t, svc.indexCtx)
	_, hasDeadline := svc.indexCtx.Deadline()
	assert.True(t, hasDeadline)
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
	svc := &testService{lsp: nil}
	h := NewHandler(svc)

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

func TestHandler_DocumentSyncLifecycle(t *testing.T) {
	svc := &testService{docs: make(map[string]testDoc)}
	h := NewHandler(svc)

	openReq := &Request{
		Method: "textDocument/didOpen",
		Params: json.RawMessage(`{"textDocument":{"uri":"wippy://app:test","version":1,"text":"return {}"}}`),
	}
	resp := h.Handle(context.Background(), openReq)
	require.Nil(t, resp)

	doc, ok := svc.getDoc("app:test")
	require.True(t, ok)
	assert.Equal(t, "return {}", doc.Text)
	assert.Equal(t, 1, doc.Version)

	changeReq := &Request{
		Method: "textDocument/didChange",
		Params: json.RawMessage(`{"textDocument":{"uri":"wippy://app:test","version":2},"contentChanges":[{"text":"return {a=1}"}]}`),
	}
	resp = h.Handle(context.Background(), changeReq)
	require.Nil(t, resp)

	doc, ok = svc.getDoc("app:test")
	require.True(t, ok)
	assert.Equal(t, "return {a=1}", doc.Text)
	assert.Equal(t, 2, doc.Version)

	closeReq := &Request{
		Method: "textDocument/didClose",
		Params: json.RawMessage(`{"textDocument":{"uri":"wippy://app:test"}}`),
	}
	resp = h.Handle(context.Background(), closeReq)
	require.Nil(t, resp)

	_, ok = svc.getDoc("app:test")
	assert.False(t, ok)
}

func TestHandler_DiagnosticWithValidParams(t *testing.T) {
	h := newTestHandler()

	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "textDocument/diagnostic",
		Params:  json.RawMessage(`{"textDocument":{"uri":"wippy://@test/module"}}`),
	}

	resp := h.Handle(context.Background(), req)
	require.NotNil(t, resp)
	require.NoError(t, respError(resp))

	result, ok := resp.Result.(DocumentDiagnosticReport)
	require.True(t, ok, "expected DocumentDiagnosticReport")
	assert.Equal(t, DiagnosticReportKindFull, result.Kind)
}

func TestHandler_DiagnosticWithStoredDiagnostics(t *testing.T) {
	cache := index.NewDB()
	symbols := index.NewSymbolIndex()
	callGraph := index.NewCallGraph()
	lspSvc := golualsp.NewService(cache, symbols, callGraph)

	svc := &testService{
		lsp:  lspSvc,
		docs: make(map[string]testDoc),
		diagnostics: map[string][]Diagnostic{
			"app:test": {
				{
					Range:    Range{Start: Position{Line: 0, Character: 5}, End: Position{Line: 0, Character: 10}},
					Message:  "type error: expected string, got number",
					Severity: DiagnosticSeverityError,
					Source:   "wippy",
				},
				{
					Range:    Range{Start: Position{Line: 2, Character: 0}, End: Position{Line: 2, Character: 15}},
					Message:  "unused variable 'x'",
					Severity: DiagnosticSeverityWarning,
					Source:   "wippy",
				},
			},
		},
	}

	h := NewHandler(svc)

	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "textDocument/diagnostic",
		Params:  json.RawMessage(`{"textDocument":{"uri":"wippy://app:test"}}`),
	}

	resp := h.Handle(context.Background(), req)
	require.NotNil(t, resp)
	require.NoError(t, respError(resp))

	result, ok := resp.Result.(DocumentDiagnosticReport)
	require.True(t, ok, "expected DocumentDiagnosticReport")
	assert.Equal(t, DiagnosticReportKindFull, result.Kind)
	require.Len(t, result.Items, 2)

	assert.Equal(t, "type error: expected string, got number", result.Items[0].Message)
	assert.Equal(t, DiagnosticSeverityError, result.Items[0].Severity)
	assert.Equal(t, "unused variable 'x'", result.Items[1].Message)
	assert.Equal(t, DiagnosticSeverityWarning, result.Items[1].Severity)
}

func TestHandler_InitializeIncludesDiagnosticProvider(t *testing.T) {
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
	require.NotNil(t, result.Capabilities.DiagnosticProvider)
	assert.True(t, result.Capabilities.DiagnosticProvider.InterFileDependencies)
}
