// SPDX-License-Identifier: MPL-2.0

package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	golualsp "github.com/wippyai/go-lua/lsp"
	"github.com/wippyai/go-lua/lsp/completion"
	lspindex "github.com/wippyai/go-lua/lsp/index"
	"github.com/wippyai/go-lua/lsp/signature"
	"github.com/wippyai/go-lua/types/typ"
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

// Service defines the LSP service surface required by the transport.
// Implementations should be safe for concurrent use by multiple connections.
type Service interface {
	LSPService() *golualsp.Service
	Completion() *completion.Provider
	Signature() *signature.Provider
	GetDiagnostics(id string) []Diagnostic
	DocumentText(id string) (string, bool)
	ResolveReceiverTypeAt(fileID string, line, col int) typ.Type
	ResolveLocalSymbolsAt(fileID string, line, col int) []*lspindex.Symbol
	EnsureIndexed(ctx context.Context, fileID string)
	ApplyDocumentOpen(id string, text string, version int)
	ApplyDocumentChange(id string, text string, version int)
	ApplyDocumentClose(id string)
}

// Handler implements LSP method handlers against a Service implementation.
type Handler struct {
	svc Service
}

// NewHandler creates a new LSP handler.
func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

// Handle dispatches a JSON-RPC request to the appropriate handler.
// It returns nil for notifications (requests without an ID).
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
	case "textDocument/didOpen":
		h.didOpen(req.Params)
	case "textDocument/didChange":
		h.didChange(req.Params)
	case "textDocument/didClose":
		h.didClose(req.Params)
	}
}

func (h *Handler) didOpen(params json.RawMessage) {
	if h.svc == nil {
		return
	}
	var p DidOpenTextDocumentParams
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	idStr, uriErr := fileIDFromURI(p.TextDocument.URI)
	if uriErr != nil {
		return
	}
	h.svc.ApplyDocumentOpen(idStr, p.TextDocument.Text, p.TextDocument.Version)
}

func (h *Handler) didChange(params json.RawMessage) {
	if h.svc == nil {
		return
	}
	var p DidChangeTextDocumentParams
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	if len(p.ContentChanges) == 0 {
		return
	}
	idStr, uriErr := fileIDFromURI(p.TextDocument.URI)
	if uriErr != nil {
		return
	}
	latest := p.ContentChanges[len(p.ContentChanges)-1]
	h.svc.ApplyDocumentChange(idStr, latest.Text, p.TextDocument.Version)
}

func (h *Handler) didClose(params json.RawMessage) {
	if h.svc == nil {
		return
	}
	var p DidCloseTextDocumentParams
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	idStr, uriErr := fileIDFromURI(p.TextDocument.URI)
	if uriErr != nil {
		return
	}
	h.svc.ApplyDocumentClose(idStr)
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
		return h.hover(ctx, req.Params)
	case "textDocument/definition":
		return h.definition(ctx, req.Params)
	case "textDocument/references":
		return h.references(ctx, req.Params)
	case "textDocument/documentSymbol":
		return h.documentSymbol(ctx, req.Params)
	case "workspace/symbol":
		return h.workspaceSymbol(req.Params)
	case "textDocument/completion":
		return h.completion(ctx, req.Params)
	case "textDocument/signatureHelp":
		return h.signatureHelp(ctx, req.Params)
	case "textDocument/diagnostic":
		return h.diagnostic(req.Params)
	case "callHierarchy/incomingCalls":
		return h.incomingCalls(req.Params)
	case "callHierarchy/outgoingCalls":
		return h.outgoingCalls(req.Params)
	case "textDocument/prepareCallHierarchy":
		return h.prepareCallHierarchy(ctx, req.Params)
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

func fileIDFromURI(uri string) (string, *ResponseError) {
	id := URIToID(uri)
	if id == "" {
		return "", invalidParams(fmt.Errorf("unsupported document uri: %s", uri))
	}
	return id, nil
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
			DiagnosticProvider:      &DiagnosticOptions{InterFileDependencies: true, WorkspaceDiagnostics: false},
			CallHierarchyProvider:   true,
		},
	}, nil
}

// Position-based queries

func (h *Handler) hover(ctx context.Context, params json.RawMessage) (any, *ResponseError) {
	lspSvc, err := h.getLSPService()
	if err != nil {
		return nil, err
	}

	var p TextDocumentPositionParams
	if jsonErr := json.Unmarshal(params, &p); jsonErr != nil {
		return nil, invalidParams(jsonErr)
	}

	fileID, uriErr := fileIDFromURI(p.TextDocument.URI)
	if uriErr != nil {
		return nil, uriErr
	}
	h.svc.EnsureIndexed(ctx, fileID)
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

func (h *Handler) definition(ctx context.Context, params json.RawMessage) (any, *ResponseError) {
	lspSvc, err := h.getLSPService()
	if err != nil {
		return nil, err
	}

	var p TextDocumentPositionParams
	if jsonErr := json.Unmarshal(params, &p); jsonErr != nil {
		return nil, invalidParams(jsonErr)
	}

	fileID, uriErr := fileIDFromURI(p.TextDocument.URI)
	if uriErr != nil {
		return nil, uriErr
	}
	h.svc.EnsureIndexed(ctx, fileID)
	result := lspSvc.DefinitionAt(fileID, p.Position.Line+1, p.Position.Character+1)
	if result == nil {
		return nil, nil
	}

	return Location{
		URI:   IDToURI(result.File),
		Range: DiagSpanToRange(result.Span),
	}, nil
}

func (h *Handler) references(ctx context.Context, params json.RawMessage) (any, *ResponseError) {
	lspSvc, err := h.getLSPService()
	if err != nil {
		return nil, err
	}

	var p ReferenceParams
	if jsonErr := json.Unmarshal(params, &p); jsonErr != nil {
		return nil, invalidParams(jsonErr)
	}

	fileID, uriErr := fileIDFromURI(p.TextDocument.URI)
	if uriErr != nil {
		return nil, uriErr
	}
	h.svc.EnsureIndexed(ctx, fileID)
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

func (h *Handler) documentSymbol(ctx context.Context, params json.RawMessage) (any, *ResponseError) {
	lspSvc, err := h.getLSPService()
	if err != nil {
		return nil, err
	}

	var p DocumentSymbolParams
	if jsonErr := json.Unmarshal(params, &p); jsonErr != nil {
		return nil, invalidParams(jsonErr)
	}

	fileID, uriErr := fileIDFromURI(p.TextDocument.URI)
	if uriErr != nil {
		return nil, uriErr
	}
	h.svc.EnsureIndexed(ctx, fileID)
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

func (h *Handler) completion(ctx context.Context, params json.RawMessage) (any, *ResponseError) {
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

	fileID, uriErr := fileIDFromURI(p.TextDocument.URI)
	if uriErr != nil {
		return nil, uriErr
	}
	h.svc.EnsureIndexed(ctx, fileID)
	compCtx := &completion.Context{
		File: fileID,
		Line: p.Position.Line + 1,
		Col:  p.Position.Character + 1,
	}
	if p.Context != nil && p.Context.TriggerCharacter != "" {
		compCtx.Trigger = completion.TriggerCharacter
		compCtx.TriggerChar = p.Context.TriggerCharacter
		if p.Context.TriggerCharacter == triggerDot || p.Context.TriggerCharacter == triggerColon {
			compCtx.Kind = completion.ContextMember
		}
	}
	if text, ok := h.svc.DocumentText(fileID); ok {
		lineText := lineAt(text, p.Position.Line)
		prefix, receiver := completionPrefixAndReceiver(lineText, p.Position.Character)
		compCtx.Prefix = prefix
		if receiver != "" && compCtx.Kind != completion.ContextMember {
			compCtx.Kind = completion.ContextMember
		}
		if compCtx.Kind == completion.ContextMember {
			if rt := h.svc.ResolveReceiverTypeAt(fileID, compCtx.Line, compCtx.Col); rt != nil {
				compCtx.ReceiverType = rt
			}
		} else if locals := h.svc.ResolveLocalSymbolsAt(fileID, compCtx.Line, compCtx.Col); len(locals) > 0 {
			compCtx.LocalSymbols = locals
		}
	}

	items := cp.Complete(compCtx)
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

func lineAt(text string, line int) string {
	if line < 0 {
		return ""
	}
	start := 0
	current := 0
	for i := 0; i <= len(text); i++ {
		if i == len(text) || text[i] == '\n' {
			if current == line {
				return strings.TrimRight(text[start:i], "\r")
			}
			current++
			start = i + 1
		}
	}
	return ""
}

func completionPrefixAndReceiver(line string, col int) (string, string) {
	if col < 0 {
		return "", ""
	}
	if col > len(line) {
		col = len(line)
	}
	start := col
	for start > 0 && isIdentChar(line[start-1]) {
		start--
	}
	prefix := line[start:col]
	if start == 0 {
		return prefix, ""
	}
	sep := line[start-1]
	if sep != '.' && sep != ':' {
		return prefix, ""
	}
	end := start - 1
	exprStart := end
	for exprStart > 0 {
		c := line[exprStart-1]
		if isIdentChar(c) || c == '.' || c == ':' {
			exprStart--
			continue
		}
		break
	}
	receiver := strings.TrimSpace(line[exprStart:end])
	return prefix, receiver
}

func isIdentChar(b byte) bool {
	if b == '_' {
		return true
	}
	if b >= 'a' && b <= 'z' {
		return true
	}
	if b >= 'A' && b <= 'Z' {
		return true
	}
	return b >= '0' && b <= '9'
}

func (h *Handler) signatureHelp(ctx context.Context, params json.RawMessage) (any, *ResponseError) {
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

	fileID, uriErr := fileIDFromURI(p.TextDocument.URI)
	if uriErr != nil {
		return nil, uriErr
	}
	h.svc.EnsureIndexed(ctx, fileID)
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

// Diagnostics

func (h *Handler) diagnostic(params json.RawMessage) (any, *ResponseError) {
	if err := h.checkService(); err != nil {
		return nil, err
	}

	var p DocumentDiagnosticParams
	if jsonErr := json.Unmarshal(params, &p); jsonErr != nil {
		return nil, invalidParams(jsonErr)
	}

	fileID, uriErr := fileIDFromURI(p.TextDocument.URI)
	if uriErr != nil {
		return nil, uriErr
	}

	items := h.svc.GetDiagnostics(fileID)
	return DocumentDiagnosticReport{
		Kind:  DiagnosticReportKindFull,
		Items: items,
	}, nil
}

// Call hierarchy

func (h *Handler) prepareCallHierarchy(ctx context.Context, params json.RawMessage) (any, *ResponseError) {
	lspSvc, err := h.getLSPService()
	if err != nil {
		return nil, err
	}

	var p TextDocumentPositionParams
	if jsonErr := json.Unmarshal(params, &p); jsonErr != nil {
		return nil, invalidParams(jsonErr)
	}

	fileID, uriErr := fileIDFromURI(p.TextDocument.URI)
	if uriErr != nil {
		return nil, uriErr
	}
	h.svc.EnsureIndexed(ctx, fileID)
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

	fileID, uriErr := fileIDFromURI(p.Item.URI)
	if uriErr != nil {
		return nil, uriErr
	}
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

	fileID, uriErr := fileIDFromURI(p.Item.URI)
	if uriErr != nil {
		return nil, uriErr
	}
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
