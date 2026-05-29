// SPDX-License-Identifier: MPL-2.0

//go:build treesitter

// Package treesitter provides Tree-sitter parsing functionality and language support
package treesitter

import (
	"unsafe"

	tslua "github.com/tree-sitter-grammars/tree-sitter-lua/bindings/go"
	cscsharp "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"
	tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tshtml "github.com/tree-sitter/tree-sitter-html/bindings/go"
	tsjs "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tsphp "github.com/tree-sitter/tree-sitter-php/bindings/go"
	tspython "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tsts "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
	tsmd "github.com/wippyai/tree-sitter-markdown/bindings/go"
	tssql "github.com/wippyai/tree-sitter-sql/bindings/go"
)

// LanguageInfo holds information about a supported language.
type LanguageInfo struct {
	Language func() unsafe.Pointer
	Name     string
	Aliases  []string
}

// Languages maintains a registry of supported programming languages and their aliases
type Languages struct {
	supported map[string]*LanguageInfo
	li        []*LanguageInfo
}

// NewLanguages creates and initializes a new Languages registry with all supported languages
func NewLanguages() *Languages {
	li := []*LanguageInfo{
		{
			Name:     "lua",
			Aliases:  []string{"lua"},
			Language: tslua.Language,
		},
		{
			Name:     "php",
			Aliases:  []string{"php"},
			Language: tsphp.LanguagePHP,
		},
		{
			Name:     "go",
			Aliases:  []string{"go", "golang"},
			Language: tsgo.Language,
		},
		{
			Name:     "javascript",
			Aliases:  []string{"js", "javascript"},
			Language: tsjs.Language,
		},
		{
			Name:     "typescript+jsx",
			Aliases:  []string{"tsx"},
			Language: tsts.LanguageTSX,
		},
		{
			Name:     "typescript",
			Aliases:  []string{"ts", "typescript"},
			Language: tsts.LanguageTypescript,
		},
		{
			Name:     "python",
			Aliases:  []string{"python", "py"},
			Language: tspython.Language,
		},
		{
			Name:     "c#",
			Aliases:  []string{"csharp", "c#", "cs"},
			Language: cscsharp.Language,
		},
		{
			Name:     "html",
			Aliases:  []string{"html", "html5"},
			Language: tshtml.Language,
		},
		{
			Name:     "markdown",
			Aliases:  []string{"markdown", "md"},
			Language: tsmd.Language,
		},
		{
			Name:     "sql",
			Aliases:  []string{"sql"},
			Language: tssql.Language,
		},
	}

	// Initialize the map with enough capacity for all aliases
	totalAliases := 0
	for _, lang := range li {
		totalAliases += len(lang.Aliases)
	}

	supportedLanguages := make(map[string]*LanguageInfo, totalAliases)

	// Map all aliases to their language info
	for i := range li {
		langInfo := li[i]
		for _, alias := range langInfo.Aliases {
			supportedLanguages[alias] = langInfo
		}
	}

	return &Languages{
		li:        li,
		supported: supportedLanguages,
	}
}

// GetLanguageInfo returns the LanguageInfo for a given language alias or nil if not found
func (l *Languages) GetLanguageInfo(alias string) *LanguageInfo {
	return l.supported[alias]
}

// GetSupportedLanguages returns a list of all supported language names without duplicates
func (l *Languages) GetSupportedLanguages() []string {
	seen := make(map[string]bool)
	names := make([]string, 0, 10)
	for _, langInfo := range l.li {
		if !seen[langInfo.Name] {
			names = append(names, langInfo.Name)
			seen[langInfo.Name] = true
		}
	}

	return names
}
