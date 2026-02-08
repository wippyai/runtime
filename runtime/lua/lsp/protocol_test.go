package lsp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/go-lua/lsp/index"
	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/runtime/runtime/lua/lsp/transport"
)

func TestDiagSpanToRange(t *testing.T) {
	tests := []struct {
		name string
		span diag.Span
		want transport.Range
	}{
		{
			name: "normal span",
			span: diag.Span{StartLine: 10, StartCol: 5, EndLine: 10, EndCol: 15},
			want: transport.Range{
				Start: transport.Position{Line: 9, Character: 4},
				End:   transport.Position{Line: 9, Character: 14},
			},
		},
		{
			name: "first line first col",
			span: diag.Span{StartLine: 1, StartCol: 1, EndLine: 1, EndCol: 10},
			want: transport.Range{
				Start: transport.Position{Line: 0, Character: 0},
				End:   transport.Position{Line: 0, Character: 9},
			},
		},
		{
			name: "zero values clamped",
			span: diag.Span{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 0},
			want: transport.Range{
				Start: transport.Position{Line: 0, Character: 0},
				End:   transport.Position{Line: 0, Character: 0},
			},
		},
		{
			name: "multiline span",
			span: diag.Span{StartLine: 5, StartCol: 10, EndLine: 8, EndCol: 20},
			want: transport.Range{
				Start: transport.Position{Line: 4, Character: 9},
				End:   transport.Position{Line: 7, Character: 19},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := transport.DiagSpanToRange(tt.span)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertIndexSymbolKind(t *testing.T) {
	tests := []struct {
		input index.SymbolKind
		want  int
	}{
		{index.SymbolFunction, transport.SymbolKindFunction},
		{index.SymbolMethod, transport.SymbolKindMethod},
		{index.SymbolVariable, transport.SymbolKindVariable},
		{index.SymbolParameter, transport.SymbolKindVariable},
		{index.SymbolType, transport.SymbolKindClass},
		{index.SymbolField, transport.SymbolKindField},
		{index.SymbolKind(999), transport.SymbolKindVariable}, // unknown defaults to variable
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, transport.ConvertIndexSymbolKind(tt.input))
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
			assert.Equal(t, tt.want, transport.URIToID(tt.uri))
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
			assert.Equal(t, tt.want, transport.IDToURI(tt.id))
		})
	}
}

func TestSymbolKindConstants(t *testing.T) {
	tests := []struct {
		name  string
		value int
		want  int
	}{
		{"File", transport.SymbolKindFile, 1},
		{"Module", transport.SymbolKindModule, 2},
		{"Namespace", transport.SymbolKindNamespace, 3},
		{"Package", transport.SymbolKindPackage, 4},
		{"Class", transport.SymbolKindClass, 5},
		{"Method", transport.SymbolKindMethod, 6},
		{"Property", transport.SymbolKindProperty, 7},
		{"Field", transport.SymbolKindField, 8},
		{"Constructor", transport.SymbolKindConstructor, 9},
		{"Enum", transport.SymbolKindEnum, 10},
		{"Interface", transport.SymbolKindInterface, 11},
		{"Function", transport.SymbolKindFunction, 12},
		{"Variable", transport.SymbolKindVariable, 13},
		{"Constant", transport.SymbolKindConstant, 14},
		{"String", transport.SymbolKindString, 15},
		{"Number", transport.SymbolKindNumber, 16},
		{"Boolean", transport.SymbolKindBoolean, 17},
		{"Array", transport.SymbolKindArray, 18},
		{"Object", transport.SymbolKindObject, 19},
		{"Key", transport.SymbolKindKey, 20},
		{"Null", transport.SymbolKindNull, 21},
		{"EnumMember", transport.SymbolKindEnumMember, 22},
		{"Struct", transport.SymbolKindStruct, 23},
		{"Event", transport.SymbolKindEvent, 24},
		{"Operator", transport.SymbolKindOperator, 25},
		{"TypeParameter", transport.SymbolKindTypeParameter, 26},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.value, "%s should match LSP spec", tt.name)
	}
}
