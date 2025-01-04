package treesitter

import (
	_ "embed"
	"unsafe"

	treesitterlua "github.com/tree-sitter-grammars/tree-sitter-lua/bindings/go"
	treesittercsharp "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"
	treesittergo "github.com/tree-sitter/tree-sitter-go/bindings/go"
	treesitterhtml "github.com/tree-sitter/tree-sitter-html/bindings/go"
	treesitterjs "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	treesitterphp "github.com/tree-sitter/tree-sitter-php/bindings/go"
	treesitterpython "github.com/tree-sitter/tree-sitter-python/bindings/go"
	treesitterts "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// LanguageInfo holds information about a supported language.
type LanguageInfo struct {
	Name     string                // Full language name (e.g., "JavaScript")
	Aliases  []string              // Alternative names or short codes (e.g., ["js", "javascript"])
	Language func() unsafe.Pointer // Function to get the Tree-sitter language object
}

// languageDefinitions contains the core language definitions
var languageDefinitions = []LanguageInfo{
	{
		Name:     "lua",
		Aliases:  []string{"lua"},
		Language: treesitterlua.Language,
	},
	{
		Name:     "php",
		Aliases:  []string{"php"},
		Language: treesitterphp.LanguagePHP,
	},
	{
		Name:     "go",
		Aliases:  []string{"go", "golang"},
		Language: treesittergo.Language,
	},
	{
		Name:     "javascript",
		Aliases:  []string{"js", "javascript"},
		Language: treesitterjs.Language,
	},
	{
		Name:     "typescript+jsx",
		Aliases:  []string{"tsx"},
		Language: treesitterts.LanguageTSX,
	},
	{
		Name:     "typescript",
		Aliases:  []string{"ts", "typescript"},
		Language: treesitterts.LanguageTypescript,
	},
	{
		Name:     "python",
		Aliases:  []string{"python", "py"},
		Language: treesitterpython.Language,
	},
	{
		Name:     "c#",
		Aliases:  []string{"csharp", "c#", "cs"},
		Language: treesittercsharp.Language,
	},
	{
		Name:     "html",
		Aliases:  []string{"html", "html5"},
		Language: treesitterhtml.Language,
	},
	{
		Name:     "markdown",
		Aliases:  []string{"markdown", "md"},
		Language: nil, // todo: fix me and test me
	},
	{
		Name:     "sql",
		Aliases:  []string{"sql"},
		Language: nil, // todo: fix me
	},
}

// supportedLanguages is a map of all language aliases to their LanguageInfo
var supportedLanguages map[string]*LanguageInfo

func init() {
	// Initialize the map with enough capacity for all aliases
	totalAliases := 0
	for _, lang := range languageDefinitions {
		totalAliases += len(lang.Aliases)
	}
	supportedLanguages = make(map[string]*LanguageInfo, totalAliases)

	// Map all aliases to their language info
	for i := range languageDefinitions {
		langInfo := &languageDefinitions[i]
		for _, alias := range langInfo.Aliases {
			supportedLanguages[alias] = langInfo
		}
	}
}

// GetLanguageInfo returns the LanguageInfo for a given language alias.
func GetLanguageInfo(alias string) *LanguageInfo {
	return supportedLanguages[alias]
}

// GetSupportedLanguages returns a list of supported language names.
func GetSupportedLanguages() []string {
	seen := make(map[string]bool)
	names := make([]string, 0, len(languageDefinitions))
	for _, langInfo := range languageDefinitions {
		if !seen[langInfo.Name] {
			names = append(names, langInfo.Name)
			seen[langInfo.Name] = true
		}
	}
	return names
}
