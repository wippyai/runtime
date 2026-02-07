package transport

import (
	"strings"

	"github.com/wippyai/go-lua/lsp/index"
	"github.com/wippyai/go-lua/types/diag"
)

// LSP Protocol Types

// Position represents a position in a text document.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range represents a range in a text document.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location represents a location inside a resource.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// TextDocumentIdentifier identifies a text document.
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// VersionedTextDocumentIdentifier identifies a versioned document.
type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

// TextDocumentItem represents a full document snapshot.
type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId,omitempty"`
	Text       string `json:"text"`
	Version    int    `json:"version"`
}

// TextDocumentContentChangeEvent represents a change event for full sync.
type TextDocumentContentChangeEvent struct {
	Text string `json:"text"`
}

// DidOpenTextDocumentParams is a parameter for didOpen.
type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// DidChangeTextDocumentParams is a parameter for didChange.
type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

// DidCloseTextDocumentParams is a parameter for didClose.
type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// TextDocumentPositionParams is a parameter for position-based requests.
type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// ReferenceContext controls reference search.
type ReferenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

// ReferenceParams is a parameter for finding references.
type ReferenceParams struct {
	TextDocumentPositionParams
	Context ReferenceContext `json:"context"`
}

// DocumentSymbolParams is a parameter for document symbols.
type DocumentSymbolParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// WorkspaceSymbolParams is a parameter for workspace symbols.
type WorkspaceSymbolParams struct {
	Query string `json:"query"`
}

// CompletionParams is a parameter for completion.
type CompletionParams struct {
	Context *CompletionContext `json:"context,omitempty"`
	TextDocumentPositionParams
}

// CompletionContext contains additional information about the completion request.
type CompletionContext struct {
	TriggerCharacter string `json:"triggerCharacter,omitempty"`
	TriggerKind      int    `json:"triggerKind"`
}

// Hover represents hover information.
type Hover struct {
	Range    *Range        `json:"range,omitempty"`
	Contents MarkupContent `json:"contents"`
}

// MarkupContent represents markdown content.
type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// CompletionItem represents a completion item.
type CompletionItem struct {
	Documentation any    `json:"documentation,omitempty"`
	Label         string `json:"label"`
	Detail        string `json:"detail,omitempty"`
	InsertText    string `json:"insertText,omitempty"`
	Kind          int    `json:"kind,omitempty"`
}

// SignatureHelp represents signature help information.
type SignatureHelp struct {
	Signatures      []SignatureInformation `json:"signatures"`
	ActiveSignature int                    `json:"activeSignature"`
	ActiveParameter int                    `json:"activeParameter"`
}

// SignatureInformation represents a function signature.
type SignatureInformation struct {
	Label         string                 `json:"label"`
	Documentation any                    `json:"documentation,omitempty"`
	Parameters    []ParameterInformation `json:"parameters,omitempty"`
}

// ParameterInformation represents a parameter.
type ParameterInformation struct {
	Label         any `json:"label"`
	Documentation any `json:"documentation,omitempty"`
}

// DocumentSymbol represents a symbol in a document.
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Children       []DocumentSymbol `json:"children,omitempty"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Kind           int              `json:"kind"`
}

// SymbolInformation represents a symbol in the workspace.
type SymbolInformation struct {
	Name          string   `json:"name"`
	ContainerName string   `json:"containerName,omitempty"`
	Location      Location `json:"location"`
	Kind          int      `json:"kind"`
}

// Call hierarchy types

// CallHierarchyItem represents a call hierarchy item.
type CallHierarchyItem struct {
	Data           any    `json:"data,omitempty"`
	Name           string `json:"name"`
	Detail         string `json:"detail,omitempty"`
	URI            string `json:"uri"`
	Tags           []int  `json:"tags,omitempty"`
	Range          Range  `json:"range"`
	SelectionRange Range  `json:"selectionRange"`
	Kind           int    `json:"kind"`
}

// CallHierarchyIncomingCallsParams is a parameter for incoming calls.
type CallHierarchyIncomingCallsParams struct {
	Item CallHierarchyItem `json:"item"`
}

// CallHierarchyOutgoingCallsParams is a parameter for outgoing calls.
type CallHierarchyOutgoingCallsParams struct {
	Item CallHierarchyItem `json:"item"`
}

// CallHierarchyIncomingCall represents an incoming call.
type CallHierarchyIncomingCall struct {
	FromRanges []Range           `json:"fromRanges"`
	From       CallHierarchyItem `json:"from"`
}

// CallHierarchyOutgoingCall represents an outgoing call.
type CallHierarchyOutgoingCall struct {
	FromRanges []Range           `json:"fromRanges"`
	To         CallHierarchyItem `json:"to"`
}

// Server capabilities

// InitializeResult is the result of initialize request.
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
}

// ServerCapabilities describes server capabilities.
type ServerCapabilities struct {
	CompletionProvider      *CompletionOptions      `json:"completionProvider,omitempty"`
	SignatureHelpProvider   *SignatureHelpOptions   `json:"signatureHelpProvider,omitempty"`
	DiagnosticProvider      *DiagnosticOptions      `json:"diagnosticProvider,omitempty"`
	TextDocumentSync        TextDocumentSyncOptions `json:"textDocumentSync,omitempty"`
	HoverProvider           bool                    `json:"hoverProvider,omitempty"`
	DefinitionProvider      bool                    `json:"definitionProvider,omitempty"`
	ReferencesProvider      bool                    `json:"referencesProvider,omitempty"`
	DocumentSymbolProvider  bool                    `json:"documentSymbolProvider,omitempty"`
	WorkspaceSymbolProvider bool                    `json:"workspaceSymbolProvider,omitempty"`
	CallHierarchyProvider   bool                    `json:"callHierarchyProvider,omitempty"`
}

// TextDocumentSyncOptions describes text document sync options.
type TextDocumentSyncOptions struct {
	OpenClose bool `json:"openClose,omitempty"`
	Change    int  `json:"change,omitempty"`
}

// CompletionOptions describes completion options.
type CompletionOptions struct {
	TriggerCharacters []string `json:"triggerCharacters,omitempty"`
	ResolveProvider   bool     `json:"resolveProvider,omitempty"`
}

// SignatureHelpOptions describes signature help options.
type SignatureHelpOptions struct {
	TriggerCharacters   []string `json:"triggerCharacters,omitempty"`
	RetriggerCharacters []string `json:"retriggerCharacters,omitempty"`
}

// DiagnosticOptions describes diagnostic provider options.
type DiagnosticOptions struct {
	Identifier            string `json:"identifier,omitempty"`
	InterFileDependencies bool   `json:"interFileDependencies"`
	WorkspaceDiagnostics  bool   `json:"workspaceDiagnostics"`
}

// DocumentDiagnosticParams is a parameter for textDocument/diagnostic.
type DocumentDiagnosticParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// Diagnostic represents a diagnostic item.
type Diagnostic struct {
	Message  string `json:"message"`
	Source   string `json:"source,omitempty"`
	Range    Range  `json:"range"`
	Severity int    `json:"severity,omitempty"`
}

// DiagnosticSeverity values.
const (
	DiagnosticSeverityError       = 1
	DiagnosticSeverityWarning     = 2
	DiagnosticSeverityInformation = 3
	DiagnosticSeverityHint        = 4
)

// DocumentDiagnosticReport is the result of textDocument/diagnostic.
type DocumentDiagnosticReport struct {
	Kind  string       `json:"kind"`
	Items []Diagnostic `json:"items"`
}

// DiagnosticReportKind values.
const (
	DiagnosticReportKindFull      = "full"
	DiagnosticReportKindUnchanged = "unchanged"
)

// Symbol kinds (LSP specification)
const (
	SymbolKindFile          = 1
	SymbolKindModule        = 2
	SymbolKindNamespace     = 3
	SymbolKindPackage       = 4
	SymbolKindClass         = 5
	SymbolKindMethod        = 6
	SymbolKindProperty      = 7
	SymbolKindField         = 8
	SymbolKindConstructor   = 9
	SymbolKindEnum          = 10
	SymbolKindInterface     = 11
	SymbolKindFunction      = 12
	SymbolKindVariable      = 13
	SymbolKindConstant      = 14
	SymbolKindString        = 15
	SymbolKindNumber        = 16
	SymbolKindBoolean       = 17
	SymbolKindArray         = 18
	SymbolKindObject        = 19
	SymbolKindKey           = 20
	SymbolKindNull          = 21
	SymbolKindEnumMember    = 22
	SymbolKindStruct        = 23
	SymbolKindEvent         = 24
	SymbolKindOperator      = 25
	SymbolKindTypeParameter = 26
)

// Helper functions for converting types

// DiagSpanToRange converts a diag.Span to an LSP Range.
func DiagSpanToRange(span diag.Span) Range {
	return Range{
		Start: Position{Line: max(0, span.StartLine-1), Character: max(0, span.StartCol-1)},
		End:   Position{Line: max(0, span.EndLine-1), Character: max(0, span.EndCol-1)},
	}
}

// ConvertDiagnosticSeverity converts diag.Severity to LSP severity.
func ConvertDiagnosticSeverity(severity diag.Severity) int {
	switch severity {
	case diag.SeverityError:
		return DiagnosticSeverityError
	case diag.SeverityWarning:
		return DiagnosticSeverityWarning
	case diag.SeverityHint:
		return DiagnosticSeverityHint
	default:
		return DiagnosticSeverityInformation
	}
}

// ConvertDiagnostics converts diag.Diagnostic slice to LSP Diagnostic slice.
func ConvertDiagnostics(diagnostics []diag.Diagnostic) []Diagnostic {
	result := make([]Diagnostic, 0, len(diagnostics))
	for _, d := range diagnostics {
		result = append(result, Diagnostic{
			Range:    DiagSpanToRange(d.Span),
			Message:  d.Message,
			Severity: ConvertDiagnosticSeverity(d.Severity),
			Source:   "wippy",
		})
	}
	return result
}

// ConvertIndexSymbolKind converts index.SymbolKind to LSP symbol kind.
func ConvertIndexSymbolKind(kind index.SymbolKind) int {
	switch kind {
	case index.SymbolFunction:
		return SymbolKindFunction
	case index.SymbolMethod:
		return SymbolKindMethod
	case index.SymbolVariable:
		return SymbolKindVariable
	case index.SymbolParameter:
		return SymbolKindVariable
	case index.SymbolType:
		return SymbolKindClass
	case index.SymbolField:
		return SymbolKindField
	default:
		return SymbolKindVariable
	}
}

// URI scheme constants
const (
	// URIScheme is the URI scheme for wippy registry IDs.
	URIScheme = "wippy"

	// uriSchemeSeparator is the separator between scheme and path.
	uriSchemeSeparator = "://"

	// URIPrefix is the full prefix for wippy URIs.
	URIPrefix = URIScheme + uriSchemeSeparator
)

// URIToID converts an LSP document URI to a registry ID string.
// Supports both "wippy://id" format and raw ID strings.
func URIToID(uri string) string {
	// Handle wippy:// scheme
	if strings.HasPrefix(uri, URIPrefix) {
		return strings.TrimPrefix(uri, URIPrefix)
	}
	if strings.Contains(uri, uriSchemeSeparator) {
		return ""
	}

	// Assume raw ID
	return uri
}

// IDToURI converts a registry ID string to an LSP document URI.
func IDToURI(id string) string {
	// Already a URI
	if strings.Contains(id, uriSchemeSeparator) {
		return id
	}
	return URIPrefix + id
}
