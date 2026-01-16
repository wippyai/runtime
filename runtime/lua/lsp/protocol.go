package lsp

import (
	"github.com/yuin/gopher-lua/lsp/index"
	"github.com/yuin/gopher-lua/types/diag"
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
	TextDocumentPositionParams
	Context *CompletionContext `json:"context,omitempty"`
}

// CompletionContext contains additional information about the completion request.
type CompletionContext struct {
	TriggerKind      int    `json:"triggerKind"`
	TriggerCharacter string `json:"triggerCharacter,omitempty"`
}

// Hover represents hover information.
type Hover struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

// MarkupContent represents markdown content.
type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// CompletionItem represents a completion item.
type CompletionItem struct {
	Label         string `json:"label"`
	Kind          int    `json:"kind,omitempty"`
	Detail        string `json:"detail,omitempty"`
	Documentation any    `json:"documentation,omitempty"`
	InsertText    string `json:"insertText,omitempty"`
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
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// SymbolInformation represents a symbol in the workspace.
type SymbolInformation struct {
	Name          string   `json:"name"`
	Kind          int      `json:"kind"`
	Location      Location `json:"location"`
	ContainerName string   `json:"containerName,omitempty"`
}

// Call hierarchy types

// CallHierarchyItem represents a call hierarchy item.
type CallHierarchyItem struct {
	Name           string `json:"name"`
	Kind           int    `json:"kind"`
	Tags           []int  `json:"tags,omitempty"`
	Detail         string `json:"detail,omitempty"`
	URI            string `json:"uri"`
	Range          Range  `json:"range"`
	SelectionRange Range  `json:"selectionRange"`
	Data           any    `json:"data,omitempty"`
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
	From       CallHierarchyItem `json:"from"`
	FromRanges []Range           `json:"fromRanges"`
}

// CallHierarchyOutgoingCall represents an outgoing call.
type CallHierarchyOutgoingCall struct {
	To         CallHierarchyItem `json:"to"`
	FromRanges []Range           `json:"fromRanges"`
}

// Server capabilities

// InitializeResult is the result of initialize request.
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
}

// ServerCapabilities describes server capabilities.
type ServerCapabilities struct {
	TextDocumentSync        TextDocumentSyncOptions `json:"textDocumentSync,omitempty"`
	HoverProvider           bool                    `json:"hoverProvider,omitempty"`
	CompletionProvider      *CompletionOptions      `json:"completionProvider,omitempty"`
	SignatureHelpProvider   *SignatureHelpOptions   `json:"signatureHelpProvider,omitempty"`
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
	startLine := span.StartLine - 1
	if startLine < 0 {
		startLine = 0
	}
	startCol := span.StartCol - 1
	if startCol < 0 {
		startCol = 0
	}
	endLine := span.EndLine - 1
	if endLine < 0 {
		endLine = 0
	}
	endCol := span.EndCol - 1
	if endCol < 0 {
		endCol = 0
	}

	return Range{
		Start: Position{Line: startLine, Character: startCol},
		End:   Position{Line: endLine, Character: endCol},
	}
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
