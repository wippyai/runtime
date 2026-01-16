package lsp

import (
	"context"
	"encoding/json"
	"time"

	golualsp "github.com/yuin/gopher-lua/lsp"
	"github.com/yuin/gopher-lua/lsp/completion"
	"go.uber.org/zap"
)

const requestTimeout = 30 * time.Second

// Handler implements LSP method handlers.
type Handler struct {
	svc         *Service
	log         *zap.Logger
	initialized bool
}

// NewHandler creates a new LSP handler.
func NewHandler(svc *Service, log *zap.Logger) *Handler {
	return &Handler{
		svc: svc,
		log: log.Named("handler"),
	}
}

// Handle dispatches a JSON-RPC request to the appropriate handler.
func (h *Handler) Handle(ctx context.Context, req *Request) *Response {
	if req.ID == nil {
		h.handleNotification(req)
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	return h.handleRequest(ctx, req)
}

func (h *Handler) handleNotification(req *Request) {
	switch req.Method {
	case "initialized":
		h.initialized = true
	case "exit":
		// handled externally
	case "textDocument/didOpen", "textDocument/didChange", "textDocument/didClose":
		// document sync notifications - handled by event system
	}
}

func (h *Handler) handleRequest(ctx context.Context, req *Request) *Response {
	select {
	case <-ctx.Done():
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &ResponseError{Code: RequestCancelled, Message: "request cancelled"},
		}
	default:
	}

	type result struct {
		data any
		err  *ResponseError
	}

	done := make(chan result, 1)
	go func() {
		data, err := h.dispatch(req)
		done <- result{data, err}
	}()

	select {
	case <-ctx.Done():
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &ResponseError{Code: RequestCancelled, Message: "request timeout"},
		}
	case r := <-done:
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  r.data,
			Error:   r.err,
		}
	}
}

func (h *Handler) dispatch(req *Request) (any, *ResponseError) {
	switch req.Method {
	case "initialize":
		return h.initialize(req.Params)
	case "shutdown":
		return nil, nil
	case "textDocument/hover":
		return h.hover(req.Params)
	case "textDocument/definition":
		return h.definition(req.Params)
	case "textDocument/references":
		return h.references(req.Params)
	case "textDocument/documentSymbol":
		return h.documentSymbol(req.Params)
	case "workspace/symbol":
		return h.workspaceSymbol(req.Params)
	case "textDocument/completion":
		return h.completion(req.Params)
	case "textDocument/signatureHelp":
		return h.signatureHelp(req.Params)
	case "callHierarchy/incomingCalls":
		return h.incomingCalls(req.Params)
	case "callHierarchy/outgoingCalls":
		return h.outgoingCalls(req.Params)
	case "textDocument/prepareCallHierarchy":
		return h.prepareCallHierarchy(req.Params)
	default:
		return nil, &ResponseError{Code: MethodNotFound, Message: "method not found: " + req.Method}
	}
}

// checkService returns an error if service is not available.
func (h *Handler) checkService() *ResponseError {
	if h.svc == nil {
		return &ResponseError{Code: ServerNotInitialized, Message: "service not available"}
	}
	return nil
}

// LSP initialization

func (h *Handler) initialize(_ json.RawMessage) (any, *ResponseError) {
	return InitializeResult{
		Capabilities: ServerCapabilities{
			TextDocumentSync: TextDocumentSyncOptions{
				OpenClose: true,
				Change:    1, // Full sync
			},
			HoverProvider:           true,
			DefinitionProvider:      true,
			ReferencesProvider:      true,
			DocumentSymbolProvider:  true,
			WorkspaceSymbolProvider: true,
			CompletionProvider:      &CompletionOptions{TriggerCharacters: []string{".", ":"}},
			SignatureHelpProvider:   &SignatureHelpOptions{TriggerCharacters: []string{"(", ","}},
			CallHierarchyProvider:   true,
		},
	}, nil
}

// Position-based queries

func (h *Handler) hover(params json.RawMessage) (any, *ResponseError) {
	if err := h.checkService(); err != nil {
		return nil, err
	}

	var p TextDocumentPositionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &ResponseError{Code: InvalidParams, Message: err.Error()}
	}

	result := h.svc.LSPService().HoverAt(p.TextDocument.URI, p.Position.Line+1, p.Position.Character+1)
	if result == nil {
		return nil, nil
	}

	r := DiagSpanToRange(result.Span)
	return Hover{
		Contents: MarkupContent{
			Kind:  "markdown",
			Value: "```lua\n" + result.Signature + "\n```",
		},
		Range: &r,
	}, nil
}

func (h *Handler) definition(params json.RawMessage) (any, *ResponseError) {
	if err := h.checkService(); err != nil {
		return nil, err
	}

	var p TextDocumentPositionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &ResponseError{Code: InvalidParams, Message: err.Error()}
	}

	result := h.svc.LSPService().DefinitionAt(p.TextDocument.URI, p.Position.Line+1, p.Position.Character+1)
	if result == nil {
		return nil, nil
	}

	return Location{
		URI:   result.File,
		Range: DiagSpanToRange(result.Span),
	}, nil
}

func (h *Handler) references(params json.RawMessage) (any, *ResponseError) {
	if err := h.checkService(); err != nil {
		return nil, err
	}

	var p ReferenceParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &ResponseError{Code: InvalidParams, Message: err.Error()}
	}

	results := h.svc.LSPService().ReferencesAt(
		p.TextDocument.URI,
		p.Position.Line+1,
		p.Position.Character+1,
		p.Context.IncludeDeclaration,
	)

	var locations []Location
	for _, r := range results {
		locations = append(locations, Location{
			URI:   r.File,
			Range: DiagSpanToRange(r.Span),
		})
	}

	return locations, nil
}

// Symbol queries

func (h *Handler) documentSymbol(params json.RawMessage) (any, *ResponseError) {
	if err := h.checkService(); err != nil {
		return nil, err
	}

	var p DocumentSymbolParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &ResponseError{Code: InvalidParams, Message: err.Error()}
	}

	results := h.svc.LSPService().DocumentSymbols(p.TextDocument.URI)
	return convertDocumentSymbols(results), nil
}

func (h *Handler) workspaceSymbol(params json.RawMessage) (any, *ResponseError) {
	if err := h.checkService(); err != nil {
		return nil, err
	}

	var p WorkspaceSymbolParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &ResponseError{Code: InvalidParams, Message: err.Error()}
	}

	results := h.svc.LSPService().WorkspaceSymbols(p.Query)
	var symbols []SymbolInformation
	for _, r := range results {
		symbols = append(symbols, SymbolInformation{
			Name: r.Name,
			Kind: ConvertIndexSymbolKind(r.Kind),
			Location: Location{
				URI:   r.File,
				Range: DiagSpanToRange(r.Span),
			},
		})
	}

	return symbols, nil
}

// Completion

func (h *Handler) completion(params json.RawMessage) (any, *ResponseError) {
	if err := h.checkService(); err != nil {
		return nil, err
	}

	var p CompletionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &ResponseError{Code: InvalidParams, Message: err.Error()}
	}

	cp := h.svc.Completion()
	if cp == nil {
		return nil, nil
	}

	ctx := &completion.Context{
		File: p.TextDocument.URI,
		Line: p.Position.Line + 1,
		Col:  p.Position.Character + 1,
	}
	if p.Context != nil && p.Context.TriggerCharacter != "" {
		ctx.Trigger = completion.TriggerCharacter
		ctx.TriggerChar = p.Context.TriggerCharacter
		if p.Context.TriggerCharacter == "." || p.Context.TriggerCharacter == ":" {
			ctx.Kind = completion.ContextMember
		}
	}

	items := cp.Complete(ctx)
	var result []CompletionItem
	for _, item := range items {
		result = append(result, CompletionItem{
			Label:      item.Label,
			Kind:       int(item.Kind),
			Detail:     item.Detail,
			InsertText: item.InsertText,
		})
	}

	return result, nil
}

func (h *Handler) signatureHelp(params json.RawMessage) (any, *ResponseError) {
	if err := h.checkService(); err != nil {
		return nil, err
	}

	var p TextDocumentPositionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &ResponseError{Code: InvalidParams, Message: err.Error()}
	}

	sp := h.svc.Signature()
	if sp == nil {
		return nil, nil
	}

	sig := sp.Help(p.TextDocument.URI, p.Position.Line+1, p.Position.Character+1)
	if sig == nil {
		return nil, nil
	}

	var signatures []SignatureInformation
	for _, s := range sig.Signatures {
		var params []ParameterInformation
		for _, param := range s.Parameters {
			params = append(params, ParameterInformation{Label: param.Label})
		}
		signatures = append(signatures, SignatureInformation{
			Label:      s.Label,
			Parameters: params,
		})
	}

	return SignatureHelp{
		Signatures:      signatures,
		ActiveSignature: sig.ActiveSignature,
		ActiveParameter: sig.ActiveParameter,
	}, nil
}

// Call hierarchy

func (h *Handler) prepareCallHierarchy(params json.RawMessage) (any, *ResponseError) {
	if err := h.checkService(); err != nil {
		return nil, err
	}

	var p TextDocumentPositionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &ResponseError{Code: InvalidParams, Message: err.Error()}
	}

	result := h.svc.LSPService().HoverAt(p.TextDocument.URI, p.Position.Line+1, p.Position.Character+1)
	if result == nil || result.Symbol == nil {
		return nil, nil
	}

	sym := result.Symbol
	return []CallHierarchyItem{{
		Name:           sym.Name,
		Kind:           SymbolKindFunction,
		URI:            sym.File,
		Range:          DiagSpanToRange(sym.DefSpan),
		SelectionRange: DiagSpanToRange(sym.DefSpan),
	}}, nil
}

func (h *Handler) incomingCalls(params json.RawMessage) (any, *ResponseError) {
	if err := h.checkService(); err != nil {
		return nil, err
	}

	var p CallHierarchyIncomingCallsParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &ResponseError{Code: InvalidParams, Message: err.Error()}
	}

	lspSvc := h.svc.LSPService()
	if lspSvc == nil {
		return nil, nil
	}
	cg := lspSvc.CallGraph()
	if cg == nil {
		return nil, nil
	}
	edges := cg.CallersOf(p.Item.URI, p.Item.Name)
	var results []CallHierarchyIncomingCall
	for _, edge := range edges {
		results = append(results, CallHierarchyIncomingCall{
			From: CallHierarchyItem{
				Name:           edge.CallerName,
				Kind:           SymbolKindFunction,
				URI:            edge.CallerFile,
				Range:          DiagSpanToRange(edge.CallerSpan),
				SelectionRange: DiagSpanToRange(edge.CallerSpan),
			},
			FromRanges: []Range{DiagSpanToRange(edge.CallSpan)},
		})
	}

	return results, nil
}

func (h *Handler) outgoingCalls(params json.RawMessage) (any, *ResponseError) {
	if err := h.checkService(); err != nil {
		return nil, err
	}

	var p CallHierarchyOutgoingCallsParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &ResponseError{Code: InvalidParams, Message: err.Error()}
	}

	lspSvc := h.svc.LSPService()
	if lspSvc == nil {
		return nil, nil
	}
	cg := lspSvc.CallGraph()
	if cg == nil {
		return nil, nil
	}
	edges := cg.CalleesOf(p.Item.URI, p.Item.Name)
	var results []CallHierarchyOutgoingCall
	for _, edge := range edges {
		results = append(results, CallHierarchyOutgoingCall{
			To: CallHierarchyItem{
				Name:           edge.CalleeName,
				Kind:           SymbolKindFunction,
				URI:            edge.CalleeFile,
				Range:          DiagSpanToRange(edge.CalleeSpan),
				SelectionRange: DiagSpanToRange(edge.CalleeSpan),
			},
			FromRanges: []Range{DiagSpanToRange(edge.CallSpan)},
		})
	}

	return results, nil
}

// Helper functions

func convertDocumentSymbols(syms []*golualsp.DocumentSymbol) []DocumentSymbol {
	var result []DocumentSymbol
	for _, s := range syms {
		ds := DocumentSymbol{
			Name:           s.Name,
			Kind:           ConvertIndexSymbolKind(s.Kind),
			Range:          DiagSpanToRange(s.Span),
			SelectionRange: DiagSpanToRange(s.Span),
		}
		if len(s.Children) > 0 {
			ds.Children = convertDocumentSymbols(s.Children)
		}
		result = append(result, ds)
	}
	return result
}
