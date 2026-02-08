package transport

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/go-lua/lsp/index"
	"github.com/wippyai/go-lua/types/diag"
)

func TestDiagSpanToRange(t *testing.T) {
	tests := []struct {
		name string
		span diag.Span
		want Range
	}{
		{
			name: "normal span",
			span: diag.Span{StartLine: 10, StartCol: 5, EndLine: 10, EndCol: 15},
			want: Range{
				Start: Position{Line: 9, Character: 4},
				End:   Position{Line: 9, Character: 14},
			},
		},
		{
			name: "first line first col",
			span: diag.Span{StartLine: 1, StartCol: 1, EndLine: 1, EndCol: 10},
			want: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 0, Character: 9},
			},
		},
		{
			name: "zero values clamped",
			span: diag.Span{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 0},
			want: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 0, Character: 0},
			},
		},
		{
			name: "multiline span",
			span: diag.Span{StartLine: 5, StartCol: 10, EndLine: 8, EndCol: 20},
			want: Range{
				Start: Position{Line: 4, Character: 9},
				End:   Position{Line: 7, Character: 19},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DiagSpanToRange(tt.span)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertIndexSymbolKind(t *testing.T) {
	tests := []struct {
		input index.SymbolKind
		want  int
	}{
		{index.SymbolFunction, SymbolKindFunction},
		{index.SymbolMethod, SymbolKindMethod},
		{index.SymbolVariable, SymbolKindVariable},
		{index.SymbolParameter, SymbolKindVariable},
		{index.SymbolType, SymbolKindClass},
		{index.SymbolField, SymbolKindField},
		{index.SymbolKind(999), SymbolKindVariable}, // unknown defaults to variable
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, ConvertIndexSymbolKind(tt.input))
	}
}

func TestURIToID(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		want string
	}{
		{"wippy scheme", "wippy://@test/module", "@test/module"},
		{"raw id", "@test/module", "@test/module"},
		{"file scheme rejected", "file:///path/to/file.lua", ""},
		{"other scheme rejected", "http://example.com", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, URIToID(tt.uri))
		})
	}
}

func TestIDToURI(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{"registry id", "@test/module", "wippy://@test/module"},
		{"already wippy uri", "wippy://@test/module", "wippy://@test/module"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IDToURI(tt.id))
		})
	}
}

func TestConvertDiagnosticSeverity(t *testing.T) {
	tests := []struct {
		input diag.Severity
		want  int
	}{
		{diag.SeverityError, DiagnosticSeverityError},
		{diag.SeverityWarning, DiagnosticSeverityWarning},
		{diag.SeverityHint, DiagnosticSeverityHint},
		{diag.Severity(99), DiagnosticSeverityInformation},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, ConvertDiagnosticSeverity(tt.input))
	}
}

func TestConvertDiagnostics(t *testing.T) {
	diagnostics := []diag.Diagnostic{
		{
			Span:     diag.Span{StartLine: 1, StartCol: 5, EndLine: 1, EndCol: 10},
			Message:  "type mismatch",
			Severity: diag.SeverityError,
		},
		{
			Span:     diag.Span{StartLine: 3, StartCol: 1, EndLine: 3, EndCol: 5},
			Message:  "unused variable",
			Severity: diag.SeverityWarning,
		},
	}

	result := ConvertDiagnostics(diagnostics)
	assert.Len(t, result, 2)

	assert.Equal(t, "type mismatch", result[0].Message)
	assert.Equal(t, DiagnosticSeverityError, result[0].Severity)
	assert.Equal(t, "wippy", result[0].Source)
	assert.Equal(t, 0, result[0].Range.Start.Line)
	assert.Equal(t, 4, result[0].Range.Start.Character)

	assert.Equal(t, "unused variable", result[1].Message)
	assert.Equal(t, DiagnosticSeverityWarning, result[1].Severity)
}

func TestConvertDiagnostics_Empty(t *testing.T) {
	result := ConvertDiagnostics(nil)
	assert.NotNil(t, result)
	assert.Len(t, result, 0)

	result = ConvertDiagnostics([]diag.Diagnostic{})
	assert.NotNil(t, result)
	assert.Len(t, result, 0)
}

func TestSymbolKindConstants(t *testing.T) {
	tests := []struct {
		name  string
		value int
		want  int
	}{
		{"File", SymbolKindFile, 1},
		{"Module", SymbolKindModule, 2},
		{"Namespace", SymbolKindNamespace, 3},
		{"Package", SymbolKindPackage, 4},
		{"Class", SymbolKindClass, 5},
		{"Method", SymbolKindMethod, 6},
		{"Property", SymbolKindProperty, 7},
		{"Field", SymbolKindField, 8},
		{"Constructor", SymbolKindConstructor, 9},
		{"Enum", SymbolKindEnum, 10},
		{"Interface", SymbolKindInterface, 11},
		{"Function", SymbolKindFunction, 12},
		{"Variable", SymbolKindVariable, 13},
		{"Constant", SymbolKindConstant, 14},
		{"String", SymbolKindString, 15},
		{"Number", SymbolKindNumber, 16},
		{"Boolean", SymbolKindBoolean, 17},
		{"Array", SymbolKindArray, 18},
		{"Object", SymbolKindObject, 19},
		{"Key", SymbolKindKey, 20},
		{"Null", SymbolKindNull, 21},
		{"EnumMember", SymbolKindEnumMember, 22},
		{"Struct", SymbolKindStruct, 23},
		{"Event", SymbolKindEvent, 24},
		{"Operator", SymbolKindOperator, 25},
		{"TypeParameter", SymbolKindTypeParameter, 26},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.value, "%s should match LSP spec", tt.name)
	}
}
