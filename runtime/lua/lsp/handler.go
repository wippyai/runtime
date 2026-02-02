package lsp

import (
	"context"
	"encoding/json"
	"time"

	golualsp "github.com/wippyai/go-lua/lsp"
	"github.com/wippyai/go-lua/lsp/completion"
)

const requestTimeout = 30 * time.Second

const jsonRPCVersion = "2.0"

// TextDocumentSyncKind values
const (
	TextDocumentSyncKindNone        = 0
	TextDocumentSyncKindFull        = 1
	TextDocumentSyncKindIncremental = 2
)

// MarkupKind values
const MarkupKindMarkdown = "markdown"

// Code fence formatting
const (
	codeFenceStart = "```lua\n"
	codeFenceEnd   = "\n```"
)

// Trigger characters for completion and signature help
const (
	triggerDot   = "."
	triggerColon = ":"
	triggerParen = "("
	triggerComma = ","
)

// Error messages
const (
	errMsgRequestCancelled    = "request canceled"
	errMsgServiceNotAvailable = "service not available"
	errMsgLSPNotAvailable     = "lsp service not available"
	errMsgMethodNotFound      = "method not found: "
)

// Handler implements LSP method handlers.
type Handler struct {
	svc *Service
}

// NewHandler creates a new LSP handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
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
	case "initialized", "exit":
		// handled externally
	case "textDocument/didOpen", "textDocument/didChange", "textDocument/didClose":
		// document sync notifications - handled by event system
	}
}

func (h *Handler) handleRequest(ctx context.Context, req *Request) *Response {
	data, err := h.dispatch(ctx, req)
	if ctx.Err() != nil {
		return &Response{
			JSONRPC: jsonRPCVersion,
			ID:      req.ID,
			Error:   &ResponseError{Code: RequestCancelled, Message: errMsgRequestCancelled},
		}
	}
	return &Response{
		JSONRPC: jsonRPCVersion,
		ID:      req.ID,
		Result:  data,
		Error:   err,
	}
}

func (h *Handler) dispatch(ctx context.Context, req *Request) (any, *ResponseError) {
	select {
	case <-ctx.Done():
		return nil, &ResponseError{Code: RequestCancelled, Message: errMsgRequestCancelled}
	default:
	}

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
		return nil, &ResponseError{Code: MethodNotFound, Message: errMsgMethodNotFound + req.Method}
	}
}

// getLSPService returns the LSP service or an error if not available.
func (h *Handler) getLSPService() (*golualsp.Service, *ResponseError) {
	if h.svc == nil {
		return nil, &ResponseError{Code: ServerNotInitialized, Message: errMsgServiceNotAvailable}
	}
	lspSvc := h.svc.LSPService()
	if lspSvc == nil {
		return nil, &ResponseError{Code: ServerNotInitialized, Message: errMsgLSPNotAvailable}
	}
	return lspSvc, nil
}

// checkService returns an error if the service is not available.
func (h *Handler) checkService() *ResponseError {
	if h.svc == nil {
		return &ResponseError{Code: ServerNotInitialized, Message: errMsgServiceNotAvailable}
	}
	return nil
}

// invalidParams creates an InvalidParams error from a JSON parsing error.
func invalidParams(err error) *ResponseError {
	return &ResponseError{Code: InvalidParams, Message: err.Error()}
}

// LSP initialization

func (h *Handler) initialize(_ json.RawMessage) (any, *ResponseError) {
	return InitializeResult{
		Capabilities: ServerCapabilities{
			TextDocumentSync: TextDocumentSyncOptions{
				OpenClose: true,
				Change:    TextDocumentSyncKindFull,
			},
			HoverProvider:           true,
			DefinitionProvider:      true,
			ReferencesProvider:      true,
			DocumentSymbolProvider:  true,
			WorkspaceSymbolProvider: true,
			CompletionProvider:      &CompletionOptions{TriggerCharacters: []string{triggerDot, triggerColon}},
			SignatureHelpProvider:   &SignatureHelpOptions{TriggerCharacters: []string{triggerParen, triggerComma}},
			CallHierarchyProvider:   true,
		},
	}, nil
}

// Position-based queries

func (h *Handler) hover(params json.RawMessage) (any, *ResponseError) {
	lspSvc, err := h.getLSPService()
	if err != nil {
		return nil, err
	}

	var p TextDocumentPositionParams
	if jsonErr := json.Unmarshal(params, &p); jsonErr != nil {
		return nil, invalidParams(jsonErr)
	}

	fileID := URIToID(p.TextDocument.URI)
	result := lspSvc.HoverAt(fileID, p.Position.Line+1, p.Position.Character+1)
	if result == nil {
		return nil, nil
	}

	r := DiagSpanToRange(result.Span)
	return Hover{
		Contents: MarkupContent{
			Kind:  MarkupKindMarkdown,
			Value: codeFenceStart + result.Signature + codeFenceEnd,
		},
		Range: &r,
	}, nil
}

func (h *Handler) definition(params json.RawMessage) (any, *ResponseError) {
	lspSvc, err := h.getLSPService()
	if err != nil {
		return nil, err
	}

	var p TextDocumentPositionParams
	if jsonErr := json.Unmarshal(params, &p); jsonErr != nil {
		return nil, invalidParams(jsonErr)
	}

	fileID := URIToID(p.TextDocument.URI)
	result := lspSvc.DefinitionAt(fileID, p.Position.Line+1, p.Position.Character+1)
	if result == nil {
		return nil, nil
	}

	return Location{
		URI:   IDToURI(result.File),
		Range: DiagSpanToRange(result.Span),
	}, nil
}

func (h *Handler) references(params json.RawMessage) (any, *ResponseError) {
	lspSvc, err := h.getLSPService()
	if err != nil {
		return nil, err
	}

	var p ReferenceParams
	if jsonErr := json.Unmarshal(params, &p); jsonErr != nil {
		return nil, invalidParams(jsonErr)
	}

	fileID := URIToID(p.TextDocument.URI)
	results := lspSvc.ReferencesAt(
		fileID,
		p.Position.Line+1,
		p.Position.Character+1,
		p.Context.IncludeDeclaration,
	)

	locations := make([]Location, 0, len(results))
	for _, r := range results {
		locations = append(locations, Location{
			URI:   IDToURI(r.File),
			Range: DiagSpanToRange(r.Span),
		})
	}

	return locations, nil
}

// Symbol queries

func (h *Handler) documentSymbol(params json.RawMessage) (any, *ResponseError) {
	lspSvc, err := h.getLSPService()
	if err != nil {
		return nil, err
	}

	var p DocumentSymbolParams
	if jsonErr := json.Unmarshal(params, &p); jsonErr != nil {
		return nil, invalidParams(jsonErr)
	}

	fileID := URIToID(p.TextDocument.URI)
	results := lspSvc.DocumentSymbols(fileID)
	return convertDocumentSymbols(results), nil
}

func (h *Handler) workspaceSymbol(params json.RawMessage) (any, *ResponseError) {
	lspSvc, err := h.getLSPService()
	if err != nil {
		return nil, err
	}

	var p WorkspaceSymbolParams
	if jsonErr := json.Unmarshal(params, &p); jsonErr != nil {
		return nil, invalidParams(jsonErr)
	}

	results := lspSvc.WorkspaceSymbols(p.Query)
	symbols := make([]SymbolInformation, 0, len(results))
	for _, r := range results {
		symbols = append(symbols, SymbolInformation{
			Name: r.Name,
			Kind: ConvertIndexSymbolKind(r.Kind),
			Location: Location{
				URI:   IDToURI(r.File),
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
	if jsonErr := json.Unmarshal(params, &p); jsonErr != nil {
		return nil, invalidParams(jsonErr)
	}

	cp := h.svc.Completion()
	if cp == nil {
		return nil, nil
	}

	fileID := URIToID(p.TextDocument.URI)
	ctx := &completion.Context{
		File: fileID,
		Line: p.Position.Line + 1,
		Col:  p.Position.Character + 1,
	}
	if p.Context != nil && p.Context.TriggerCharacter != "" {
		ctx.Trigger = completion.TriggerCharacter
		ctx.TriggerChar = p.Context.TriggerCharacter
		if p.Context.TriggerCharacter == triggerDot || p.Context.TriggerCharacter == triggerColon {
			ctx.Kind = completion.ContextMember
		}
	}

	items := cp.Complete(ctx)
	result := make([]CompletionItem, 0, len(items))
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
	if jsonErr := json.Unmarshal(params, &p); jsonErr != nil {
		return nil, invalidParams(jsonErr)
	}

	sp := h.svc.Signature()
	if sp == nil {
		return nil, nil
	}

	fileID := URIToID(p.TextDocument.URI)
	sig := sp.Help(fileID, p.Position.Line+1, p.Position.Character+1)
	if sig == nil {
		return nil, nil
	}

	signatures := make([]SignatureInformation, 0, len(sig.Signatures))
	for _, s := range sig.Signatures {
		sigParams := make([]ParameterInformation, 0, len(s.Parameters))
		for _, param := range s.Parameters {
			sigParams = append(sigParams, ParameterInformation{Label: param.Label})
		}
		signatures = append(signatures, SignatureInformation{
			Label:      s.Label,
			Parameters: sigParams,
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
	lspSvc, err := h.getLSPService()
	if err != nil {
		return nil, err
	}

	var p TextDocumentPositionParams
	if jsonErr := json.Unmarshal(params, &p); jsonErr != nil {
		return nil, invalidParams(jsonErr)
	}

	fileID := URIToID(p.TextDocument.URI)
	result := lspSvc.HoverAt(fileID, p.Position.Line+1, p.Position.Character+1)
	if result == nil || result.Symbol == nil {
		return nil, nil
	}

	sym := result.Symbol
	return []CallHierarchyItem{{
		Name:           sym.Name,
		Kind:           SymbolKindFunction,
		URI:            IDToURI(sym.File),
		Range:          DiagSpanToRange(sym.DefSpan),
		SelectionRange: DiagSpanToRange(sym.DefSpan),
	}}, nil
}

func (h *Handler) incomingCalls(params json.RawMessage) (any, *ResponseError) {
	lspSvc, err := h.getLSPService()
	if err != nil {
		return nil, err
	}

	var p CallHierarchyIncomingCallsParams
	if jsonErr := json.Unmarshal(params, &p); jsonErr != nil {
		return nil, invalidParams(jsonErr)
	}

	cg := lspSvc.CallGraph()
	if cg == nil {
		return nil, nil
	}

	fileID := URIToID(p.Item.URI)
	edges := cg.CallersOf(fileID, p.Item.Name)
	results := make([]CallHierarchyIncomingCall, 0, len(edges))
	for _, edge := range edges {
		results = append(results, CallHierarchyIncomingCall{
			From: CallHierarchyItem{
				Name:           edge.CallerName,
				Kind:           SymbolKindFunction,
				URI:            IDToURI(edge.CallerFile),
				Range:          DiagSpanToRange(edge.CallerSpan),
				SelectionRange: DiagSpanToRange(edge.CallerSpan),
			},
			FromRanges: []Range{DiagSpanToRange(edge.CallSpan)},
		})
	}

	return results, nil
}

func (h *Handler) outgoingCalls(params json.RawMessage) (any, *ResponseError) {
	lspSvc, err := h.getLSPService()
	if err != nil {
		return nil, err
	}

	var p CallHierarchyOutgoingCallsParams
	if jsonErr := json.Unmarshal(params, &p); jsonErr != nil {
		return nil, invalidParams(jsonErr)
	}

	cg := lspSvc.CallGraph()
	if cg == nil {
		return nil, nil
	}

	fileID := URIToID(p.Item.URI)
	edges := cg.CalleesOf(fileID, p.Item.Name)
	results := make([]CallHierarchyOutgoingCall, 0, len(edges))
	for _, edge := range edges {
		results = append(results, CallHierarchyOutgoingCall{
			To: CallHierarchyItem{
				Name:           edge.CalleeName,
				Kind:           SymbolKindFunction,
				URI:            IDToURI(edge.CalleeFile),
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
	if len(syms) == 0 {
		return nil
	}
	result := make([]DocumentSymbol, 0, len(syms))
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
