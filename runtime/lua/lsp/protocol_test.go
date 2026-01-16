package lsp

import (
	"testing"

	"github.com/yuin/gopher-lua/lsp/index"
	"github.com/yuin/gopher-lua/types/diag"
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

			if got.Start.Line != tt.want.Start.Line {
				t.Errorf("Start.Line = %d, want %d", got.Start.Line, tt.want.Start.Line)
			}
			if got.Start.Character != tt.want.Start.Character {
				t.Errorf("Start.Character = %d, want %d", got.Start.Character, tt.want.Start.Character)
			}
			if got.End.Line != tt.want.End.Line {
				t.Errorf("End.Line = %d, want %d", got.End.Line, tt.want.End.Line)
			}
			if got.End.Character != tt.want.End.Character {
				t.Errorf("End.Character = %d, want %d", got.End.Character, tt.want.End.Character)
			}
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
		got := ConvertIndexSymbolKind(tt.input)
		if got != tt.want {
			t.Errorf("ConvertIndexSymbolKind(%d) = %d, want %d", tt.input, got, tt.want)
		}
	}
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
		if tt.value != tt.want {
			t.Errorf("%s = %d, want %d (LSP spec)", tt.name, tt.value, tt.want)
		}
	}
}
